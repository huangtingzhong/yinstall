package winrm

import (
	"context"
	"fmt"
	"strings"
	"time"

	mwinrm "github.com/masterzen/winrm"
	"github.com/yinstall/internal/logging"
)

const probeCommand = "echo yinstall-winrm-probe"

// ConnectInfo WinRM 建连参数摘要，供 debug 与错误信息共用。
type ConnectInfo struct {
	Host     string
	Port     int
	User     string
	Auth     string
	UseSSL   bool
	Password string // 明文，仅用于排障 debug（与 SSH 一致）
}

func (i ConnectInfo) effectivePort() int {
	if i.Port <= 0 {
		if i.UseSSL {
			return 5986
		}
		return 5985
	}
	return i.Port
}

func (i ConnectInfo) transportLabel() string {
	if i.UseSSL {
		return "https"
	}
	return "http"
}

// FormatLines 格式化登录信息行。
func (i ConnectInfo) FormatLines() []string {
	auth := strings.TrimSpace(i.Auth)
	if auth == "" {
		auth = "negotiate"
	}
	lines := []string{
		fmt.Sprintf("  User: %s@%s:%d", i.User, i.Host, i.effectivePort()),
		fmt.Sprintf("  Auth: %s", auth),
		fmt.Sprintf("  Transport: %s", i.transportLabel()),
	}
	if i.Password != "" {
		lines = append(lines, fmt.Sprintf("  Password: %s", i.Password))
	}
	return lines
}

// FormatBlock 将登录信息格式化为多行文本块。
func (i ConnectInfo) FormatBlock() string {
	return strings.Join(i.FormatLines(), "\n")
}

func buildConnectInfo(cfg Config) ConnectInfo {
	user := strings.TrimSpace(cfg.User)
	if user == "" {
		user = "Administrator"
	}
	auth := strings.TrimSpace(cfg.Auth)
	if auth == "" {
		auth = "negotiate"
	}
	return ConnectInfo{
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     user,
		Auth:     auth,
		UseSSL:   cfg.UseSSL,
		Password: cfg.Password,
	}
}

// LogConnectStart 在 WinRM 建连前写入 debug（仅 debug 文件，不进终端）。
func LogConnectStart(logger *logging.Logger, info ConnectInfo, stepID string, attempt, maxAttempts int) {
	if logger == nil {
		return
	}
	prefix := fmt.Sprintf("host=%s step=%s", info.Host, stepID)
	port := info.effectivePort()
	if maxAttempts > 1 {
		logger.DebugWrite("DEBUG", fmt.Sprintf("%s >>> winrm connect attempt=%d/%d user=%s@%s:%d auth=%s ssl=%v",
			prefix, attempt, maxAttempts, info.User, info.Host, port, info.Auth, info.UseSSL))
	} else {
		logger.DebugWrite("DEBUG", fmt.Sprintf("%s >>> winrm connect user=%s@%s:%d auth=%s ssl=%v",
			prefix, info.User, info.Host, port, info.Auth, info.UseSSL))
	}
	for _, line := range info.FormatLines() {
		logger.DebugWrite("DEBUG", fmt.Sprintf("%s winrm connect|%s", prefix, strings.TrimSpace(line)))
	}
}

// LogConnectResult 记录 WinRM 建连结果；失败时在 ERROR 级别再次输出登录信息。
func LogConnectResult(logger *logging.Logger, info ConnectInfo, stepID string, success bool, errMsg string, duration time.Duration) {
	if logger == nil {
		return
	}
	prefix := fmt.Sprintf("host=%s step=%s", info.Host, stepID)
	exitCode := 0
	if !success {
		exitCode = -1
	}
	logger.DebugWrite("DEBUG", fmt.Sprintf("%s winrm connect exit_code=%d duration=%s", prefix, exitCode, duration))
	if success {
		logger.DebugWrite("DEBUG", fmt.Sprintf("%s winrm connect stdout| (session established)", prefix))
		return
	}
	logger.DebugWrite("ERROR", fmt.Sprintf("%s WinRM connection failed:", prefix))
	for _, line := range info.FormatLines() {
		logger.DebugWrite("ERROR", fmt.Sprintf("%s|%s", prefix, strings.TrimSpace(line)))
	}
	if errMsg != "" {
		logger.DebugWrite("ERROR", fmt.Sprintf("%s winrm connect error| %s", prefix, errMsg))
	}
}

// WrapConnectErrorAfterRetries 多次重试仍失败时的错误包装。
func WrapConnectErrorAfterRetries(info ConnectInfo, attempts int, err error) error {
	if err == nil {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "failed to connect to %s via WinRM after %d attempts:\n", info.Host, attempts)
	b.WriteString(info.FormatBlock())
	fmt.Fprintf(&b, "\n  Last error: %v", err)
	return fmt.Errorf("%s", b.String())
}

// ConnectWithRetry 建立 WinRM 连接并探测可用性，带 debug 日志与重试。
func ConnectWithRetry(cfg Config, maxAttempts int, retryDelay time.Duration) (*Executor, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		return nil, fmt.Errorf("winrm host required")
	}
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	info := buildConnectInfo(cfg)
	stepID := cfg.StepID
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		LogConnectStart(cfg.Logger, info, stepID, attempt, maxAttempts)
		start := time.Now()
		ex, err := connectOnce(cfg)
		if err == nil {
			LogConnectResult(cfg.Logger, info, stepID, true, "", time.Since(start))
			return ex, nil
		}
		lastErr = err
		LogConnectResult(cfg.Logger, info, stepID, false, err.Error(), time.Since(start))
		if attempt < maxAttempts && retryDelay > 0 {
			if cfg.Logger != nil {
				cfg.Logger.Warn("WinRM connection attempt %d/%d failed for %s:\n%s  Error: %v\nretrying in %v...",
					attempt, maxAttempts, cfg.Host, info.FormatBlock(), err, retryDelay)
			}
			time.Sleep(retryDelay)
		}
	}
	return nil, WrapConnectErrorAfterRetries(info, maxAttempts, lastErr)
}

func connectOnce(cfg Config) (*Executor, error) {
	port := cfg.Port
	if port == 0 {
		if cfg.UseSSL {
			port = 5986
		} else {
			port = 5985
		}
	}
	user := strings.TrimSpace(cfg.User)
	if user == "" {
		user = "Administrator"
	}
	endpoint := mwinrm.NewEndpoint(cfg.Host, port, cfg.UseSSL, true, nil, nil, nil, 30*time.Second)
	client, err := mwinrm.NewClient(endpoint, user, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("winrm client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, exitCode, err := client.RunWithContextWithString(ctx, probeCommand, "")
	if err != nil {
		return nil, fmt.Errorf("winrm probe: %w", err)
	}
	if exitCode != 0 {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = strings.TrimSpace(stdout)
		}
		if msg == "" {
			msg = fmt.Sprintf("probe exit_code=%d", exitCode)
		}
		return nil, fmt.Errorf("winrm probe: %s", msg)
	}
	return &Executor{
		host:     cfg.Host,
		user:     user,
		password: cfg.Password,
		port:     port,
		useSSL:   cfg.UseSSL,
		logger:   cfg.Logger,
		stepID:   cfg.StepID,
		client:   client,
	}, nil
}
