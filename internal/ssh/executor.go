package ssh

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
	Upload(localPath, remotePath string) error
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

	var cmd *exec.Cmd
	if sudo && os.Getuid() != 0 {
		// Use non-interactive sudo to avoid hanging on password prompts.
		cmd = exec.Command("sudo", "-n", "bash", "-c", command)
	} else {
		cmd = exec.Command("bash", "-c", command)
	}

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

func (e *LocalExecutor) ExecuteScript(script string, sudo bool) (*ExecResult, error) {
	return e.Execute(script, sudo)
}

func (e *LocalExecutor) Upload(localPath, remotePath string) error {
	// 本机直接复制
	if localPath == remotePath {
		return nil
	}
	input, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	return os.WriteFile(remotePath, input, 0644)
}

func (e *LocalExecutor) Download(remotePath, localPath string) error {
	return e.Upload(remotePath, localPath)
}

func (e *LocalExecutor) Close() error {
	return nil
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
	// 始终使用 bash -c 来执行命令，确保支持 bash 内置命令（如 source）
	escapedCmd := strings.ReplaceAll(command, "'", "'\"'\"'")
	var actualCmd string
	if sudo && e.config.User != "root" {
		// Use non-interactive sudo to avoid hanging on password prompts.
		actualCmd = fmt.Sprintf("sudo -n bash -c '%s'", escapedCmd)
	} else {
		actualCmd = fmt.Sprintf("bash -c '%s'", escapedCmd)
	}

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

func (e *SSHExecutor) ExecuteScript(script string, sudo bool) (*ExecResult, error) {
	return e.Execute(script, sudo)
}

func (e *SSHExecutor) Upload(localPath, remotePath string) error {
	session, err := e.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	w, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	copyErrCh := make(chan error, 1)
	go func() {
		defer w.Close()
		if _, err := fmt.Fprintf(w, "C0644 %d %s\n", stat.Size(), stat.Name()); err != nil {
			copyErrCh <- fmt.Errorf("failed to write SCP header: %w", err)
			return
		}
		if _, err := io.Copy(w, file); err != nil {
			copyErrCh <- fmt.Errorf("failed to copy file data: %w", err)
			return
		}
		if _, err := fmt.Fprint(w, "\x00"); err != nil {
			copyErrCh <- fmt.Errorf("failed to write SCP trailer: %w", err)
			return
		}
		copyErrCh <- nil
	}()

	if err := session.Run(fmt.Sprintf("scp -t %s", remotePath)); err != nil {
		return err
	}

	if copyErr := <-copyErrCh; copyErr != nil {
		return copyErr
	}
	return nil
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
