package ssh

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yinstall/internal/logging"
	"golang.org/x/crypto/ssh"
)

const (
	// sshKeepAliveInterval TCP keepalive 间隔，防止长时间安装时 SSH 连接被网络设备断开
	sshKeepAliveInterval = 30 * time.Second
	// sshConnectRetries SSH 连接建立最大重试次数
	sshConnectRetries = 3
	// sshRetryBaseDelay 重试基础延迟（指数退避）
	sshRetryBaseDelay = 2 * time.Second
)

// Executor 命令执行器接口
type Executor interface {
	Execute(cmd string, sudo bool) (*ExecResult, error)
	ExecuteScript(script string, sudo bool) (*ExecResult, error)
	Upload(localPath, remotePath string, uploadCtx *UploadContext) error
	Download(remotePath, localPath string) error
	Close() error
	Host() string
	IsLocal() bool
}

// ExecResult 命令执行结果
type ExecResult struct {
	Command   string
	Stdout    string
	Stderr    string
	ExitCode  int
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
}

// GetStdout 返回标准输出，供仅依赖执行结果的调用方使用
func (r *ExecResult) GetStdout() string {
	if r == nil {
		return ""
	}
	return r.Stdout
}

// GetExitCode 返回退出码，供仅依赖执行结果的调用方使用
func (r *ExecResult) GetExitCode() int {
	if r == nil {
		return -1
	}
	return r.ExitCode
}

// GetStderr 返回标准错误
func (r *ExecResult) GetStderr() string {
	if r == nil {
		return ""
	}
	return r.Stderr
}

// GetDuration 返回执行耗时
func (r *ExecResult) GetDuration() time.Duration {
	if r == nil {
		return 0
	}
	return r.Duration
}

// Config SSH 连接配置
type Config struct {
	Host          string
	Port          int
	User          string
	AuthMethod    string // password, key, local
	Password      string
	KeyPath       string
	KeyPassphrase string
	KnownHosts    string // strict, accept-new, ignore
	Timeout       time.Duration
	Logger        *logging.Logger // 可选的日志记录器，用于记录所有命令执行
	StepID        string          // 可选的步骤 ID，用于日志记录
	TargetOS      string          // linux|windows|darwin; empty = unix bash wrapper
}

// IsAuthenticationFailure 判断错误是否为 SSH 握手/密码认证失败（用于探测场景，非网络类致命错误）。
func IsAuthenticationFailure(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unable to authenticate") ||
		strings.Contains(s, "authentication failed") ||
		strings.Contains(s, "handshake failed")
}

// NewExecutor 创建执行器
func NewExecutor(cfg Config) (Executor, error) {
	// 判断是否本机执行
	if cfg.AuthMethod == "local" || isLocalHost(cfg.Host) {
		return &LocalExecutor{
			host: cfg.Host,
		}, nil
	}

	// SSH 执行
	return newSSHExecutor(cfg)
}

// NewExecutorWithFallback 创建执行器，支持多种认证方式的自动降级
// 优先级：1. 免密登陆 2. 默认密码 3. 用户指定的认证方式
func NewExecutorWithFallback(cfg Config, defaultPassword string) (Executor, error) {
	// 判断是否本机执行
	if cfg.AuthMethod == "local" || isLocalHost(cfg.Host) {
		return &LocalExecutor{
			host: cfg.Host,
		}, nil
	}

	// 如果用户明确指定了密码，直接使用
	if cfg.Password != "" {
		return newSSHExecutor(cfg)
	}

	// 自动降级逻辑：先尝试免密，再尝试默认密码
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var authErrors []string

	// 1. 尝试免密登陆（使用 ssh-agent 或 ~/.ssh/id_rsa）
	keyPath := cfg.KeyPath
	if keyPath == "" {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, ".ssh", "id_rsa")
	}

	if info, err := os.Stat(keyPath); err != nil {
		authErrors = append(authErrors, fmt.Sprintf("SSH key: file not found (%s)", keyPath))
	} else if info.IsDir() {
		authErrors = append(authErrors, fmt.Sprintf("SSH key: path is a directory, not a file (%s)", keyPath))
	} else {
		f, readErr := os.Open(keyPath)
		if readErr != nil {
			authErrors = append(authErrors, fmt.Sprintf("SSH key: file not readable (%s): %v", keyPath, readErr))
		} else {
			f.Close()
			cfgKey := cfg
			cfgKey.AuthMethod = "key"
			cfgKey.KeyPath = keyPath

			if executor, keyErr := newSSHExecutor(cfgKey); keyErr == nil {
				return executor, nil
			} else {
				authErrors = append(authErrors, fmt.Sprintf("SSH key (%s): %v", keyPath, keyErr))
			}
		}
	}

	// 2. 尝试默认密码
	if defaultPassword != "" {
		cfgPwd := cfg
		cfgPwd.AuthMethod = "password"
		cfgPwd.Password = defaultPassword

		if executor, pwdErr := newSSHExecutor(cfgPwd); pwdErr == nil {
			return executor, nil
		} else {
			authErrors = append(authErrors, fmt.Sprintf("default password: %v", pwdErr))
		}
	} else {
		authErrors = append(authErrors, "default password: not provided")
	}

	// 3. 所有方式都失败，返回每种方式的具体失败原因
	errDetail := strings.Join(authErrors, "\n  - ")
	return nil, fmt.Errorf(
		"failed to connect to %s: all authentication methods failed\n  - %s\n"+
			"  Please provide valid credentials using --ssh-password or --ssh-key-path",
		addr, errDetail,
	)
}

// isLocalHost 判断是否本机
func isLocalHost(host string) bool {
	if host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	// 检查是否本机 IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			if ipnet.IP.String() == host {
				return true
			}
		}
	}
	return false
}

// LocalExecutor 本机执行器
type LocalExecutor struct {
	host string
}

func (e *LocalExecutor) Host() string {
	if e.host == "" {
		return "localhost"
	}
	return e.host
}

func (e *LocalExecutor) IsLocal() bool {
	return true
}

func (e *LocalExecutor) Execute(command string, sudo bool) (*ExecResult, error) {
	result := &ExecResult{
		Command:   command,
		StartTime: time.Now(),
	}

	var cmd *exec.Cmd = localExecCommand(command, sudo)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	return result, nil
}

// ExecuteContext 在 context 控制下执行命令；context 取消时通过 cmd.Cancel 终止进程。
// 本方法不在 Executor 接口中，供 collect 子命令的 collectExecAdapter 使用。
func (e *LocalExecutor) ExecuteContext(ctx context.Context, command string, sudo bool) (*ExecResult, error) {
	result := &ExecResult{
		Command:   command,
		StartTime: time.Now(),
	}

	var cmd *exec.Cmd = localExecCommandContext(ctx, command, sudo)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Stdout = stdoutBuf.String()
	result.Stderr = stderrBuf.String()

	if err != nil {
		if ctx.Err() != nil {
			// context 超时或取消，统一返回 124（与 SSH session 超时一致）
			result.ExitCode = 124
			return result, fmt.Errorf("command timed out: %w", ctx.Err())
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	return result, nil
}

func (e *LocalExecutor) ExecuteScript(script string, sudo bool) (*ExecResult, error) {
	return e.Execute(script, sudo)
}

func (e *LocalExecutor) Download(remotePath, localPath string) error {
	return e.Upload(remotePath, localPath, nil)
}

func (e *LocalExecutor) Close() error {
	return nil
}

func localExecCommand(command string, sudo bool) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return windowsLocalCommand(command)
	}
	if sudo && os.Getuid() != 0 {
		return exec.Command("sudo", "-n", "bash", "-c", command)
	}
	return exec.Command("bash", "-c", command)
}

func localExecCommandContext(ctx context.Context, command string, sudo bool) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return windowsLocalCommandContext(ctx, command)
	}
	if sudo && os.Getuid() != 0 {
		return exec.CommandContext(ctx, "sudo", "-n", "bash", "-c", command)
	}
	return exec.CommandContext(ctx, "bash", "-c", command)
}

// windowsLocalCommand runs commands on Windows local mode.
// Avoid cmd /c powershell -Command "..." — nested quoting breaks; invoke powershell.exe directly.
// Quoted exe paths ("C:/path/mysqld.exe" ...) must not go through cmd /c — quotes become part of the program name.
func windowsLocalCommand(command string) *exec.Cmd {
	if args, ok := powerShellLocalArgs(command); ok {
		return exec.Command("powershell.exe", args...)
	}
	if exe, args, ok := parseQuotedWindowsExeCommand(command); ok {
		return exec.Command(exe, args...)
	}
	return exec.Command("cmd.exe", "/c", command)
}

func windowsLocalCommandContext(ctx context.Context, command string) *exec.Cmd {
	if args, ok := powerShellLocalArgs(command); ok {
		return exec.CommandContext(ctx, "powershell.exe", args...)
	}
	if exe, args, ok := parseQuotedWindowsExeCommand(command); ok {
		return exec.CommandContext(ctx, exe, args...)
	}
	return exec.CommandContext(ctx, "cmd.exe", "/c", command)
}

// parseQuotedWindowsExeCommand splits `"C:/bin/foo.exe" --arg=val` for direct exec.Command invocation.
func parseQuotedWindowsExeCommand(command string) (exe string, args []string, ok bool) {
	trimmed := strings.TrimSpace(command)
	if !strings.HasPrefix(trimmed, `"`) {
		return "", nil, false
	}
	end := strings.Index(trimmed[1:], `"`)
	if end < 0 {
		return "", nil, false
	}
	exe = trimmed[1 : end+1]
	rest := strings.TrimSpace(trimmed[end+2:])
	if rest == "" {
		return exe, nil, true
	}
	return exe, splitWindowsCommandArgs(rest), true
}

func splitWindowsCommandArgs(s string) []string {
	var args []string
	for {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			break
		}
		if s[0] == '"' {
			end := strings.Index(s[1:], `"`)
			if end < 0 {
				args = append(args, s[1:])
				break
			}
			args = append(args, s[1:end+1])
			s = s[end+2:]
			continue
		}
		idx := strings.IndexAny(s, " \t")
		if idx < 0 {
			token := s
			if i := strings.Index(token, `"`); i > 0 && strings.HasSuffix(token, `"`) {
				token = token[:i] + token[i+1:len(token)-1]
			}
			args = append(args, token)
			break
		}
		token := s[:idx]
		if i := strings.Index(token, `"`); i > 0 && strings.HasSuffix(token, `"`) {
			token = token[:i] + token[i+1:len(token)-1]
		}
		args = append(args, token)
		s = s[idx:]
	}
	return args
}

func powerShellLocalArgs(command string) ([]string, bool) {
	trimmed := strings.TrimSpace(command)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "powershell") {
		return nil, false
	}
	const marker = "-command "
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return nil, false
	}
	prefix := strings.Fields(trimmed[:idx])
	script := strings.TrimSpace(trimmed[idx+len(marker):])
	if len(script) >= 2 && script[0] == '"' && script[len(script)-1] == '"' {
		script = script[1 : len(script)-1]
	}
	args := append(prefix[1:], "-Command", script) // drop "powershell"
	return args, true
}

// SSHExecutor SSH 执行器
type SSHExecutor struct {
	client *ssh.Client
	config Config
}

func newSSHExecutor(cfg Config) (*SSHExecutor, error) {
	var authMethods []ssh.AuthMethod

	switch cfg.AuthMethod {
	case "password":
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	case "key":
		key, err := os.ReadFile(cfg.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read private key: %w", err)
		}
		var signer ssh.Signer
		if cfg.KeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(cfg.KeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	default:
		return nil, fmt.Errorf("unsupported auth method: %s", cfg.AuthMethod)
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	sshConfig := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 简化处理
		Timeout:         timeout,
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	var client *ssh.Client
	var lastErr error
	for attempt := 0; attempt < sshConnectRetries; attempt++ {
		if attempt > 0 {
			delay := sshRetryBaseDelay * time.Duration(1<<uint(attempt-1))
			time.Sleep(delay)
		}

		rawConn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			lastErr = fmt.Errorf("failed to connect to %s: %w", addr, err)
			continue
		}
		if tc, ok := rawConn.(*net.TCPConn); ok {
			_ = tc.SetKeepAlive(true)
			_ = tc.SetKeepAlivePeriod(sshKeepAliveInterval)
		}

		c, chans, reqs, err := ssh.NewClientConn(rawConn, addr, sshConfig)
		if err != nil {
			rawConn.Close()
			lastErr = fmt.Errorf("failed to establish SSH connection to %s: %w", addr, err)
			continue
		}
		client = ssh.NewClient(c, chans, reqs)
		break
	}
	if client == nil {
		return nil, lastErr
	}

	return &SSHExecutor{
		client: client,
		config: cfg,
	}, nil
}

func (e *SSHExecutor) Host() string {
	return e.config.Host
}

func (e *SSHExecutor) IsLocal() bool {
	return false
}

func (e *SSHExecutor) Execute(command string, sudo bool) (*ExecResult, error) {
	result := &ExecResult{
		Command:   command,
		StartTime: time.Now(),
	}

	// 构建实际执行的命令
	actualCmd := wrapSSHCommand(e.config, command, sudo)

	session, err := e.client.NewSession()
	if err != nil {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.ExitCode = -1
		result.Stderr = fmt.Sprintf("failed to create session: %v", err)
		return result, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(actualCmd)
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			result.ExitCode = exitErr.ExitStatus()
		} else {
			result.ExitCode = -1
		}
	}

	return result, nil
}

// ExecuteContext 在 context 控制下执行命令；context 取消时先发 SIGKILL 再关闭 session，
// 确保 goroutine 退出且不泄漏，SSH 连接保持可复用。
// 本方法不在 Executor 接口中，供 collect 子命令的 collectExecAdapter 使用。
func (e *SSHExecutor) ExecuteContext(ctx context.Context, command string, sudo bool) (*ExecResult, error) {
	result := &ExecResult{
		Command:   command,
		StartTime: time.Now(),
	}

	actualCmd := wrapSSHCommand(e.config, command, sudo)

	session, err := e.client.NewSession()
	if err != nil {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.ExitCode = -1
		result.Stderr = fmt.Sprintf("failed to create session: %v", err)
		return result, fmt.Errorf("failed to create session: %w", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	type runResult struct{ code int }
	ch := make(chan runResult, 1)

	go func() {
		runErr := session.Run(actualCmd)
		code := 0
		if runErr != nil {
			if exitErr, ok := runErr.(*ssh.ExitError); ok {
				code = exitErr.ExitStatus()
			} else {
				code = -1
			}
		}
		ch <- runResult{code}
	}()

	select {
	case res := <-ch:
		session.Close()
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.Stdout = stdoutBuf.String()
		result.Stderr = stderrBuf.String()
		result.ExitCode = res.code
		return result, nil
	case <-ctx.Done():
		// 先 SIGKILL 让远端进程退出，再 Close session 解除 goroutine 的 session.Run 阻塞
		_ = session.Signal(ssh.SIGKILL)
		session.Close()
		<-ch // 等 goroutine 退出，避免 goroutine 泄漏
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.Stdout = stdoutBuf.String()
		result.Stderr = stderrBuf.String()
		result.ExitCode = 124 // 与 GNU timeout 退出码一致
		return result, fmt.Errorf("command timed out: %w", ctx.Err())
	}
}

func (e *SSHExecutor) ExecuteScript(script string, sudo bool) (*ExecResult, error) {
	return e.Execute(script, sudo)
}

func (e *SSHExecutor) Download(remotePath, localPath string) error {
	session, err := e.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	var stdout bytes.Buffer
	session.Stdout = &stdout

	if err := session.Run(fmt.Sprintf("cat %s", remotePath)); err != nil {
		return err
	}

	return os.WriteFile(localPath, stdout.Bytes(), 0644)
}

func (e *SSHExecutor) Close() error {
	if e.client != nil {
		return e.client.Close()
	}
	return nil
}
