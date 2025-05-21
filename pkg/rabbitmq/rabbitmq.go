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
	"linux_daemon_service/pkg/api"
	"linux_daemon_service/pkg/trivy"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQ 구조체는 RabbitMQ 연결과 채널을 관리합니다
type RabbitMQ struct {
	conn           *amqp.Connection
	channel        *amqp.Channel // 기존 호환성을 위한 채널 (deprecated)
	requestChannel *amqp.Channel // 요청 큐 소비용 채널
	resultChannel  *amqp.Channel // 결과 큐 발행용 채널
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

	// 기본 채널 생성 (이전 호환성용)
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("rabbitmq 기본 채널 생성 실패: %v", err)
	}

	// 요청 채널 생성
	requestCh, err := conn.Channel()
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("rabbitmq 요청 채널 생성 실패: %v", err)
	}
	log.Printf("요청 전용 채널 생성 완료")

	// 결과 채널 생성
	resultCh, err := conn.Channel()
	if err != nil {
		requestCh.Close()
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("rabbitmq 결과 채널 생성 실패: %v", err)
	}
	log.Printf("결과 전용 채널 생성 완료")

	// Exchange 선언 (결과 채널 사용)
	err = resultCh.ExchangeDeclare(
		exchange, // 이름
		"topic",  // 타입 (topic으로 변경하여 라우팅 키 패턴 지원)
		true,     // durable
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		resultCh.Close()
		requestCh.Close()
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("exchange 선언 실패: %v", err)
	}

	// 요청 큐 선언 (요청 채널 사용)
	_, err = requestCh.QueueDeclare(
		requestQueue, // 이름
		true,         // durable
		false,        // delete when unused
		false,        // exclusive
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		resultCh.Close()
		requestCh.Close()
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("요청 queue 선언 실패: %v", err)
	}

	// 요청 큐와 Exchange 바인딩 (요청 채널 사용)
	err = requestCh.QueueBind(
		requestQueue,   // queue name
		requestRouting, // routing key
		exchange,       // exchange
		false,
		nil,
	)
	if err != nil {
		resultCh.Close()
		requestCh.Close()
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("요청 queue 바인딩 실패: %v", err)
	}

	// 결과 큐 선언 (결과 채널 사용)
	_, err = resultCh.QueueDeclare(
		resultQueue, // 이름
		true,        // durable
		false,       // delete when unused
		false,       // exclusive
		false,       // no-wait
		nil,         // arguments
	)
	if err != nil {
		resultCh.Close()
		requestCh.Close()
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("결과 queue 선언 실패: %v", err)
	}

	// 결과 큐와 Exchange 바인딩 (결과 채널 사용)
	err = resultCh.QueueBind(
		resultQueue,   // queue name
		resultRouting, // routing key
		exchange,      // exchange
		false,
		nil,
	)
	if err != nil {
		resultCh.Close()
		requestCh.Close()
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("결과 queue 바인딩 실패: %v", err)
	}

	// 각 채널에 QoS 설정
	if err := requestCh.Qos(1, 0, false); err != nil {
		resultCh.Close()
		requestCh.Close()
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("요청 채널 QoS 설정 실패: %v", err)
	}

	if err := resultCh.Qos(1, 0, false); err != nil {
		resultCh.Close()
		requestCh.Close()
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("결과 채널 QoS 설정 실패: %v", err)
	}

	log.Printf("RabbitMQ 채널 분리 설정 완료: 요청 채널 및 결과 채널 준비됨")

	return &RabbitMQ{
		conn:           conn,
		channel:        ch, // 이전 호환성용
		requestChannel: requestCh,
		resultChannel:  resultCh,
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

	// 결과 전용 채널 사용
	err = r.resultChannel.PublishWithContext(
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

		// 결과 채널이 닫혔는지 확인하고 필요시 새 채널 생성 시도
		if r.resultChannel.IsClosed() {
			log.Printf("결과 채널이 닫혀 있습니다. 새 채널 생성 시도...")
			newResultCh, chErr := r.conn.Channel()
			if chErr != nil {
				log.Printf("새 결과 채널 생성 실패: %v", chErr)
				return fmt.Errorf("결과 채널 재생성 실패: %v", chErr)
			}
			r.resultChannel = newResultCh

			// 새 채널에 QoS 설정
			if qosErr := r.resultChannel.Qos(1, 0, false); qosErr != nil {
				log.Printf("새 결과 채널 QoS 설정 실패: %v", qosErr)
			}

			// 재시도
			retryErr := r.resultChannel.PublishWithContext(
				ctx,
				r.exchange,
				r.resultRouting,
				false,
				false,
				amqp.Publishing{
					ContentType:  "application/json",
					DeliveryMode: amqp.Persistent,
					Body:         jsonData,
				},
			)

			if retryErr != nil {
				log.Printf("재시도 결과 메시지 발행 실패: %v", retryErr)
				return fmt.Errorf("rabbitmq 결과 발행 실패 (재시도 후): %v", retryErr)
			}

			log.Printf("재시도 결과 메시지 발행 성공!")
			return nil
		}

		return fmt.Errorf("rabbitmq 결과 발행 실패: %v", err)
	}

	log.Printf("결과 메시지 발행 성공!")
	return nil
}

// ConsumeRequests는 이미지 분석 요청을 소비합니다.
func (r *RabbitMQ) ConsumeRequests(ctx context.Context) error {
	// 요청 전용 채널 사용
	msgs, err := r.requestChannel.Consume(
		r.requestQueue, // queue
		"",             // consumer
		false,          // auto-ack (수동 ack로 변경)
		false,          // exclusive
		false,          // no-local
		false,          // no-wait
		nil,            // args
	)
	if err != nil {
		// 요청 채널이 닫혔는지 확인하고 필요시 새 채널 생성 시도
		if r.requestChannel.IsClosed() {
			log.Printf("요청 채널이 닫혀 있습니다. 새 채널 생성 시도...")
			newRequestCh, chErr := r.conn.Channel()
			if chErr != nil {
				log.Printf("새 요청 채널 생성 실패: %v", chErr)
				return fmt.Errorf("요청 채널 재생성 실패: %v", chErr)
			}
			r.requestChannel = newRequestCh

			// 새 채널에 QoS 설정
			if qosErr := r.requestChannel.Qos(1, 0, false); qosErr != nil {
				log.Printf("새 요청 채널 QoS 설정 실패: %v", qosErr)
			}

			// 요청 큐 선언 및 바인딩 재설정
			_, declareErr := r.requestChannel.QueueDeclare(
				r.requestQueue,
				true,
				false,
				false,
				false,
				nil,
			)
			if declareErr != nil {
				log.Printf("요청 큐 재선언 실패: %v", declareErr)
				return fmt.Errorf("요청 큐 재선언 실패: %v", declareErr)
			}

			bindErr := r.requestChannel.QueueBind(
				r.requestQueue,
				r.requestRouting,
				r.exchange,
				false,
				nil,
			)
			if bindErr != nil {
				log.Printf("요청 큐 재바인딩 실패: %v", bindErr)
				return fmt.Errorf("요청 큐 재바인딩 실패: %v", bindErr)
			}

			// 재시도
			return r.ConsumeRequests(ctx)
		}

		return fmt.Errorf("요청 메시지 소비 실패: %v", err)
	}

	log.Printf("요청 큐에서 메시지 소비 시작")

	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Printf("컨텍스트 취소로 요청 소비 중단")
				return
			case msg, ok := <-msgs:
				if !ok {
					log.Println("메시지 채널이 닫혔습니다")

					// 채널이 닫힌 경우 재연결 시도
					log.Println("요청 채널 재연결 시도...")

					// 기존 채널 종료
					if r.requestChannel != nil && !r.requestChannel.IsClosed() {
						r.requestChannel.Close()
					}

					// 새 채널 생성
					newCh, err := r.conn.Channel()
					if err != nil {
						log.Printf("요청 채널 재생성 실패: %v", err)
						return
					}

					r.requestChannel = newCh

					// QoS 설정
					if err := r.requestChannel.Qos(1, 0, false); err != nil {
						log.Printf("요청 채널 QoS 설정 실패: %v", err)
					}

					// 소비 재시작 시도
					if err := r.ConsumeRequests(ctx); err != nil {
						log.Printf("요청 소비 재시작 실패: %v", err)
					}
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
	log.Printf("요청 채널에서 메시지 수신: %s", string(msg.Body))

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
	// API 클라이언트 생성
	apiClient := api.NewClient(r.config)

	// 이미지 분석 실행
	log.Printf("이미지 분석 시작: JobID=%s, Image=%s:%s", request.JobID, request.ImageName, request.Tag)
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

		// API로 실패 결과 전송
		if err := apiClient.SendAnalysisResult(ctx, failedResult); err != nil {
			log.Printf("API 실패 결과 전송 실패: %v", err)
		}
		return
	}

	// 분석 결과 API로 전송
	log.Printf("분석 결과 API 전송 준비: JobID=%s, Status=%s, Vulnerabilities=%d",
		result.JobID, result.Status, result.Vulnerabilities)

	if err := apiClient.SendAnalysisResult(ctx, result); err != nil {
		log.Printf("API 결과 전송 실패: %v", err)

		// 실패 시 RabbitMQ로 백업 전송 시도 (선택적)
		log.Printf("RabbitMQ로 백업 전송 시도")
		if publishErr := r.PublishResult(ctx, result); publishErr != nil {
			log.Printf("RabbitMQ 백업 전송도 실패: %v", publishErr)
		} else {
			log.Printf("RabbitMQ 백업 전송 성공")
		}
	} else {
		log.Printf("API 결과 전송 완료: JobID=%s", result.JobID)
	}
}

// Close는 RabbitMQ 연결을 닫습니다.
func (r *RabbitMQ) Close() error {
	if r.resultChannel != nil {
		if !r.resultChannel.IsClosed() {
			r.resultChannel.Close()
		}
	}

	if r.requestChannel != nil {
		if !r.requestChannel.IsClosed() {
			r.requestChannel.Close()
		}
	}

	if r.channel != nil {
		if !r.channel.IsClosed() {
			r.channel.Close()
		}
	}

	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}
