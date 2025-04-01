FROM golang:1.17-alpine AS builder

# 작업 디렉토리 설정
WORKDIR /app

# 의존성 파일 복사 및 다운로드
COPY go.mod go.sum ./
RUN go mod download

# 소스 코드 복사
COPY . .

# 애플리케이션 빌드
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o linux_daemon_service .

# 실행 이미지 생성
FROM alpine:latest

# 필요한 패키지 설치
RUN apk --no-cache add ca-certificates tzdata

# 바이너리 복사
COPY --from=builder /app/linux_daemon_service /usr/local/bin/

# 설정 파일 복사
COPY --from=builder /app/config/config.yaml /etc/linux_daemon_service/config.yaml

# 실행 권한 설정
RUN chmod +x /usr/local/bin/linux_daemon_service

# 로그 디렉토리 생성
RUN mkdir -p /var/log/linux_daemon_service

# 실행
CMD ["/usr/local/bin/linux_daemon_service", "-config", "/etc/linux_daemon_service/config.yaml"] 