package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config는 데몬 서비스의 전체 설정을 나타냅니다.
type Config struct {
	// Backend 설정
	Backend struct {
		URL      string `yaml:"url"`      // Backend 서버 URL
		Timeout  int    `yaml:"timeout"`  // API 요청 타임아웃 (초)
		RetryCnt int    `yaml:"retryCnt"` // API 요청 재시도 횟수
	} `yaml:"backend"`

	// RabbitMQ 설정
	RabbitMQ struct {
		URL        string `yaml:"url"`        // RabbitMQ 서버 URL
		Exchange   string `yaml:"exchange"`   // 교환기 이름
		Queue      string `yaml:"queue"`      // 큐 이름
		RoutingKey string `yaml:"routingKey"` // 라우팅 키
	} `yaml:"rabbitmq"`

	// 로깅 설정
	Logging struct {
		Level      string `yaml:"level"`      // 로그 레벨 (debug, info, warn, error)
		File       string `yaml:"file"`       // 로그 파일 경로
		MaxSize    int    `yaml:"maxSize"`    // 로그 파일 최대 크기 (MB)
		MaxBackups int    `yaml:"maxBackups"` // 백업 파일 최대 개수
		MaxAge     int    `yaml:"maxAge"`     // 로그 파일 보관 기간 (일)
	} `yaml:"logging"`

	// 모니터링 설정
	Monitoring struct {
		Enabled     bool   `yaml:"enabled"`     // 모니터링 활성화 여부
		MetricsPort int    `yaml:"metricsPort"` // 메트릭 수집 포트
		Endpoint    string `yaml:"endpoint"`    // 메트릭 엔드포인트
		Process     struct {
			Name          string  `yaml:"name"`          // 모니터링할 프로세스 이름 (trivy)
			CPULimit      float64 `yaml:"cpuLimit"`      // CPU 사용량 제한 (%)
			MemoryLimit   int64   `yaml:"memoryLimit"`   // 메모리 사용량 제한 (MB)
			Timeout       int     `yaml:"timeout"`       // 프로세스 실행 타임아웃 (초)
			CheckInterval int     `yaml:"checkInterval"` // 모니터링 체크 간격 (초)
		} `yaml:"process"`
	} `yaml:"monitoring"`
}

// LoadConfig는 설정 파일을 로드합니다.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
