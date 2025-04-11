package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"linux_daemon_service/config"
	"linux_daemon_service/pkg/trivy"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQ 구조체는 RabbitMQ 연결과 채널을 관리합니다
type RabbitMQ struct {
	conn           *amqp.Connection
	channel        *amqp.Channel
	config         config.Config
	exchange       string
	requestQueue   string
	requestRouting string
	resultQueue    string
	resultRouting  string
}

// BackendRequestMessage 백엔드에서 보내는 메시지 구조체
type BackendRequestMessage struct {
	JobID     string `json:"job_id"`
	ImageURL  string `json:"image_url"`
	CreatedAt string `json:"created_at"`
	Action    string `json:"action"`
}

// BackendResponseMessage 백엔드에 전송할 결과 메시지 구조체
type BackendResponseMessage struct {
	JobID           string                `json:"job_id"`
	ImageURL        string                `json:"image_url"`
	Status          string                `json:"status"`          // success, error
	Vulnerabilities int                   `json:"vulnerabilities"` // 취약점 개수
	CompletedAt     string                `json:"completed_at"`
	ErrorMsg        string                `json:"error_msg,omitempty"`
	Action          string                `json:"action"`
	VulnDetails     []trivy.Vulnerability `json:"vuln_details,omitempty"` // 취약점 상세 정보
}

// 환경 변수 처리 함수 - 환경 변수가 있으면 사용, 없으면 config 값 사용
func getEnvOrConfig(envKey, configValue string) string {
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	return configValue
}

// ConnectWithRetry RabbitMQ에 최대 시도 횟수만큼 재시도합니다
func ConnectWithRetry(url string, maxRetries int, retryInterval time.Duration) (*amqp.Connection, error) {
	var conn *amqp.Connection
	var err error

	log.Printf("RabbitMQ 연결 시도 중: %s", url)

	for i := 0; i < maxRetries; i++ {
		conn, err = amqp.Dial(url)
		if err == nil {
			log.Printf("RabbitMQ에 연결되었습니다 (시도 %d/%d)", i+1, maxRetries)
			return conn, nil
		}

		log.Printf("RabbitMQ 연결 실패 (시도 %d/%d): %v", i+1, maxRetries, err)

		if i < maxRetries-1 {
			log.Printf("%s 후 재시도합니다...", retryInterval)
			time.Sleep(retryInterval)
		}
	}

	return nil, fmt.Errorf("rabbitmq에 연결할 수 없습니다 (최대 시도 횟수 초과): %v", err)
}

// NewRabbitMQ는 RabbitMQ 클라이언트를 생성합니다.
func NewRabbitMQ(cfg config.Config) (*RabbitMQ, error) {
	// 환경 변수 또는 설정 파일에서 RabbitMQ 정보 가져오기
	url := getEnvOrConfig("RABBITMQ_URL", cfg.RabbitMQ.URL)
	exchange := getEnvOrConfig("RABBITMQ_EXCHANGE", cfg.RabbitMQ.Exchange)

	// 이미지 분석 명령 수신 큐
	requestQueue := os.Getenv("RABBITMQ_REQUEST_QUEUE")
	requestRouting := os.Getenv("RABBITMQ_REQUEST_ROUTING_KEY")

	// 이미지 분석 결과 발신 큐
	resultQueue := os.Getenv("RABBITMQ_RESULT_QUEUE")
	resultRouting := os.Getenv("RABBITMQ_RESULT_ROUTING_KEY")

	log.Printf("RabbitMQ 설정: URL=%s, Exchange=%s", url, exchange)
	log.Printf("요청 큐: %s (라우팅 키: %s)", requestQueue, requestRouting)
	log.Printf("결과 큐: %s (라우팅 키: %s)", resultQueue, resultRouting)

	// RabbitMQ 연결 (최대 5회 재시도, 5초 간격)
	conn, err := ConnectWithRetry(url, 5, 5*time.Second)
	if err != nil {
		return nil, err
	}

	// 채널 생성
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("rabbitmq 채널 생성 실패: %v", err)
	}

	// Exchange 선언
	err = ch.ExchangeDeclare(
		exchange, // 이름
		"topic",  // 타입 (topic으로 변경하여 라우팅 키 패턴 지원)
		true,     // durable
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("exchange 선언 실패: %v", err)
	}

	// 요청 큐 선언
	_, err = ch.QueueDeclare(
		requestQueue, // 이름
		true,         // durable
		false,        // delete when unused
		false,        // exclusive
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("요청 queue 선언 실패: %v", err)
	}

	// 요청 큐와 Exchange 바인딩
	err = ch.QueueBind(
		requestQueue,   // queue name
		requestRouting, // routing key
		exchange,       // exchange
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("요청 queue 바인딩 실패: %v", err)
	}

	// 결과 큐 선언
	_, err = ch.QueueDeclare(
		resultQueue, // 이름
		true,        // durable
		false,       // delete when unused
		false,       // exclusive
		false,       // no-wait
		nil,         // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("결과 queue 선언 실패: %v", err)
	}

	// 결과 큐와 Exchange 바인딩
	err = ch.QueueBind(
		resultQueue,   // queue name
		resultRouting, // routing key
		exchange,      // exchange
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("결과 queue 바인딩 실패: %v", err)
	}

	return &RabbitMQ{
		conn:           conn,
		channel:        ch,
		config:         cfg,
		exchange:       exchange,
		requestQueue:   requestQueue,
		requestRouting: requestRouting,
		resultQueue:    resultQueue,
		resultRouting:  resultRouting,
	}, nil
}

// PublishResult는 도커 이미지 분석 결과를 발행합니다.
func (r *RabbitMQ) PublishResult(ctx context.Context, result *trivy.DockerImageResult) error {
	// 백엔드 응답 메시지로 변환
	backendResponse := &BackendResponseMessage{
		JobID:           result.JobID,
		ImageURL:        fmt.Sprintf("%s:%s", result.ImageName, result.Tag),
		Status:          result.Status,
		Vulnerabilities: result.Vulnerabilities,
		CompletedAt:     result.CompletedAt.Format(time.RFC3339),
		ErrorMsg:        result.ErrorMsg,
		Action:          "analyze_docker_image_result",
		VulnDetails:     result.VulnDetails,
	}

	// 메시지를 JSON으로 인코딩
	jsonData, err := json.Marshal(backendResponse)
	if err != nil {
		return fmt.Errorf("json 인코딩 실패: %v", err)
	}

	log.Printf("결과 메시지 발행 시도: exchange=%s, routing_key=%s", r.exchange, r.resultRouting)
	log.Printf("결과 메시지 내용: %s", string(jsonData))

	// 현재 환경 변수 출력 (디버깅용)
	log.Printf("환경 변수 확인: RABBITMQ_QUEUE=%s, RABBITMQ_ROUTING_KEY=%s",
		os.Getenv("RABBITMQ_QUEUE"), os.Getenv("RABBITMQ_ROUTING_KEY"))
	log.Printf("환경 변수 확인: RABBITMQ_RESULT_QUEUE=%s, RABBITMQ_RESULT_ROUTING_KEY=%s",
		os.Getenv("RABBITMQ_RESULT_QUEUE"), os.Getenv("RABBITMQ_RESULT_ROUTING_KEY"))

	err = r.channel.PublishWithContext(
		ctx,
		r.exchange,      // exchange
		r.resultRouting, // routing key
		false,           // mandatory
		false,           // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // 메시지 지속성 추가
			Body:         jsonData,
		},
	)

	if err != nil {
		log.Printf("결과 메시지 발행 실패: %v", err)
		return fmt.Errorf("rabbitmq 발행 실패: %v", err)
	}

	log.Printf("결과 메시지 발행 성공!")
	return nil
}

// ConsumeRequests는 이미지 분석 요청을 소비합니다.
func (r *RabbitMQ) ConsumeRequests(ctx context.Context) error {
	msgs, err := r.channel.Consume(
		r.requestQueue, // queue
		"",             // consumer
		false,          // auto-ack (수동 ack로 변경)
		false,          // exclusive
		false,          // no-local
		false,          // no-wait
		nil,            // args
	)
	if err != nil {
		return fmt.Errorf("요청 메시지 소비 실패: %v", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					log.Println("메시지 채널이 닫혔습니다")
					return
				}

				// 요청 메시지 처리 및 응답 발행
				go r.processImageRequest(ctx, msg)
			}
		}
	}()

	return nil
}

// processImageRequest는 이미지 분석 요청을 처리하고 결과를 발행합니다.
func (r *RabbitMQ) processImageRequest(ctx context.Context, msg amqp.Delivery) {
	defer func() {
		if err := msg.Ack(false); err != nil {
			log.Printf("메시지 Ack 실패: %v", err)
		}
	}()

	// 원본 메시지 로깅
	log.Printf("수신된 메시지: %s", string(msg.Body))

	// 요청 메시지 파싱 시도 - 백엔드 형식
	var backendRequest BackendRequestMessage
	if err := json.Unmarshal(msg.Body, &backendRequest); err != nil {
		log.Printf("백엔드 요청 파싱 실패: %v, 원본 메시지: %s", err, msg.Body)

		// 기존 형식으로 파싱 시도
		var request trivy.DockerImageRequest
		if err := json.Unmarshal(msg.Body, &request); err != nil {
			log.Printf("요청 파싱 실패: %v, 원본 메시지: %s", err, msg.Body)
			return
		}

		log.Printf("도커 이미지 분석 요청 수신 (기존 형식): JobID=%s, Image=%s:%s",
			request.JobID, request.ImageName, request.Tag)

		// 이미지 분석 실행
		r.processStandardRequest(ctx, &request)
		return
	}

	// 백엔드 형식에서 이미지 이름과 태그 추출
	log.Printf("도커 이미지 분석 요청 수신 (백엔드 형식): JobID=%s, ImageURL=%s, Action=%s",
		backendRequest.JobID, backendRequest.ImageURL, backendRequest.Action)

	// 작업 타입 확인
	if backendRequest.Action != "analyze_docker_image" {
		log.Printf("지원하지 않는 작업 타입: %s", backendRequest.Action)
		return
	}

	// 이미지 URL 파싱 (format: registry/image:tag)
	imageAndTag := strings.Split(backendRequest.ImageURL, ":")
	imageName := imageAndTag[0]
	tag := "latest" // 기본값

	if len(imageAndTag) > 1 {
		tag = imageAndTag[1]
	}

	// 내부 요청 객체 생성
	request := &trivy.DockerImageRequest{
		JobID:     backendRequest.JobID,
		ImageName: imageName,
		Tag:       tag,
	}

	log.Printf("파싱된 이미지 정보: %s:%s", request.ImageName, request.Tag)

	// 이미지 분석 실행
	r.processStandardRequest(ctx, request)
}

// processStandardRequest는 표준 요청 형식에 대한 이미지 분석을 처리합니다.
func (r *RabbitMQ) processStandardRequest(ctx context.Context, request *trivy.DockerImageRequest) {
	// 이미지 분석 실행
	analysisCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	result, err := trivy.AnalyzeDockerImage(analysisCtx, request)
	if err != nil {
		log.Printf("이미지 분석 실패: %v", err)

		failedResult := &trivy.DockerImageResult{
			JobID:       request.JobID,
			ImageName:   request.ImageName,
			Tag:         request.Tag,
			Status:      "error",
			CompletedAt: time.Now(),
			ErrorMsg:    fmt.Sprintf("분석 처리 오류: %v", err),
		}

		if err := r.PublishResult(ctx, failedResult); err != nil {
			log.Printf("실패 결과 발행 실패: %v", err)
		}
		return
	}

	// 분석 결과 발행
	log.Printf("분석 결과 발행: JobID=%s, Status=%s, Vulnerabilities=%d",
		result.JobID, result.Status, result.Vulnerabilities)

	if err := r.PublishResult(ctx, result); err != nil {
		log.Printf("결과 발행 실패: %v", err)
	}
}

// Close는 RabbitMQ 연결을 닫습니다.
func (r *RabbitMQ) Close() error {
	if r.channel != nil {
		r.channel.Close()
	}
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}
