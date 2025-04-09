#!/bin/bash

# 환경 변수 로드
source ./setup-env.sh

# 필요한 패키지 확인
if ! command -v jq &> /dev/null; then
    echo "jq가 설치되어 있지 않습니다. 설치가 필요합니다."
    echo "설치 방법: sudo apt-get install jq"
    exit 1
fi

if ! command -v amqp-publish &> /dev/null; then
    echo "amqp-utils가 설치되어 있지 않습니다. 설치가 필요합니다."
    echo "설치 방법: gem install amqp-utils"
    exit 1
fi

# 테스트할 도커 이미지
TEST_IMAGE="ubuntu:latest"

# 현재 시간을 ISO 형식으로 (2023-04-09T06:50:32+00:00)
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%S.%3N+00:00")
JOB_ID=$(cat /proc/sys/kernel/random/uuid)

# 백엔드 형식의 JSON 메시지 생성 
MESSAGE=$(jq -n \
    --arg job_id "$JOB_ID" \
    --arg image "$TEST_IMAGE" \
    --arg created_at "$TIMESTAMP" \
    '{job_id: $job_id, image_url: $image, created_at: $created_at, action: "analyze_docker_image"}')

echo "다음 메시지를 전송합니다:"
echo $MESSAGE | jq .

# RabbitMQ에 메시지 전송
echo "요청 큐 ($RABBITMQ_REQUEST_QUEUE)에 메시지 전송 중..."
echo $MESSAGE | amqp-publish --url="$RABBITMQ_URL" \
    --exchange="$RABBITMQ_EXCHANGE" \
    --routing_key="$RABBITMQ_REQUEST_ROUTING_KEY" \
    --vhost="/"

echo "메시지가 전송되었습니다. 결과 큐를 확인하려면 다음 명령을 실행하세요:"
echo "amqp-consume --url=\"$RABBITMQ_URL\" --queue=\"$RABBITMQ_RESULT_QUEUE\" --vhost=\"/\""

# 선택적으로 결과 큐 확인
read -p "결과 큐를 확인하시겠습니까? (y/n): " CHECK_RESULTS
if [[ "$CHECK_RESULTS" == "y" ]]; then
    echo "결과 큐 ($RABBITMQ_RESULT_QUEUE)를 확인 중... (중단하려면 Ctrl+C를 누르세요)"
    amqp-consume --url="$RABBITMQ_URL" --queue="$RABBITMQ_RESULT_QUEUE" --vhost="/"
fi 