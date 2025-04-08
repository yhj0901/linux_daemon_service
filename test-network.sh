#!/bin/bash

# 연결 테스트를 위한 RabbitMQ 서버 정보
RABBITMQ_SERVER="10.0.0.46"
RABBITMQ_PORT="5672"

echo "RabbitMQ 서버 연결 테스트 시작: $RABBITMQ_SERVER:$RABBITMQ_PORT"
echo "---------------------------------------------------------"

# 1. 기본 네트워크 정보
echo "[1] 네트워크 인터페이스 정보:"
ip addr show | grep -E "inet|eth|ens"
echo ""

# 2. 라우팅 테이블 확인
echo "[2] 라우팅 테이블:"
ip route
echo ""

# 3. RabbitMQ 서버 IP로 ping 테스트
echo "[3] Ping 테스트 ($RABBITMQ_SERVER):"
ping -c 4 $RABBITMQ_SERVER
PING_STATUS=$?
if [ $PING_STATUS -eq 0 ]; then
    echo "✅ Ping 성공: $RABBITMQ_SERVER에 연결할 수 있습니다."
else
    echo "❌ Ping 실패: $RABBITMQ_SERVER에 연결할 수 없습니다."
    echo "  - Security Group 및 NACL 설정을 확인하세요."
    echo "  - 방화벽 설정을 확인하세요."
fi
echo ""

# 4. 포트 연결 테스트
echo "[4] 포트 연결 테스트 ($RABBITMQ_SERVER:$RABBITMQ_PORT):"
nc -zv $RABBITMQ_SERVER $RABBITMQ_PORT -w 5 2>&1
NC_STATUS=$?
if [ $NC_STATUS -eq 0 ]; then
    echo "✅ 포트 연결 성공: $RABBITMQ_SERVER:$RABBITMQ_PORT에 연결할 수 있습니다."
else
    echo "❌ 포트 연결 실패: $RABBITMQ_SERVER:$RABBITMQ_PORT에 연결할 수 없습니다."
    echo "  - RabbitMQ 서버가 실행 중인지 확인하세요."
    echo "  - 보안 그룹 설정에서 포트를 허용했는지 확인하세요."
fi
echo ""

# 5. 종합 결과
echo "[5] 종합 결과:"
if [ $PING_STATUS -eq 0 ] && [ $NC_STATUS -eq 0 ]; then
    echo "✅ 모든 테스트 통과: RabbitMQ 서버에 정상적으로 연결할 수 있습니다."
    echo "  - 데몬 서비스를 시작할 수 있습니다."
else
    echo "❌ 일부 테스트 실패: RabbitMQ 서버 연결에 문제가 있습니다."
    echo "  - AWS 콘솔에서 다음 사항을 확인하세요:"
    echo "    1. 두 인스턴스가 같은 VPC에 있는지 확인"
    echo "    2. 보안 그룹 설정에서 RabbitMQ 포트($RABBITMQ_PORT)를 허용했는지 확인"
    echo "    3. 네트워크 ACL 설정 확인"
    echo "    4. 라우팅 테이블이 서브넷 간 통신을 허용하는지 확인"
    echo ""
    echo "  - RabbitMQ 서버에서 다음 사항을 확인하세요:"
    echo "    1. RabbitMQ 서비스가 실행 중인지 확인 (systemctl status rabbitmq-server)"
    echo "    2. RabbitMQ가 모든 인터페이스에서 연결을 수락하도록 설정되었는지 확인"
fi 