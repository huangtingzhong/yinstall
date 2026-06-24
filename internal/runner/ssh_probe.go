package runner

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/ssh"
)

const defaultSSHProbeCmd = "echo SSH_OK"

// ProbePasswordSSH 从控制端以密码建立 SSH 并执行探测命令（默认 echo SSH_OK）。
// 命令经 StepContext.Execute 写入 debug（>>> cmd / exit_code / stdout|stderr）；连接失败单独记一条 connect 摘要。
func ProbePasswordSSH(logger *logging.Logger, stepID string, cfg ssh.Config) (ok bool, note string, err error) {
	if logger == nil {
		return false, "", fmt.Errorf("logger is nil")
	}
	if strings.TrimSpace(cfg.Host) == "" {
		return false, "", fmt.Errorf("ssh host is empty")
	}
	if cfg.Port <= 0 {
		cfg.Port = 22
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	if cfg.KnownHosts == "" {
		cfg.KnownHosts = "ignore"
	}
	cfg.AuthMethod = "password"
	cfg.Logger = logger
	cfg.StepID = stepID

	connectInfo := ssh.BuildConnectAttemptInfo(cfg, true, "")
	ssh.LogConnectStart(logger, connectInfo, stepID, 1, 1)
	connStart := time.Now()
	exec, connErr := ssh.NewExecutor(cfg)
	if connErr != nil {
		ssh.LogConnectResult(logger, connectInfo, stepID, false, connErr.Error(), time.Since(connStart))
		if ssh.IsAuthenticationFailure(connErr) {
			return false, "auth_failed", ssh.WrapConnectError(connectInfo, connErr)
		}
		return false, "connect_failed", ssh.WrapConnectError(connectInfo, connErr)
	}
	defer exec.Close()
	ssh.LogConnectResult(logger, connectInfo, stepID, true, "", time.Since(connStart))

	ctx := &StepContext{
		Executor:      SSHExecutorAdapter(exec),
		Logger:        logger,
		CurrentStepID: stepID,
	}
	result, execErr := ctx.Execute(defaultSSHProbeCmd, false)
	if execErr != nil {
		return false, "exec_failed", nil
	}
	if result == nil || result.GetExitCode() != 0 || !strings.Contains(result.GetStdout(), "SSH_OK") {
		note := "probe_failed"
		if result != nil && result.GetExitCode() != 0 {
			note = fmt.Sprintf("exit=%d", result.GetExitCode())
		}
		return false, note, nil
	}
	return true, "ok", nil
}
