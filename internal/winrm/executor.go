package winrm

import (
	"context"
	"fmt"
	"strings"
	"time"

	mwinrm "github.com/masterzen/winrm"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/ssh"
)

const (
	defaultExecuteTimeout = 30 * time.Minute
	// SetupExecuteTimeout is the WinRM execute timeout for MSSQL setup.exe -Wait (MS-008 / CLEAN-MSSQL-002).
	SetupExecuteTimeout = 60 * time.Minute
)

// Executor runs commands on Windows via WinRM (implements ssh.Executor).
type Executor struct {
	host           string
	user           string
	password       string
	port           int
	useSSL         bool
	logger         *logging.Logger
	stepID         string
	client         *mwinrm.Client
	executeTimeout time.Duration
}

// SetExecuteTimeout overrides the WinRM command timeout; 0 restores defaultExecuteTimeout.
func (e *Executor) SetExecuteTimeout(d time.Duration) {
	if e == nil {
		return
	}
	e.executeTimeout = d
}

func (e *Executor) commandTimeout() time.Duration {
	if e == nil || e.executeTimeout <= 0 {
		return defaultExecuteTimeout
	}
	return e.executeTimeout
}

// Config holds WinRM connection parameters.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	UseSSL   bool
	Auth     string
	Logger   *logging.Logger
	StepID   string
}

// NewExecutor creates a WinRM executor with a single connect attempt and debug logging.
func NewExecutor(cfg Config) (*Executor, error) {
	return ConnectWithRetry(cfg, 1, 0)
}

func (e *Executor) Host() string  { return e.host }
func (e *Executor) IsLocal() bool { return false }
func (e *Executor) Close() error  { return nil }

func (e *Executor) Execute(cmd string, sudo bool) (*ssh.ExecResult, error) {
	_ = sudo
	if e.client == nil {
		return nil, fmt.Errorf("winrm client not initialized")
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), e.commandTimeout())
	defer cancel()
	stdout, stderr, exitCode, err := e.client.RunWithContextWithString(ctx, cmd, "")
	end := time.Now()
	res := &ssh.ExecResult{
		Command:   cmd,
		Stdout:    stdout,
		Stderr:    stderr,
		ExitCode:  exitCode,
		StartTime: start,
		EndTime:   end,
		Duration:  end.Sub(start),
	}
	if err != nil {
		return res, fmt.Errorf("winrm execute: %w", err)
	}
	return res, nil
}

func (e *Executor) ExecuteScript(script string, sudo bool) (*ssh.ExecResult, error) {
	escaped := strings.ReplaceAll(script, `"`, `\"`)
	cmd := `powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "` + escaped + `"`
	return e.Execute(cmd, sudo)
}

func (e *Executor) Upload(localPath, remotePath string, uploadCtx *ssh.UploadContext) error {
	if e.client == nil {
		return fmt.Errorf("winrm client not initialized")
	}
	return uploadViaWinRM(e.client, e.host, localPath, remotePath, uploadCtx)
}

func (e *Executor) Download(remotePath, localPath string) error {
	if e.client == nil {
		return fmt.Errorf("winrm client not initialized")
	}
	return downloadViaWinRM(e.client, remotePath, localPath)
}
