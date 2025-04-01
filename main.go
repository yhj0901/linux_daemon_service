package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yhj0901/rabbitmq-bridge/go-client/pkg/rabbitmq"
)

var (
	configPath = flag.String("config", "", "설정 파일 경로")
	pidFile    = flag.String("pid", "/var/run/linux_daemon_service.pid", "PID 파일 경로")
)

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
		return 0, fmt.Errorf("PID 파일을 읽을 수 없습니다: %v", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		return 0, fmt.Errorf("PID 파일의 내용이 올바르지 않습니다: %v", err)
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

func sendMessageToRabbitMQ() {
	config := rabbitmq.Config{
		Host:     "10.0.2.194",
		Port:     5672,
		Username: "guest",
		Password: "guest",
		VHost:    "/",
	}

	rabbitmqClient, err := rabbitmq.NewClient(config)
	if err != nil {
		log.Fatalf("RabbitMQ 클라이언트 생성 실패: %v", err)
	}

	// Exchange 선언
	if err := rabbitmqClient.DeclareExchange("test", "direct"); err != nil {
		log.Fatalf("Exchange 선언 실패: %v", err)
	}

	// Queue 선언
	if err := rabbitmqClient.DeclareQueue("request_queue"); err != nil {
		log.Fatalf("Queue 선언 실패: %v", err)
	}

	// Queue와 Exchange 바인딩
	if err := rabbitmqClient.BindQueue("request_queue", "test", "request_key"); err != nil {
		log.Fatalf("Queue와 Exchange 바인딩 실패: %v", err)
	}

	// 응답 큐 선언
	replyQueue, err := rabbitmqClient.DeclareReplyQueue()
	if err != nil {
		log.Fatalf("응답 큐 선언 실패: %v", err)
	}

	log.Printf("응답 큐 생성됨: %s", replyQueue)

	// 요청 메시지
	message := []byte("한번 요청해보자!!!")
	log.Printf("요청 메시지: %s", message)

	// 상관 ID 생성
	correlationID := fmt.Sprintf("go-Daemon-Service-%d", time.Now().UnixNano())

	// 컨텍스트 생성
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 요청 전송
	err = rabbitmqClient.PublishWithOptions(ctx, "test", "request_key", message, rabbitmq.PublishOptions{
		ReplyTo:       replyQueue,
		CorrelationID: correlationID,
	})
	if err != nil {
		log.Fatalf("메시지 전송 실패: %v", err)
	}
	log.Printf("메시지 전송 성공: %s", correlationID)

	// 응답 수신
	response, err := rabbitmqClient.WaitForResponse(ctx, replyQueue, correlationID)
	if err != nil {
		log.Fatalf("응답 수신 실패: %v", err)
	}

	log.Printf("응답 수신 성공: %s", response)

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
		log.Fatalf("PID 파일을 생성할 수 없습니다: %v", err)
	}
	defer pm.RemovePID()

	// 로그 설정 - 표준 출력으로 변경
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// 시그널 채널 생성
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGTERM, // 종료
		syscall.SIGHUP,  // 설정 리로드
	)

	// 데몬 프로세스 시작 로그
	log.Println("데몬 서비스가 시작되었습니다.")

	// 설정 파일 로드 (필요한 경우)
	if *configPath != "" {
		log.Printf("설정 파일 로드 중: %s", *configPath)
		// TODO: 설정 파일 로드 로직 구현
	}

	// 메인 작업 고루틴
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-done:
				return
			default:
				// rabbitmq sender 실행 10초에 한번씩 메시지 전송
				go sendMessageToRabbitMQ()
				log.Println("데몬 서비스가 실행 중입니다...")
				time.Sleep(10 * time.Second)
			}
		}
	}()

	// 시그널 처리
	for sig := range sigChan {
		switch sig {
		case syscall.SIGTERM:
			log.Printf("종료 시그널 수신: %v", sig)
			close(done)
			return
		case syscall.SIGHUP:
			log.Println("설정 리로드 시그널 수신")
			// TODO: 설정 리로드 로직 구현
		}
	}
}
