#!/bin/bash

# RabbitMQ 환경 변수 설정
export RABBITMQ_URL="amqp://guest:guest@10.0.0.46:5672/"
export RABBITMQ_EXCHANGE="image_analysis"
export RABBITMQ_QUEUE="go_analysis_queue"
export RABBITMQ_ROUTING_KEY="analysis.results"

# 요청 및 결과 큐 설정
export RABBITMQ_REQUEST_QUEUE="image_analysis_requests"
export RABBITMQ_REQUEST_ROUTING_KEY="analysis.requests"
export RABBITMQ_RESULT_QUEUE="image_analysis_results"
export RABBITMQ_RESULT_ROUTING_KEY="analysis.results"

# 테스트 메시지 활성화 (테스트 시에만 true로 설정)
export ENABLE_TEST_MESSAGE="false"

echo "RabbitMQ 환경 변수가 설정되었습니다."
echo "현재 쉘에 환경 변수가 로드되었습니다. systemd 서비스에 적용하려면 다음 명령을 실행하세요:"
echo ""
echo "sudo systemctl edit linux_daemon_service.service"
echo ""
echo "다음 내용을 추가하세요:"
echo "[Service]"
echo "Environment=\"RABBITMQ_URL=${RABBITMQ_URL}\""
echo "Environment=\"RABBITMQ_EXCHANGE=${RABBITMQ_EXCHANGE}\""
echo "Environment=\"RABBITMQ_REQUEST_QUEUE=${RABBITMQ_REQUEST_QUEUE}\""
echo "Environment=\"RABBITMQ_REQUEST_ROUTING_KEY=${RABBITMQ_REQUEST_ROUTING_KEY}\""
echo "Environment=\"RABBITMQ_RESULT_QUEUE=${RABBITMQ_RESULT_QUEUE}\""
echo "Environment=\"RABBITMQ_RESULT_ROUTING_KEY=${RABBITMQ_RESULT_ROUTING_KEY}\""
echo ""
echo "편집 후 서비스를 다시 시작하세요:"
echo "sudo systemctl daemon-reload"
echo "sudo systemctl restart linux_daemon_service.service" 