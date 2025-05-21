package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"linux_daemon_service/config"
	"linux_daemon_service/pkg/trivy"
)

// BackendClient는 백엔드 API와 통신하는 클라이언트입니다.
type BackendClient struct {
	httpClient *http.Client
	baseURL    string
	config     config.Config
}

// AnalysisResultRequest는 분석 결과를 백엔드 API로 전송하는 요청 구조체입니다.
type AnalysisResultRequest struct {
	JobID           string                `json:"job_id"`
	ImageURL        string                `json:"image_url"`
	Status          string                `json:"status"`          // success, error
	Vulnerabilities int                   `json:"vulnerabilities"` // 취약점 개수
	CompletedAt     string                `json:"completed_at"`
	ErrorMsg        string                `json:"error_msg,omitempty"`
	Action          string                `json:"action"`
	VulnDetails     []trivy.Vulnerability `json:"vuln_details,omitempty"` // 취약점 상세 정보
}

// NewClient는 새로운 BackendClient를 생성합니다.
func NewClient(cfg config.Config) *BackendClient {
	timeout := time.Duration(cfg.Backend.Timeout) * time.Second
	client := &http.Client{
		Timeout: timeout,
	}

	return &BackendClient{
		httpClient: client,
		baseURL:    cfg.Backend.URL,
		config:     cfg,
	}
}

// SendAnalysisResult는 도커 이미지 분석 결과를 백엔드 API로 전송합니다.
func (c *BackendClient) SendAnalysisResult(ctx context.Context, result *trivy.DockerImageResult) error {
	// 백엔드 요청 메시지로 변환
	backendRequest := &AnalysisResultRequest{
		JobID:           result.JobID,
		ImageURL:        fmt.Sprintf("%s:%s", result.ImageName, result.Tag),
		Status:          result.Status,
		Vulnerabilities: result.Vulnerabilities,
		CompletedAt:     result.CompletedAt.Format(time.RFC3339),
		ErrorMsg:        result.ErrorMsg,
		Action:          "analyze_docker_image_result",
		VulnDetails:     result.VulnDetails,
	}

	// 메시지를 JSON으로 인코딩
	jsonData, err := json.Marshal(backendRequest)
	if err != nil {
		return fmt.Errorf("json 인코딩 실패: %v", err)
	}

	// API 엔드포인트 URL 생성
	url := fmt.Sprintf("%s/image-analysis/results", c.baseURL)

	log.Printf("분석 결과 API 전송: %s", url)
	log.Printf("분석 결과 내용: %s", string(jsonData))

	// 최대 재시도 횟수
	maxRetries := c.config.Backend.RetryCnt

	// 초기 재시도 간격 (밀리초)
	retryInterval := 1000

	// API 호출 및 재시도 로직
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// HTTP 요청 생성
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return fmt.Errorf("HTTP 요청 생성 실패: %v", err)
		}

		// 헤더 설정
		req.Header.Set("Content-Type", "application/json")

		// 요청 전송
		resp, err := c.httpClient.Do(req)

		// 요청 성공 시
		if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusAccepted) {
			defer resp.Body.Close()
			log.Printf("분석 결과 API 전송 성공: 상태 코드 %d", resp.StatusCode)
			return nil
		}

		// 에러 로깅
		if err != nil {
			log.Printf("분석 결과 API 전송 시도 %d/%d 실패: %v", attempt+1, maxRetries+1, err)
		} else {
			log.Printf("분석 결과 API 전송 시도 %d/%d 실패: 상태 코드 %d", attempt+1, maxRetries+1, resp.StatusCode)
			resp.Body.Close()
		}

		// 마지막 시도였으면 에러 반환
		if attempt == maxRetries {
			if err != nil {
				return fmt.Errorf("분석 결과 API 전송 실패 (최대 재시도 횟수 초과): %v", err)
			}
			return fmt.Errorf("분석 결과 API 전송 실패 (최대 재시도 횟수 초과): 상태 코드 %d", resp.StatusCode)
		}

		// 다음 재시도 전 대기 (지수 백오프)
		sleepTime := time.Duration(retryInterval*(1<<uint(attempt))) * time.Millisecond
		log.Printf("%v 후 재시도합니다...", sleepTime)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepTime):
			// 재시도 준비
		}
	}

	return fmt.Errorf("알 수 없는 오류: 재시도 로직에서 빠져나옴")
}
