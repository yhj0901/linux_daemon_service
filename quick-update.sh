#!/bin/bash

# 빠른 업데이트 스크립트
echo "=== 리눅스 데몬 서비스 빠른 업데이트 ==="

# 빌드
echo "1. 빌드 중..."
go build -o linux_daemon_service

# 서비스 중지
echo "2. 서비스 중지 중..."
sudo systemctl stop linux_daemon_service

# 바이너리 복사
echo "3. 바이너리 복사 중..."
sudo cp linux_daemon_service /usr/local/bin/
sudo chmod +x /usr/local/bin/linux_daemon_service

# 서비스 다시 시작
echo "4. 서비스 다시 시작 중..."
sudo systemctl daemon-reload
sudo systemctl start linux_daemon_service

# 상태 확인
echo "5. 서비스 상태 확인 중..."
sudo systemctl status linux_daemon_service

echo ""
echo "완료! 로그 확인: sudo journalctl -u linux_daemon_service -f" 