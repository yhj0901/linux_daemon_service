package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
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

// 메시지 구조체
type AnalysisMessage struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
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
	conn       *amqp.Connection
	channel    *amqp.Channel
	config     config.Config
	exchange   string
	routingKey string
	queueName  string
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
	queueName := getEnvOrConfig("RABBITMQ_QUEUE", cfg.RabbitMQ.Queue)
	routingKey := getEnvOrConfig("RABBITMQ_ROUTING_KEY", cfg.RabbitMQ.RoutingKey)

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

	// Queue 선언
	_, err = ch.QueueDeclare(
		queueName, // 이름
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("queue 선언 실패: %v", err)
	}

	// Queue와 Exchange 바인딩
	err = ch.QueueBind(
		queueName,  // queue name
		routingKey, // routing key
		exchange,   // exchange
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("queue 바인딩 실패: %v", err)
	}

	return &RabbitMQ{
		conn:       conn,
		channel:    ch,
		config:     cfg,
		exchange:   exchange,
		routingKey: routingKey,
		queueName:  queueName,
	}, nil
}

// PublishJSONMessage는 JSON 메시지를 발행합니다.
func (r *RabbitMQ) PublishJSONMessage(ctx context.Context, message interface{}) error {
	// 메시지를 JSON으로 인코딩
	jsonData, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("json 인코딩 실패: %v", err)
	}

	return r.channel.PublishWithContext(
		ctx,
		r.exchange,   // exchange
		r.routingKey, // routing key
		false,        // mandatory
		false,        // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        jsonData,
		},
	)
}

// Consume은 메시지를 소비하고 JSON으로 파싱합니다.
func (r *RabbitMQ) Consume(ctx context.Context, handler func(msg *AnalysisMessage)) error {
	msgs, err := r.channel.Consume(
		r.queueName, // queue
		"",          // consumer
		true,        // auto-ack
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		nil,         // args
	)
	if err != nil {
		return fmt.Errorf("메시지 소비 실패: %v", err)
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

				// JSON 메시지 파싱
				var analysisMsg AnalysisMessage
				if err := json.Unmarshal(msg.Body, &analysisMsg); err != nil {
					log.Printf("json 파싱 실패: %v, 원본 메시지: %s", err, msg.Body)
					continue
				}

				// 파싱된 메시지 처리
				handler(&analysisMsg)
			}
		}
	}()

	return nil
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
	log.Println("데몬 서비스가 시작되었습니다.")
	log.Printf("설정 정보: RabbitMQ URL=%s, Exchange=%s, RoutingKey=%s, Queue=%s",
		cfg.RabbitMQ.URL, rabbitmq.exchange, rabbitmq.routingKey, rabbitmq.queueName)

	// 메인 컨텍스트
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 메시지 소비 시작 - JSON 메시지 처리
	err = rabbitmq.Consume(ctx, func(msg *AnalysisMessage) {
		log.Printf("분석 메시지 수신: JobID=%s, Status=%s", msg.JobID, msg.Status)
	})
	if err != nil {
		log.Fatalf("메시지 소비 시작 실패: %v", err)
	}

	// 메시지 발행 테스트 - 파이썬과 동일한 형식으로 전송
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// 파이썬과 같은 형식의 메시지 생성
				message := AnalysisMessage{
					JobID:  fmt.Sprintf("job-%d", time.Now().Unix()),
					Status: "processing",
				}

				err := rabbitmq.PublishJSONMessage(ctx, message)
				if err != nil {
					log.Printf("메시지 발행 실패: %v", err)
				} else {
					log.Printf("메시지 발행 성공: %+v", message)
				}
				time.Sleep(10 * time.Second)
			}
		}
	}()

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

			// 여기에 설정 업데이트 로직 구현
			log.Printf("설정 파일이 리로드되었습니다: %s", *configPath)

			// 예시로 RabbitMQ 설정 출력
			log.Printf("업데이트된 RabbitMQ 설정: URL=%s, Exchange=%s",
				newCfg.RabbitMQ.URL, rabbitmq.exchange)
		}
	}
}
