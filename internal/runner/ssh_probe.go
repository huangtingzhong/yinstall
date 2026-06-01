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

	connectLabel := fmt.Sprintf("ssh connect user=%s@%s:%d auth=password", cfg.User, cfg.Host, cfg.Port)
	logger.LogCommandStart(cfg.Host, stepID, connectLabel)
	connStart := time.Now()
	exec, connErr := ssh.NewExecutor(cfg)
	if connErr != nil {
		logger.LogCommandResult(cfg.Host, stepID, "", connErr.Error(), -1, time.Since(connStart))
		if ssh.IsAuthenticationFailure(connErr) {
			return false, "auth_failed", nil
		}
		return false, "connect_failed", connErr
	}
	defer exec.Close()
	logger.LogCommandResult(cfg.Host, stepID, "(session established)", "", 0, time.Since(connStart))

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
