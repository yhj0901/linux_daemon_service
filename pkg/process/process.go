package process

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// ProcessManager는 프로세스 관리를 담당하는 구조체입니다.
type ProcessManager struct {
	pidFile string
}

// NewProcessManager는 새로운 ProcessManager를 생성합니다.
func NewProcessManager(pidFile string) *ProcessManager {
	return &ProcessManager{
		pidFile: pidFile,
	}
}

// GetPID는 현재 실행 중인 프로세스의 PID를 반환합니다.
func (pm *ProcessManager) GetPID() (int, error) {
	pidBytes, err := os.ReadFile(pm.pidFile)
	if err != nil {
		return 0, fmt.Errorf("pid 파일을 읽을 수 없습니다: %v", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		return 0, fmt.Errorf("pid 파일의 내용이 올바르지 않습니다: %v", err)
	}

	return pid, nil
}

// IsRunning은 프로세스가 실행 중인지 확인합니다.
func (pm *ProcessManager) IsRunning() (bool, error) {
	pid, err := pm.GetPID()
	if err != nil {
		return false, nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil, nil
}

// Stop은 프로세스를 정상적으로 종료합니다.
func (pm *ProcessManager) Stop() error {
	pid, err := pm.GetPID()
	if err != nil {
		return err
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("프로세스를 찾을 수 없습니다: %v", err)
	}

	return process.Signal(syscall.SIGTERM)
}

// Reload는 프로세스를 재시작합니다.
func (pm *ProcessManager) Reload() error {
	pid, err := pm.GetPID()
	if err != nil {
		return err
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("프로세스를 찾을 수 없습니다: %v", err)
	}

	return process.Signal(syscall.SIGHUP)
}

// WritePID는 현재 프로세스의 PID를 파일에 저장합니다.
func (pm *ProcessManager) WritePID() error {
	pid := os.Getpid()
	return os.WriteFile(pm.pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0644)
}

// RemovePID는 PID 파일을 삭제합니다.
func (pm *ProcessManager) RemovePID() error {
	return os.Remove(pm.pidFile)
}
