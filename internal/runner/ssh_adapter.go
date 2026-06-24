package runner

import (
	"time"

	"github.com/yinstall/internal/ssh"
)

// sshExecutorAdapter 将 ssh.Executor 适配为 runner.Executor（与 cli.runnerExecAdapter 一致）。
type sshExecutorAdapter struct {
	e ssh.Executor
}

// SetExecuteTimeout forwards to the underlying executor when supported (e.g. WinRM).
func (a *sshExecutorAdapter) SetExecuteTimeout(d time.Duration) {
	if a == nil || a.e == nil {
		return
	}
	if s, ok := a.e.(ExecuteTimeoutSetter); ok {
		s.SetExecuteTimeout(d)
	}
}

// SSHExecutorAdapter 包装 ssh.Executor，供 StepContext.Execute 记录 debug 命令日志。
func SSHExecutorAdapter(e ssh.Executor) Executor {
	if e == nil {
		return nil
	}
	return &sshExecutorAdapter{e: e}
}

func (a *sshExecutorAdapter) Execute(cmd string, sudo bool) (ExecResult, error) {
	return a.e.Execute(cmd, sudo)
}

func (a *sshExecutorAdapter) Host() string {
	return a.e.Host()
}

func (a *sshExecutorAdapter) Close() error {
	return a.e.Close()
}

func (a *sshExecutorAdapter) Upload(localPath, remotePath string, uploadCtx *ssh.UploadContext) error {
	return a.e.Upload(localPath, remotePath, uploadCtx)
}

// SSHExecutor returns the underlying ssh.Executor (transport detection, tests).
func (a *sshExecutorAdapter) SSHExecutor() ssh.Executor {
	if a == nil {
		return nil
	}
	return a.e
}
