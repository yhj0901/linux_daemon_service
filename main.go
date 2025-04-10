package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"linux_daemon_service/config"

	amqp "github.com/rabbitmq/amqp091-go"
)

var (
	configPath = flag.String("config", "config/config.yaml", "설정 파일 경로")
	pidFile    = flag.String("pid", "/var/run/linux_daemon_service.pid", "PID 파일 경로")
)

// 도커 이미지 분석 요청 메시지 구조체
type DockerImageRequest struct {
	JobID     string `json:"job_id"`
	ImageName string `json:"image_name"`
	Tag       string `json:"tag"`
}

// 백엔드에서 보내는 메시지 구조체 추가
type BackendRequestMessage struct {
	JobID     string `json:"job_id"`
	ImageURL  string `json:"image_url"`
	CreatedAt string `json:"created_at"`
	Action    string `json:"action"`
}

// 도커 이미지 분석 결과 메시지 구조체
type DockerImageResult struct {
	JobID           string          `json:"job_id"`
	ImageName       string          `json:"image_name"`
	Tag             string          `json:"tag"`
	Status          string          `json:"status"`          // success, error
	Vulnerabilities int             `json:"vulnerabilities"` // 취약점 개수
	CompletedAt     time.Time       `json:"completed_at"`
	ErrorMsg        string          `json:"error_msg,omitempty"`
	VulnDetails     []Vulnerability `json:"vuln_details,omitempty"` // 취약점 상세 정보
}

type Vulnerability struct {
	VulnerabilityID  string   `json:"vulnerability_id"`
	PkgID            string   `json:"pkg_id"`
	PkgName          string   `json:"pkg_name"`
	InstalledVersion string   `json:"installed_version"`
	FixedVersion     string   `json:"fixed_version"`
	Status           string   `json:"status"`
	Severity         string   `json:"severity"`
	Title            string   `json:"title"`
	Description      string   `json:"description,omitempty"`
	References       []string `json:"references,omitempty"`
	PublishedDate    string   `json:"published_date,omitempty"`
	LastModifiedDate string   `json:"last_modified_date,omitempty"`
}

// 백엔드에 전송할 결과 메시지 구조체
type BackendResponseMessage struct {
	JobID           string          `json:"job_id"`
	ImageURL        string          `json:"image_url"`
	Status          string          `json:"status"`          // success, error
	Vulnerabilities int             `json:"vulnerabilities"` // 취약점 개수
	CompletedAt     string          `json:"completed_at"`
	ErrorMsg        string          `json:"error_msg,omitempty"`
	Action          string          `json:"action"`
	VulnDetails     []Vulnerability `json:"vuln_details,omitempty"` // 취약점 상세 정보
}

// ProcessManager는 프로세스 관리를 담당하는 구조체입니다.
type ProcessManager struct {
	pidFile string
}

// NewProcessManager는 새로운 ProcessManager를 생성합니다.
func NewProcessManager(pidFile string) *ProcessManager {
	return &ProcessManager{
		pidFile: pidFile,
	}
}

// GetPID는 현재 실행 중인 프로세스의 PID를 반환합니다.
func (pm *ProcessManager) GetPID() (int, error) {
	pidBytes, err := os.ReadFile(pm.pidFile)
	if err != nil {
		return 0, fmt.Errorf("pid 파일을 읽을 수 없습니다: %v", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		return 0, fmt.Errorf("pid 파일의 내용이 올바르지 않습니다: %v", err)
	}

	return pid, nil
}

// IsRunning은 프로세스가 실행 중인지 확인합니다.
func (pm *ProcessManager) IsRunning() (bool, error) {
	pid, err := pm.GetPID()
	if err != nil {
		return false, nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil, nil
}

// Stop은 프로세스를 정상적으로 종료합니다.
func (pm *ProcessManager) Stop() error {
	pid, err := pm.GetPID()
	if err != nil {
		return err
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("프로세스를 찾을 수 없습니다: %v", err)
	}

	return process.Signal(syscall.SIGTERM)
}

// Reload는 프로세스를 재시작합니다.
func (pm *ProcessManager) Reload() error {
	pid, err := pm.GetPID()
	if err != nil {
		return err
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("프로세스를 찾을 수 없습니다: %v", err)
	}

	return process.Signal(syscall.SIGHUP)
}

// WritePID는 현재 프로세스의 PID를 파일에 저장합니다.
func (pm *ProcessManager) WritePID() error {
	pid := os.Getpid()
	return os.WriteFile(pm.pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0644)
}

// RemovePID는 PID 파일을 삭제합니다.
func (pm *ProcessManager) RemovePID() error {
	return os.Remove(pm.pidFile)
}

// RabbitMQ 관련 구조체와 함수
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

	// 요청 큐 설정
	requestQueue := getEnvOrConfig("RABBITMQ_REQUEST_QUEUE", cfg.RabbitMQ.RequestQueue)
	requestRouting := getEnvOrConfig("RABBITMQ_REQUEST_ROUTING_KEY", cfg.RabbitMQ.RequestRoutingKey)

	// 결과 큐 설정 - 백엔드 호환성 지원
	// 기존 형식(RABBITMQ_QUEUE)이 있으면 우선 사용, 없으면 신규 형식 사용
	var resultQueue, resultRouting string

	// 백엔드 호환 방식 (기존 환경 변수)
	legacyQueue := os.Getenv("RABBITMQ_QUEUE")
	legacyRouting := os.Getenv("RABBITMQ_ROUTING_KEY")

	// 신규 방식 (확장된 환경 변수)
	newQueue := os.Getenv("RABBITMQ_RESULT_QUEUE")
	newRouting := os.Getenv("RABBITMQ_RESULT_ROUTING_KEY")

	// 우선순위: 기존 환경 변수 > 신규 환경 변수 > 설정 파일
	if legacyQueue != "" {
		resultQueue = legacyQueue
		log.Printf("기존 환경 변수를 사용합니다: RABBITMQ_QUEUE=%s", resultQueue)
	} else if newQueue != "" {
		resultQueue = newQueue
		log.Printf("신규 환경 변수를 사용합니다: RABBITMQ_RESULT_QUEUE=%s", resultQueue)
	} else {
		resultQueue = cfg.RabbitMQ.ResultQueue
		log.Printf("설정 파일의 결과 큐를 사용합니다: %s", resultQueue)
	}

	if legacyRouting != "" {
		resultRouting = legacyRouting
		log.Printf("기존 환경 변수를 사용합니다: RABBITMQ_ROUTING_KEY=%s", resultRouting)
	} else if newRouting != "" {
		resultRouting = newRouting
		log.Printf("신규 환경 변수를 사용합니다: RABBITMQ_RESULT_ROUTING_KEY=%s", resultRouting)
	} else {
		resultRouting = cfg.RabbitMQ.ResultRoutingKey
		log.Printf("설정 파일의 결과 라우팅 키를 사용합니다: %s", resultRouting)
	}

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
func (r *RabbitMQ) PublishResult(ctx context.Context, result *DockerImageResult) error {
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

// AnalyzeDockerImage는 Trivy를 사용하여 도커 이미지를 분석합니다.
func AnalyzeDockerImage(ctx context.Context, req *DockerImageRequest) (*DockerImageResult, error) {
	fmt.Println("AnalyzeDockerImage 실행 :", ctx)
	imageFullName := fmt.Sprintf("%s:%s", req.ImageName, req.Tag)

	result := &DockerImageResult{
		JobID:     req.JobID,
		ImageName: req.ImageName,
		Tag:       req.Tag,
		Status:    "processing",
	}

	// 사용자 홈 디렉토리 가져오기
	homeDir, err := os.UserHomeDir()
	if err != nil {
		result.Status = "error"
		result.ErrorMsg = fmt.Sprintf("사용자 홈 디렉토리를 찾을 수 없습니다: %v", err)
		return result, nil
	}

	// 시스템 아키텍처 확인
	arch := runtime.GOARCH
	osType := runtime.GOOS
	log.Printf("시스템 아키텍처: %s, OS: %s", arch, osType)

	// 아키텍처에 맞는 trivy 파일명 결정
	var trivyFileName string
	switch {
	case arch == "amd64" && osType == "linux":
		trivyFileName = "trivy_linux_amd64"
	case arch == "arm64" && osType == "linux":
		trivyFileName = "trivy_linux_arm64"
	default:
		result.Status = "error"
		result.ErrorMsg = fmt.Sprintf("지원하지 않는 아키텍처입니다: %s/%s", osType, arch)
		return result, nil
	}

	// trivy 경로 설정
	binDir := filepath.Join(homeDir, "bin")
	trivyPath := filepath.Join(binDir, trivyFileName)
	fmt.Println("trivyPath: ", trivyPath)

	// bin 디렉토리 생성
	if err := os.MkdirAll(binDir, 0755); err != nil {
		result.Status = "error"
		result.ErrorMsg = fmt.Sprintf("bin 디렉토리 생성 실패: %v", err)
		return result, nil
	}

	// trivy 존재 여부 확인
	if _, err := os.Stat(trivyPath); os.IsNotExist(err) {
		log.Printf("trivy가 설치되어 있지 않습니다. S3에서 다운로드 시작...")

		// S3에서 파일 다운로드
		cmd := exec.Command("aws", "s3", "cp",
			fmt.Sprintf("s3://docker-analysis-api-dev-binaries-92a1fdfdba50a7e6/%s", trivyFileName),
			trivyPath)

		if output, err := cmd.CombinedOutput(); err != nil {
			result.Status = "error"
			result.ErrorMsg = fmt.Sprintf("trivy 다운로드 실패: %v - %s", err, string(output))
			return result, nil
		}

		// 실행 권한 설정
		if err := os.Chmod(trivyPath, 0755); err != nil {
			result.Status = "error"
			result.ErrorMsg = fmt.Sprintf("trivy 권한 설정 실패: %v", err)
			return result, nil
		}

		log.Printf("trivy 다운로드 및 권한 설정 완료: %s", trivyPath)
	}

	// Trivy 명령어 실행 준비
	cmd := exec.CommandContext(ctx, trivyPath, "image", "--format", "json", imageFullName)
	log.Printf("실행 명령어: %s %s", trivyPath, strings.Join(cmd.Args[1:], " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("trivy 실행 오류: %v", err)
		log.Printf("trivy 출력: %s", string(output))
		result.Status = "error"
		result.ErrorMsg = fmt.Sprintf("이미지 분석 실패: %v - %s", err, string(output))
		return result, nil
	}

	// 결과 처리
	result.CompletedAt = time.Now()

	// trivy 출력에서 JSON 부분만 추출
	outputStr := string(output)
	jsonStart := strings.Index(outputStr, "{")
	if jsonStart == -1 {
		log.Printf("JSON 시작 부분을 찾을 수 없습니다")
		result.Status = "error"
		result.ErrorMsg = "JSON 출력을 찾을 수 없습니다"
		return result, nil
	}

	jsonOutput := outputStr[jsonStart:]
	log.Printf("추출된 JSON 출력: %s", jsonOutput)

	// Trivy 출력 결과 파싱
	var trivyResult struct {
		SchemaVersion int    `json:"SchemaVersion"`
		CreatedAt     string `json:"CreatedAt"`
		Results       []struct {
			Target          string `json:"Target"`
			Vulnerabilities []struct {
				VulnerabilityID  string   `json:"VulnerabilityID"`
				PkgID            string   `json:"PkgID"`
				PkgName          string   `json:"PkgName"`
				InstalledVersion string   `json:"InstalledVersion"`
				FixedVersion     string   `json:"FixedVersion"`
				Status           string   `json:"Status"`
				PrimaryURL       string   `json:"PrimaryURL"`
				Title            string   `json:"Title"`
				CweIDs           []string `json:"CweIDs"`
				References       []string `json:"References"`
				Severity         string   `json:"Severity"`
				PublishedDate    string   `json:"PublishedDate"`
				LastModifiedDate string   `json:"LastModifiedDate"`
			} `json:"Vulnerabilities"`
		} `json:"Results"`
	}

	// JSON 파싱 시도
	if err := json.Unmarshal([]byte(jsonOutput), &trivyResult); err != nil {
		log.Printf("trivy 출력 파싱 실패: %v", err)
		log.Printf("원본 출력: %s", jsonOutput)

		result.Status = "error"
		result.ErrorMsg = fmt.Sprintf("결과 파싱 실패: %v", err)
		return result, nil
	}

	log.Printf("파싱된 trivyResult 구조체: %+v", trivyResult)

	// 취약점 개수 계산
	vulnerabilityCount := 0
	for _, result := range trivyResult.Results {
		vulnerabilityCount += len(result.Vulnerabilities)
		log.Printf("대상 %s에서 %d개의 취약점 발견", result.Target, len(result.Vulnerabilities))
	}

	log.Printf("총 %d개의 취약점 발견", vulnerabilityCount)

	result.Status = "success"
	result.Vulnerabilities = vulnerabilityCount

	// 취약점 상세 정보 추가
	for _, vuln := range trivyResult.Results {
		for _, detail := range vuln.Vulnerabilities {
			vulnDetail := Vulnerability{
				VulnerabilityID:  detail.VulnerabilityID,
				PkgID:            detail.PkgID,
				PkgName:          detail.PkgName,
				InstalledVersion: detail.InstalledVersion,
				FixedVersion:     detail.FixedVersion,
				Status:           detail.Status,
				Severity:         detail.Severity,
				Title:            detail.Title,
				References:       detail.References,
				PublishedDate:    detail.PublishedDate,
				LastModifiedDate: detail.LastModifiedDate,
			}
			result.VulnDetails = append(result.VulnDetails, vulnDetail)
		}
	}

	return result, nil
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
		var request DockerImageRequest
		if err := json.Unmarshal(msg.Body, &request); err != nil {
			log.Printf("요청 파싱 실패: %v, 원본 메시지: %s", err, msg.Body)
			return
		}

		log.Printf("도커 이미지 분석 요청 수신 (기존 형식): JobID=%s, Image=%s:%s",
			request.JobID, request.ImageName, request.Tag)

		// 이미지 분석 실행
		processStandardRequest(ctx, r, &request)
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
	request := &DockerImageRequest{
		JobID:     backendRequest.JobID,
		ImageName: imageName,
		Tag:       tag,
	}

	log.Printf("파싱된 이미지 정보: %s:%s", request.ImageName, request.Tag)

	// 이미지 분석 실행
	processStandardRequest(ctx, r, request)
}

// processStandardRequest는 표준 요청 형식에 대한 이미지 분석을 처리합니다.
func processStandardRequest(ctx context.Context, r *RabbitMQ, request *DockerImageRequest) {
	// 이미지 분석 실행
	analysisCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	result, err := AnalyzeDockerImage(analysisCtx, request)
	if err != nil {
		log.Printf("이미지 분석 실패: %v", err)

		failedResult := &DockerImageResult{
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

func main() {
	flag.Parse()

	// 프로세스 매니저 생성
	pm := NewProcessManager(*pidFile)

	// 중복 실행 체크
	isRunning, err := pm.IsRunning()
	if err != nil {
		log.Printf("프로세스 상태 확인 실패: %v", err)
	}
	if isRunning {
		log.Printf("프로세스가 이미 실행 중입니다")
	}

	// PID 파일 생성 - systemd가 프로세스를 식별하고 관리하기 위해 사용
	if err := pm.WritePID(); err != nil {
		log.Fatalf("pid 파일을 생성할 수 없습니다: %v", err)
	}
	defer pm.RemovePID()

	// 로그 설정 - 표준 출력으로 변경
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// 설정 파일 로드
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("설정 파일 로드 실패: %v", err)
	}

	// RabbitMQ 클라이언트 생성
	rabbitmq, err := NewRabbitMQ(*cfg)
	if err != nil {
		log.Fatalf("rabbitmq 클라이언트 생성 실패: %v", err)
	}
	defer rabbitmq.Close()

	// 시그널 채널 생성
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGTERM, // 종료
		syscall.SIGHUP,  // 설정 리로드
	)

	// 데몬 프로세스 시작 로그
	log.Println("도커 이미지 분석 데몬 서비스가 시작되었습니다.")
	log.Printf("설정 정보: RabbitMQ URL=%s, Exchange=%s", cfg.RabbitMQ.URL, rabbitmq.exchange)
	log.Printf("요청 큐: %s (라우팅 키: %s)", rabbitmq.requestQueue, rabbitmq.requestRouting)
	log.Printf("결과 큐: %s (라우팅 키: %s)", rabbitmq.resultQueue, rabbitmq.resultRouting)

	// 메인 컨텍스트
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 이미지 분석 요청 수신 시작
	err = rabbitmq.ConsumeRequests(ctx)
	if err != nil {
		log.Fatalf("이미지 분석 요청 소비 시작 실패: %v", err)
	}

	log.Println("도커 이미지 분석 요청 대기 중...")

	// 시그널 처리
	for sig := range sigChan {
		switch sig {
		case syscall.SIGTERM:
			log.Printf("종료 시그널 수신: %v", sig)
			cancel()
			return
		case syscall.SIGHUP:
			log.Println("설정 리로드 시그널 수신")
			// 설정 파일 다시 로드
			newCfg, err := config.LoadConfig(*configPath)
			if err != nil {
				log.Printf("설정 파일 리로드 실패: %v", err)
				continue
			}

			// 설정 업데이트 로직
			log.Printf("설정 파일이 리로드되었습니다: %s", *configPath)

			// RabbitMQ 설정 출력
			log.Printf("업데이트된 RabbitMQ 설정: URL=%s, Exchange=%s",
				newCfg.RabbitMQ.URL, newCfg.RabbitMQ.Exchange)
		}
	}
}
