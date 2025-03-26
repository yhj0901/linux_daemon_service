# Linux Daemon Service

이 프로젝트는 Linux 데몬 프로세스 생성 예제를 보여주는 Go 프로그램입니다.

## 설명

이 프로젝트는 Linux 시스템에서 데몬 프로세스를 생성하고 관리하는 방법을 보여주는 예제 코드를 포함하고 있습니다.

## 시작하기

### 사전 요구사항
- Go 1.16 이상
- Linux 운영체제
- systemd (서비스 관리용)

### 설치

```bash
git clone https://github.com/yourusername/linux_daemon_service.git
cd linux_daemon_service
go mod tidy
```

### 설정 파일

프로젝트는 `config/config.yaml` 파일을 통해 설정을 관리합니다.

```yaml
# Backend 설정
backend:
  url: "http://localhost:8080/api"  # Backend 서버 URL
  timeout: 30                       # API 요청 타임아웃 (초)
  retryCnt: 3                       # API 요청 재시도 횟수

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
go run main.go -config config/config.yaml
```

## 라이선스

이 프로젝트는 MIT 라이선스 하에 있습니다. 