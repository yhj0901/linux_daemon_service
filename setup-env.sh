#!/bin/bash

# RabbitMQ 환경 변수 설정
export RABBITMQ_URL="amqp://guest:guest@10.0.0.46:5672/"
export RABBITMQ_EXCHANGE="image_analysis"
export RABBITMQ_QUEUE="go_analysis_queue"
export RABBITMQ_ROUTING_KEY="analysis.results"

# 현재 설정 표시
echo "환경 변수가 설정되었습니다:"
echo "RABBITMQ_URL = $RABBITMQ_URL"
echo "RABBITMQ_EXCHANGE = $RABBITMQ_EXCHANGE"
echo "RABBITMQ_QUEUE = $RABBITMQ_QUEUE"
echo "RABBITMQ_ROUTING_KEY = $RABBITMQ_ROUTING_KEY"

# 실행 방법 안내
echo ""
echo "다음 명령으로 환경 변수를 로드하세요:"
echo "source setup-env.sh"
echo ""
echo "또는 systemd 서비스에 추가하려면 다음 단계를 따르세요:"
echo "1. /etc/systemd/system/linux_daemon_service.service 파일 수정"
echo "2. [Service] 섹션에 다음 내용 추가:"
echo "   Environment=\"RABBITMQ_URL=$RABBITMQ_URL\""
echo "   Environment=\"RABBITMQ_EXCHANGE=$RABBITMQ_EXCHANGE\""
echo "   Environment=\"RABBITMQ_QUEUE=$RABBITMQ_QUEUE\""
echo "   Environment=\"RABBITMQ_ROUTING_KEY=$RABBITMQ_ROUTING_KEY\""
echo "3. systemctl daemon-reload 실행"
echo "4. systemctl restart linux_daemon_service 실행" 