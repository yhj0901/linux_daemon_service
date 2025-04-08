#!/bin/bash

# 색상 정의
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# 함수: 성공 메시지
success() {
    echo -e "${GREEN}✓ $1${NC}"
}

# 함수: 경고 메시지
warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

# 함수: 오류 메시지
error() {
    echo -e "${RED}✗ $1${NC}"
    exit 1
}

# 루트 권한 확인
if [ "$(id -u)" != "0" ]; then
   error "이 스크립트는 루트 권한으로 실행해야 합니다. 'sudo $0'을 사용하세요."
fi

echo "=== 리눅스 데몬 서비스 업데이트 ==="

# 현재 디렉토리 확인
if [ ! -f "main.go" ]; then
    error "스크립트는 프로젝트 루트 디렉토리에서 실행해야 합니다."
fi

# 1. 빌드
echo "1. 데몬 서비스 빌드 중..."
go build -o linux_daemon_service || error "빌드 실패"
success "빌드 완료"

# 2. 서비스 중지
echo "2. 기존 서비스 중지 중..."
systemctl stop linux_daemon_service
if [ $? -eq 0 ]; then
    success "서비스 중지 완료"
else
    warning "서비스가 실행 중이지 않거나 중지 실패"
fi

# 3. 바이너리 교체
echo "3. 바이너리 교체 중..."
cp linux_daemon_service /usr/local/bin/ || error "바이너리 복사 실패"
chmod +x /usr/local/bin/linux_daemon_service || error "실행 권한 설정 실패"
success "바이너리 교체 완료"

# 4. 설정 파일 교체
echo "4. 설정 파일 교체 중..."
if [ -d "config" ] && [ -f "config/config.yaml" ]; then
    mkdir -p /etc/linux_daemon_service
    cp config/config.yaml /etc/linux_daemon_service/ || error "설정 파일 복사 실패"
    success "설정 파일 교체 완료"
else
    warning "config/config.yaml 파일이 없습니다. 설정 파일 교체를 건너뜁니다."
fi

# 5. 서비스 파일 교체
echo "5. 서비스 파일 교체 중..."
if [ -f "linux_daemon_service.service" ]; then
    cp linux_daemon_service.service /etc/systemd/system/ || error "서비스 파일 복사 실패"
    success "서비스 파일 교체 완료"
else
    warning "linux_daemon_service.service 파일이 없습니다. 서비스 파일 교체를 건너뜁니다."
fi

# 6. 서비스 다시 로드 및 시작
echo "6. 서비스 다시 로드 및 시작 중..."
systemctl daemon-reload || error "서비스 다시 로드 실패"
systemctl enable linux_daemon_service || error "서비스 활성화 실패"
systemctl start linux_daemon_service || error "서비스 시작 실패"
success "서비스 다시 로드 및 시작 완료"

# 7. 서비스 상태 확인
echo "7. 서비스 상태 확인 중..."
systemctl status linux_daemon_service
if [ $? -eq 0 ]; then
    success "서비스가 정상적으로 실행 중입니다."
else
    error "서비스 실행 상태가 비정상입니다. 로그를 확인하세요: journalctl -u linux_daemon_service -f"
fi

echo ""
echo "업데이트가 완료되었습니다. 다음 명령으로 로그를 확인할 수 있습니다:"
echo "  journalctl -u linux_daemon_service -f" 