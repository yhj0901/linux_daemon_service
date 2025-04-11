package trivy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DockerImageRequest 도커 이미지 분석 요청 메시지 구조체
type DockerImageRequest struct {
	JobID     string `json:"job_id"`
	ImageName string `json:"image_name"`
	Tag       string `json:"tag"`
}

// DockerImageResult 도커 이미지 분석 결과 메시지 구조체
type DockerImageResult struct {
	JobID           string          `json:"job_id"`
	ImageName       string          `json:"image_name"`
	Tag             string          `json:"tag"`
	Status          string          `json:"status"`          // success, error
	Vulnerabilities int             `json:"vulnerabilities"` // 취약점 개수
	CompletedAt     time.Time       `json:"completed_at"`
	ErrorMsg        string          `json:"error_msg,omitempty"`
	VulnDetails     []Vulnerability `json:"vuln_details,omitempty"` // 취약점 상세 정보
}

// Vulnerability 취약점 상세 정보 구조체
type Vulnerability struct {
	VulnerabilityID  string   `json:"vulnerability_id"`
	PkgID            string   `json:"pkg_id"`
	PkgName          string   `json:"pkg_name"`
	InstalledVersion string   `json:"installed_version"`
	FixedVersion     string   `json:"fixed_version"`
	Status           string   `json:"status"`
	Severity         string   `json:"severity"`
	Title            string   `json:"title"`
	Description      string   `json:"description,omitempty"`
	References       []string `json:"references,omitempty"`
	PublishedDate    string   `json:"published_date,omitempty"`
	LastModifiedDate string   `json:"last_modified_date,omitempty"`
}

// AnalyzeDockerImage는 Trivy를 사용하여 도커 이미지를 분석합니다.
func AnalyzeDockerImage(ctx context.Context, req *DockerImageRequest) (*DockerImageResult, error) {
	fmt.Println("AnalyzeDockerImage 실행 :", ctx)
	imageFullName := fmt.Sprintf("%s:%s", req.ImageName, req.Tag)

	result := &DockerImageResult{
		JobID:     req.JobID,
		ImageName: req.ImageName,
		Tag:       req.Tag,
		Status:    "processing",
	}

	// 사용자 홈 디렉토리 가져오기
	homeDir, err := os.UserHomeDir()
	if err != nil {
		result.Status = "error"
		result.ErrorMsg = fmt.Sprintf("사용자 홈 디렉토리를 찾을 수 없습니다: %v", err)
		return result, nil
	}

	// 시스템 아키텍처 확인
	arch := runtime.GOARCH
	osType := runtime.GOOS
	log.Printf("시스템 아키텍처: %s, OS: %s", arch, osType)

	// 아키텍처에 맞는 trivy 파일명 결정
	var trivyFileName string
	switch {
	case arch == "amd64" && osType == "linux":
		trivyFileName = "trivy_linux_amd64"
	case arch == "arm64" && osType == "linux":
		trivyFileName = "trivy_linux_arm64"
	default:
		result.Status = "error"
		result.ErrorMsg = fmt.Sprintf("지원하지 않는 아키텍처입니다: %s/%s", osType, arch)
		return result, nil
	}

	// trivy 경로 설정
	binDir := filepath.Join(homeDir, "bin")
	trivyPath := filepath.Join(binDir, trivyFileName)
	fmt.Println("trivyPath: ", trivyPath)

	// bin 디렉토리 생성
	if err := os.MkdirAll(binDir, 0755); err != nil {
		result.Status = "error"
		result.ErrorMsg = fmt.Sprintf("bin 디렉토리 생성 실패: %v", err)
		return result, nil
	}

	// trivy 존재 여부 확인
	if _, err := os.Stat(trivyPath); os.IsNotExist(err) {
		log.Printf("trivy가 설치되어 있지 않습니다. S3에서 다운로드 시작...")

		// S3에서 파일 다운로드
		cmd := exec.Command("aws", "s3", "cp",
			fmt.Sprintf("s3://docker-analysis-api-dev-binaries-92a1fdfdba50a7e6/%s", trivyFileName),
			trivyPath)

		if output, err := cmd.CombinedOutput(); err != nil {
			result.Status = "error"
			result.ErrorMsg = fmt.Sprintf("trivy 다운로드 실패: %v - %s", err, string(output))
			return result, nil
		}

		// 실행 권한 설정
		if err := os.Chmod(trivyPath, 0755); err != nil {
			result.Status = "error"
			result.ErrorMsg = fmt.Sprintf("trivy 권한 설정 실패: %v", err)
			return result, nil
		}

		log.Printf("trivy 다운로드 및 권한 설정 완료: %s", trivyPath)
	}

	// Trivy 명령어 실행 준비
	cmd := exec.CommandContext(ctx, trivyPath, "image", "--format", "json", imageFullName)
	log.Printf("실행 명령어: %s %s", trivyPath, strings.Join(cmd.Args[1:], " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("trivy 실행 오류: %v", err)
		log.Printf("trivy 출력: %s", string(output))
		result.Status = "error"
		result.ErrorMsg = fmt.Sprintf("이미지 분석 실패: %v - %s", err, string(output))
		return result, nil
	}

	// 결과 처리
	result.CompletedAt = time.Now()

	// trivy 출력에서 JSON 부분만 추출
	outputStr := string(output)
	jsonStart := strings.Index(outputStr, "{")
	if jsonStart == -1 {
		log.Printf("JSON 시작 부분을 찾을 수 없습니다")
		result.Status = "error"
		result.ErrorMsg = "JSON 출력을 찾을 수 없습니다"
		return result, nil
	}

	jsonOutput := outputStr[jsonStart:]

	// Trivy 출력 결과 파싱
	var trivyResult struct {
		SchemaVersion int    `json:"SchemaVersion"`
		CreatedAt     string `json:"CreatedAt"`
		Results       []struct {
			Target          string `json:"Target"`
			Vulnerabilities []struct {
				VulnerabilityID  string   `json:"VulnerabilityID"`
				PkgID            string   `json:"PkgID"`
				PkgName          string   `json:"PkgName"`
				InstalledVersion string   `json:"InstalledVersion"`
				FixedVersion     string   `json:"FixedVersion"`
				Status           string   `json:"Status"`
				PrimaryURL       string   `json:"PrimaryURL"`
				Title            string   `json:"Title"`
				CweIDs           []string `json:"CweIDs"`
				References       []string `json:"References"`
				Severity         string   `json:"Severity"`
				PublishedDate    string   `json:"PublishedDate"`
				LastModifiedDate string   `json:"LastModifiedDate"`
			} `json:"Vulnerabilities"`
		} `json:"Results"`
	}

	// JSON 파싱 시도
	if err := json.Unmarshal([]byte(jsonOutput), &trivyResult); err != nil {
		log.Printf("trivy 출력 파싱 실패: %v", err)
		log.Printf("원본 출력: %s", jsonOutput)

		result.Status = "error"
		result.ErrorMsg = fmt.Sprintf("결과 파싱 실패: %v", err)
		return result, nil
	}

	log.Printf("파싱된 trivyResult 구조체: %+v", trivyResult)

	// 취약점 개수 계산
	vulnerabilityCount := 0
	for _, result := range trivyResult.Results {
		vulnerabilityCount += len(result.Vulnerabilities)
		log.Printf("대상 %s에서 %d개의 취약점 발견", result.Target, len(result.Vulnerabilities))
	}

	log.Printf("총 %d개의 취약점 발견", vulnerabilityCount)

	result.Status = "success"
	result.Vulnerabilities = vulnerabilityCount

	// 취약점 상세 정보 추가
	for _, vuln := range trivyResult.Results {
		for _, detail := range vuln.Vulnerabilities {
			vulnDetail := Vulnerability{
				VulnerabilityID:  detail.VulnerabilityID,
				PkgID:            detail.PkgID,
				PkgName:          detail.PkgName,
				InstalledVersion: detail.InstalledVersion,
				FixedVersion:     detail.FixedVersion,
				Status:           detail.Status,
				Severity:         detail.Severity,
				Title:            detail.Title,
				References:       detail.References,
				PublishedDate:    detail.PublishedDate,
				LastModifiedDate: detail.LastModifiedDate,
			}
			result.VulnDetails = append(result.VulnDetails, vulnDetail)
		}
	}

	return result, nil
}
