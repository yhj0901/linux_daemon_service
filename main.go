package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"linux_daemon_service/config"
	"linux_daemon_service/pkg/process"
	"linux_daemon_service/pkg/rabbitmq"
)

var (
	configPath = flag.String("config", "config/config.yaml", "설정 파일 경로")
	pidFile    = flag.String("pid", "/var/run/linux_daemon_service.pid", "PID 파일 경로")
)

func main() {
	flag.Parse()

	// 프로세스 매니저 생성
	pm := process.NewProcessManager(*pidFile)

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
	rabbitmqClient, err := rabbitmq.NewRabbitMQ(*cfg)
	if err != nil {
		log.Fatalf("rabbitmq 클라이언트 생성 실패: %v", err)
	}
	defer rabbitmqClient.Close()

	// 시그널 채널 생성
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGTERM, // 종료
		syscall.SIGHUP,  // 설정 리로드
	)

	// 데몬 프로세스 시작 로그
	log.Println("도커 이미지 분석 데몬 서비스가 시작되었습니다.")
	log.Printf("설정 정보: RabbitMQ URL=%s, Exchange=%s", cfg.RabbitMQ.URL, cfg.RabbitMQ.Exchange)
	log.Printf("요청 큐: %s (라우팅 키: %s)", cfg.RabbitMQ.RequestQueue, cfg.RabbitMQ.RequestRoutingKey)
	log.Printf("결과 큐: %s (라우팅 키: %s)", cfg.RabbitMQ.ResultQueue, cfg.RabbitMQ.ResultRoutingKey)

	// 메인 컨텍스트
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 이미지 분석 요청 수신 시작
	err = rabbitmqClient.ConsumeRequests(ctx)
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
