package ssh

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

// OutputLineHandler 命令执行中实时回调一行输出（无尾部换行）。
// stream 为 "stdout" 或 "stderr"。由 StepContext 挂接以写入 debug 流式日志。
type OutputLineHandler func(stream, line string)

// BindOutputLineHandler 若 e 支持则挂接行回调；返回 clear 与是否挂接成功。
// WinRM 等整包执行器返回 attached=false，调用方应事后写 LogCommandResult。
func BindOutputLineHandler(e Executor, h OutputLineHandler) (clear func(), attached bool) {
	type setter interface {
		SetOutputLineHandler(OutputLineHandler)
	}
	if e == nil {
		return func() {}, false
	}
	if s, ok := e.(setter); ok {
		s.SetOutputLineHandler(h)
		return func() { s.SetOutputLineHandler(nil) }, true
	}
	return func() {}, false
}

// LocalExecutor 本机执行器
type LocalExecutor struct {
	host       string
	outMu      sync.Mutex
	outHandler OutputLineHandler
}

// SetOutputLineHandler 设置/清除执行期 stdout/stderr 行回调。
func (e *LocalExecutor) SetOutputLineHandler(h OutputLineHandler) {
	if e == nil {
		return
	}
	e.outMu.Lock()
	e.outHandler = h
	e.outMu.Unlock()
}

func (e *LocalExecutor) outputLineHandler() OutputLineHandler {
	if e == nil {
		return nil
	}
	e.outMu.Lock()
	defer e.outMu.Unlock()
	return e.outHandler
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
	cmd := localExecCommand(command, sudo)
	stdout, stderr, exitCode, _ := runCmdWithStream(cmd, e.outputLineHandler())
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode
	return result, nil
}

// ExecuteContext 在 context 控制下执行命令；context 取消时通过 cmd.Cancel 终止进程。
// 本方法不在 Executor 接口中，供 collect 子命令的 collectExecAdapter 使用。
func (e *LocalExecutor) ExecuteContext(ctx context.Context, command string, sudo bool) (*ExecResult, error) {
	result := &ExecResult{
		Command:   command,
		StartTime: time.Now(),
	}
	cmd := localExecCommandContext(ctx, command, sudo)
	stdout, stderr, exitCode, runErr := runCmdWithStream(cmd, e.outputLineHandler())
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Stdout = stdout
	result.Stderr = stderr
	if ctx.Err() != nil {
		result.ExitCode = 124
		return result, fmt.Errorf("command timed out: %w", ctx.Err())
	}
	result.ExitCode = exitCode
	if runErr != nil {
		// 管道/启动失败：与旧 Run 行为接近，exit=-1 且仍返回 result
		if result.ExitCode == 0 {
			result.ExitCode = -1
		}
		return result, nil
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
	client     *ssh.Client
	config     Config
	outMu      sync.Mutex
	outHandler OutputLineHandler
}

// SetOutputLineHandler 设置/清除执行期 stdout/stderr 行回调。
func (e *SSHExecutor) SetOutputLineHandler(h OutputLineHandler) {
	if e == nil {
		return
	}
	e.outMu.Lock()
	e.outHandler = h
	e.outMu.Unlock()
}

func (e *SSHExecutor) outputLineHandler() OutputLineHandler {
	if e == nil {
		return nil
	}
	e.outMu.Lock()
	defer e.outMu.Unlock()
	return e.outHandler
}

func newSSHExecutor(cfg Config) (*SSHExecutor, error) {
	client, err := dialSSHClient(cfg)
	if err != nil {
		return nil, err
	}
	return &SSHExecutor{
		client: client,
		config: cfg,
	}, nil
}

func dialSSHClient(cfg Config) (*ssh.Client, error) {
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
	return client, nil
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

	stdout, stderr, exitCode, _ := runSSHSessionWithStream(session, actualCmd, e.outputLineHandler())
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode
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

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		result.ExitCode = -1
		result.Stderr = err.Error()
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		session.Close()
		result.ExitCode = -1
		result.Stderr = err.Error()
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, fmt.Errorf("stderr pipe: %w", err)
	}

	handler := e.outputLineHandler()
	var stdoutBuf, stderrBuf bytes.Buffer
	if err := session.Start(actualCmd); err != nil {
		session.Close()
		result.ExitCode = -1
		result.Stderr = err.Error()
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = pumpOutputStream(stdoutPipe, &stdoutBuf, "stdout", handler)
	}()
	go func() {
		defer wg.Done()
		_ = pumpOutputStream(stderrPipe, &stderrBuf, "stderr", handler)
	}()

	type waitRes struct{ err error }
	ch := make(chan waitRes, 1)
	go func() {
		ch <- waitRes{err: session.Wait()}
	}()

	var waitErr error
	timedOut := false
	select {
	case res := <-ch:
		waitErr = res.err
	case <-ctx.Done():
		timedOut = true
		_ = session.Signal(ssh.SIGKILL)
		session.Close()
		waitErr = (<-ch).err
	}
	wg.Wait()
	if !timedOut {
		session.Close()
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Stdout = stdoutBuf.String()
	result.Stderr = stderrBuf.String()
	if timedOut {
		result.ExitCode = 124
		return result, fmt.Errorf("command timed out: %w", ctx.Err())
	}
	result.ExitCode = exitCodeFromSSHWait(waitErr)
	if waitErr != nil {
		if _, ok := waitErr.(*ssh.ExitError); ok {
			return result, nil
		}
		return result, waitErr
	}
	return result, nil
}

// pumpOutputStream 边读边写入 buf，并按行回调 handler（实时 debug）。
func pumpOutputStream(r io.Reader, buf *bytes.Buffer, stream string, h OutputLineHandler) error {
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			_, _ = buf.WriteString(line)
			if h != nil {
				h(stream, strings.TrimRight(line, "\r\n"))
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func exitCodeFromCmdWait(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

func exitCodeFromSSHWait(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return exitErr.ExitStatus()
	}
	return -1
}

// runCmdWithStream 通过管道实时读取本地命令输出。
func runCmdWithStream(cmd *exec.Cmd, h OutputLineHandler) (stdout, stderr string, exitCode int, err error) {
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", -1, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", -1, err
	}
	if err := cmd.Start(); err != nil {
		return "", "", -1, err
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = pumpOutputStream(stdoutPipe, &stdoutBuf, "stdout", h)
	}()
	go func() {
		defer wg.Done()
		_ = pumpOutputStream(stderrPipe, &stderrBuf, "stderr", h)
	}()

	waitErr := cmd.Wait()
	wg.Wait()
	return stdoutBuf.String(), stderrBuf.String(), exitCodeFromCmdWait(waitErr), func() error {
		if waitErr == nil {
			return nil
		}
		if _, ok := waitErr.(*exec.ExitError); ok {
			return nil // 与旧行为一致：非零退出不作为 error 返回
		}
		return waitErr
	}()
}

// runSSHSessionWithStream Start + 并发读管线 + Wait；避免 SSH 缓冲死锁并支持实时日志。
func runSSHSessionWithStream(session *ssh.Session, actualCmd string, h OutputLineHandler) (stdout, stderr string, exitCode int, err error) {
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return "", "", -1, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return "", "", -1, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := session.Start(actualCmd); err != nil {
		return "", "", -1, err
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = pumpOutputStream(stdoutPipe, &stdoutBuf, "stdout", h)
	}()
	go func() {
		defer wg.Done()
		_ = pumpOutputStream(stderrPipe, &stderrBuf, "stderr", h)
	}()

	waitErr := session.Wait()
	wg.Wait()
	code := exitCodeFromSSHWait(waitErr)
	if waitErr != nil {
		if _, ok := waitErr.(*ssh.ExitError); ok {
			return stdoutBuf.String(), stderrBuf.String(), code, nil
		}
		return stdoutBuf.String(), stderrBuf.String(), code, waitErr
	}
	return stdoutBuf.String(), stderrBuf.String(), code, nil
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

// ConnectAttemptInfo SSH 建连参数摘要，供 debug 与错误信息共用。
type ConnectAttemptInfo struct {
	Host       string
	Port       int
	User       string
	AuthMethod string
	Password   string // 明文，仅用于排障日志
	KeyPath    string
}

// EffectivePort 返回有效 SSH 端口（<=0 时为 22）。
func (i ConnectAttemptInfo) EffectivePort() int {
	if i.Port <= 0 {
		return 22
	}
	return i.Port
}

// FormatLines 格式化登录信息行（每行带前导空格，便于嵌入多行错误）。
func (i ConnectAttemptInfo) FormatLines() []string {
	lines := []string{
		fmt.Sprintf("  User: %s@%s:%d", i.User, i.Host, i.EffectivePort()),
		fmt.Sprintf("  Auth: %s", i.AuthMethod),
	}
	if i.KeyPath != "" {
		lines = append(lines, fmt.Sprintf("  Key file: %s", i.KeyPath))
	}
	if i.Password != "" {
		lines = append(lines, fmt.Sprintf("  Password: %s", i.Password))
	}
	return lines
}

// FormatBlock 将登录信息格式化为多行文本块。
func (i ConnectAttemptInfo) FormatBlock() string {
	return strings.Join(i.FormatLines(), "\n")
}

// BuildConnectAttemptInfo 根据配置推断建连方式与日志摘要字段。
func BuildConnectAttemptInfo(cfg Config, passwordProvided bool, defaultPassword string) ConnectAttemptInfo {
	keyPath := cfg.KeyPath
	authMethod := cfg.AuthMethod
	password := cfg.Password

	switch {
	case passwordProvided || cfg.Password != "":
		authMethod = "password"
	case cfg.AuthMethod == "key":
		authMethod = "key"
		password = ""
	default:
		authMethod = "fallback(key,default_password)"
		password = defaultPassword
	}

	if authMethod == "key" || authMethod == "fallback(key,default_password)" {
		if keyPath == "" {
			if home, err := os.UserHomeDir(); err == nil {
				keyPath = filepath.Join(home, ".ssh", "id_rsa")
			}
		}
	}

	return ConnectAttemptInfo{
		Host:       cfg.Host,
		Port:       cfg.Port,
		User:       cfg.User,
		AuthMethod: authMethod,
		Password:   password,
		KeyPath:    keyPath,
	}
}

// WrapConnectError 在错误信息中附带 SSH 登录参数，便于 session/debug/终端排障。
func WrapConnectError(info ConnectAttemptInfo, err error) error {
	if err == nil {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "SSH connection failed for %s:\n", info.Host)
	b.WriteString(info.FormatBlock())
	fmt.Fprintf(&b, "\n  Error: %v", err)
	return fmt.Errorf("%s", b.String())
}

// WrapConnectErrorAfterRetries 多次重试仍失败时的错误包装。
func WrapConnectErrorAfterRetries(info ConnectAttemptInfo, attempts int, err error) error {
	if err == nil {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "failed to connect to %s after %d attempts:\n", info.Host, attempts)
	b.WriteString(info.FormatBlock())
	fmt.Fprintf(&b, "\n  Last error: %v", err)
	return fmt.Errorf("%s", b.String())
}

// LogConnectStart 在 SSH 建连前写入 debug（仅 debug 文件，不进终端）。
// 有密码时以明文记录，便于核对 CLI 传入值。
func LogConnectStart(logger *logging.Logger, info ConnectAttemptInfo, stepID string, attempt, maxAttempts int) {
	if logger == nil {
		return
	}
	host := info.Host
	prefix := fmt.Sprintf("host=%s step=%s", host, stepID)
	port := info.EffectivePort()
	if maxAttempts > 1 {
		logger.DebugWrite("DEBUG", fmt.Sprintf("%s >>> ssh connect attempt=%d/%d user=%s@%s:%d auth=%s",
			prefix, attempt, maxAttempts, info.User, host, port, info.AuthMethod))
	} else {
		logger.DebugWrite("DEBUG", fmt.Sprintf("%s >>> ssh connect user=%s@%s:%d auth=%s",
			prefix, info.User, host, port, info.AuthMethod))
	}
	for _, line := range info.FormatLines() {
		logger.DebugWrite("DEBUG", fmt.Sprintf("%s ssh connect|%s", prefix, strings.TrimSpace(line)))
	}
}

// LogConnectResult 记录 SSH 建连结果；失败时在 ERROR 级别再次输出登录信息。
func LogConnectResult(logger *logging.Logger, info ConnectAttemptInfo, stepID string, success bool, errMsg string, duration time.Duration) {
	if logger == nil {
		return
	}
	host := info.Host
	prefix := fmt.Sprintf("host=%s step=%s", host, stepID)
	exitCode := 0
	if !success {
		exitCode = -1
	}
	logger.DebugWrite("DEBUG", fmt.Sprintf("%s ssh connect exit_code=%d duration=%s", prefix, exitCode, duration))
	if success {
		logger.DebugWrite("DEBUG", fmt.Sprintf("%s ssh connect stdout| (session established)", prefix))
		return
	}
	logger.DebugWrite("ERROR", fmt.Sprintf("%s SSH connection failed:", prefix))
	for _, line := range info.FormatLines() {
		logger.DebugWrite("ERROR", fmt.Sprintf("%s|%s", prefix, strings.TrimSpace(line)))
	}
	if errMsg != "" {
		logger.DebugWrite("ERROR", fmt.Sprintf("%s ssh connect error| %s", prefix, errMsg))
	}
}
