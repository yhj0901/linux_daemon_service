# Linux Daemon Service

이 프로젝트는 Linux 데몬 프로세스 생성 예제를 보여주는 Go 프로그램입니다.

## 설명

이 프로젝트는 Linux 시스템에서 데몬 프로세스를 생성하고 관리하는 방법을 보여주는 예제 코드를 포함하고 있습니다.

## 프로젝트 구조

프로젝트는 다음과 같은 패키지로 구조화되어 있습니다:

```
linux_daemon_service/
├── config/               # 설정 파일 및 설정 처리 코드
│   ├── config.go         # 설정 파일 처리 로직
│   └── config.yaml       # 환경 설정 파일
├── pkg/                  # 분리된 패키지 디렉토리
│   ├── process/          # 프로세스 관리 관련 코드
│   │   └── process.go    # 프로세스 관리 기능 구현 (PID 파일 관리 등)
│   ├── rabbitmq/         # RabbitMQ 관련 코드
│   │   └── rabbitmq.go   # RabbitMQ 연결 및 메시지 처리 로직
│   └── trivy/            # Trivy 이미지 분석 관련 코드
│       └── trivy.go      # Trivy를 이용한 이미지 분석 기능 구현
├── main.go               # 메인 애플리케이션 코드
└── ... (기타 파일들)      # 서비스 파일, 스크립트 등
```

### 패키지 소개

- **config**: 설정 파일 로드 및 설정 구조체 정의
- **pkg/process**: 데몬 프로세스 관리를 위한 기능 (PID 파일 관리, 시그널 처리 등)
- **pkg/rabbitmq**: RabbitMQ 연결 및 메시지 처리 (메시지 수신 및 발행)
- **pkg/trivy**: Docker 이미지 취약점 분석 관련 기능 구현

## 시작하기

### 사전 요구사항
- Go 1.16 이상
- Linux 운영체제
- systemd (서비스 관리용)
- RabbitMQ 서버

### 설치

```bash
git clone https://github.com/yourusername/linux_daemon_service.git
cd linux_daemon_service
go mod tidy
```

### 패키지 설치 및 빌드

```bash
# 외부 패키지 설치
go get github.com/rabbitmq/amqp091-go
go get gopkg.in/yaml.v3

# 의존성 업데이트
go mod tidy

# 빌드
go build -o linux_daemon_service .
```

### 설정 파일

프로젝트는 `config/config.yaml` 파일을 통해 설정을 관리합니다.

```yaml
# Backend 설정
backend:
  url: "http://localhost:8080/api"  # Backend 서버 URL
  timeout: 30                       # API 요청 타임아웃 (초)
  retryCnt: 3                       # API 요청 재시도 횟수

# RabbitMQ 설정
rabbitmq:
  url: "${RABBITMQ_URL:-amqp://guest:guest@localhost:5672/}"  # 환경 변수 또는 기본값 사용
  exchange: "${RABBITMQ_EXCHANGE:-image_analysis}"            # 환경 변수 또는 기본값 사용
  queue: "${RABBITMQ_QUEUE:-go_analysis_queue}"               # 환경 변수 또는 기본값 사용
  routingKey: "${RABBITMQ_ROUTING_KEY:-analysis.results}"     # 환경 변수 또는 기본값 사용

# 로깅 설정
logging:
  level: "info"                     # 로그 레벨 (debug, info, warn, error)
  file: "/var/log/linux_daemon_service.log"
  maxSize: 100                      # 로그 파일 최대 크기 (MB)
  maxBackups: 3                     # 백업 파일 최대 개수
  maxAge: 7                         # 로그 파일 보관 기간 (일)

# 모니터링 설정
monitoring:
  enabled: true                     # 모니터링 활성화 여부
  metricsPort: 9090                 # 메트릭 수집 포트
  endpoint: "/metrics"              # 메트릭 엔드포인트
  process:
    name: "trivy"                   # 모니터링할 프로세스 이름
    cpuLimit: 80.0                  # CPU 사용량 제한 (%)
    memoryLimit: 1024              # 메모리 사용량 제한 (MB)
    timeout: 300                   # 프로세스 실행 타임아웃 (초)
    checkInterval: 5               # 모니터링 체크 간격 (초)
```

### 네트워크 연결 테스트

EC2 인스턴스 간 통신 문제를 해결하기 위해 네트워크 연결 테스트 스크립트를 사용합니다:

```bash
# 테스트 스크립트 실행 권한 부여
chmod +x test-network.sh

# 네트워크 테스트 실행
./test-network.sh
```

> 주의: EC2 인스턴스끼리 서로 통신하려면 같은 VPC 안에서도 서브넷(subnet)과 라우팅 설정에 따라 통신 여부가 결정됩니다. 서브넷 간 라우팅이 올바르게 설정되었는지 확인하세요.

### 환경 변수 설정

RabbitMQ 연결 정보를 환경 변수로 설정할 수 있습니다:

```bash
# 환경 변수 설정 스크립트 실행 권한 부여
chmod +x setup-env.sh

# 환경 변수 설정
source setup-env.sh
```

### 서비스 등록

1. 서비스 파일 생성 (`/etc/systemd/system/linux_daemon_service.service`):

```ini
[Unit]
Description=Linux Daemon Service
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/linux_daemon_service -config /etc/linux_daemon_service/config.yaml

# RabbitMQ 환경 변수
Environment="RABBITMQ_URL=amqp://guest:guest@10.0.2.194:5672/"
Environment="RABBITMQ_EXCHANGE=image_analysis"
Environment="RABBITMQ_QUEUE=go_analysis_queue"
Environment="RABBITMQ_ROUTING_KEY=analysis.results"

# 재시작 설정
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

2. 바이너리 설치:
```bash
sudo cp linux_daemon_service /usr/local/bin/
sudo chmod +x /usr/local/bin/linux_daemon_service
```

3. 설정 파일 설치:
```bash
sudo mkdir -p /etc/linux_daemon_service
sudo cp config/config.yaml /etc/linux_daemon_service/
```

4. 서비스 등록 및 활성화:
```bash
sudo systemctl daemon-reload
sudo systemctl enable linux_daemon_service
sudo systemctl start linux_daemon_service
```

### 서비스 관리

```bash
# 서비스 상태 확인
sudo systemctl status linux_daemon_service

# 서비스 시작
sudo systemctl start linux_daemon_service

# 서비스 중지
sudo systemctl stop linux_daemon_service

# 서비스 재시작
sudo systemctl restart linux_daemon_service

# 로그 확인
sudo journalctl -u linux_daemon_service -f
```

### 개발 모드 실행

```bash
# 환경 변수와 함께 실행
export RABBITMQ_URL="amqp://guest:guest@10.0.2.194:5672/"
go run main.go -config config/config.yaml
```

## 문제 해결

### RabbitMQ 연결 실패

"connection refused" 오류가 발생하면 다음을 확인하세요:

1. **AWS VPC 라우팅 테이블**: 서브넷 간 통신이 가능한지 확인 (`10.0.0.0/16 → local`)
2. **보안 그룹(Security Group)**: RabbitMQ 서버의 인바운드 규칙에서 포트 5672를 허용했는지 확인
3. **Network ACL**: 서브넷 간 트래픽을 막지 않는지 확인
4. **RabbitMQ 서버 상태**: `systemctl status rabbitmq-server`로 서버가 실행 중인지 확인

## 라이선스

이 프로젝트는 MIT 라이선스 하에 있습니다. 