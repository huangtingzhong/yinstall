package db

import (
	"fmt"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
)

const yacPasswordEnsureStepID = "C-001P"

// RunYACProductUserPasswordEnsure 在 YAC 安装前校验各节点产品用户 SSH 密码；
// 失败且 yac_ensure_os_password 为 true 时，在具备 root/sudo 的节点上将密码重置为 os_user_password。
// precheckMode 为 true 时仅校验，不自动改密。
func RunYACProductUserPasswordEnsure(hosts []HostExec, params map[string]interface{}, logger *logging.Logger, precheckMode bool) error {
	if len(hosts) == 0 {
		return fmt.Errorf("no hosts for YAC password ensure")
	}
	if !getParamBool(params, "yac_ensure_os_password", true) {
		logger.Info("C-001P: yac_ensure_os_password=false, skipping product user password check")
		return nil
	}

	user := getParamString(params, "os_user", "yashan")
	password := getParamString(params, "os_user_password", "")
	if password == "" {
		return fmt.Errorf("--os-user-password is required for YAC (yasboot package ce gen SSH scan)")
	}

	autoFix := !precheckMode
	sshPort := getParamIntFromParams(params, "ssh_port", 22)

	firstHost := hosts[0].Host
	logger.ConsoleWithType(yacPasswordEnsureStepID, "Ensure product user password (YAC)", firstHost, "start", "", "", 0)
	dbLogPhase(&runner.StepContext{Logger: logger, CurrentStepID: yacPasswordEnsureStepID}, "plan",
		fmt.Sprintf("hosts=%d user=%s port=%d auto_fix=%v", len(hosts), user, sshPort, autoFix))

	var fixHosts []string
	for _, h := range hosts {
		hctx := hostExecStepContext(h, params, logger)
		dbLogPhase(hctx, "op-start", fmt.Sprintf("probe user=%s port=%d", user, sshPort))
		ok, probeNote, probeErr := probeProductUserPasswordSSH(h.Host, sshPort, user, password, logger)
		if probeErr != nil {
			dbLogPhase(hctx, "op-fail", fmt.Sprintf("probe err=%s", runner.TruncateForLog(probeErr.Error(), 120)))
			return fmt.Errorf("host %s: password probe: %w", h.Host, probeErr)
		}
		if ok {
			logger.Info("C-001P: SSH password OK for %s@%s", user, h.Host)
			dbLogPhase(hctx, "op-done", "probe=ok")
			continue
		}
		dbLogPhase(hctx, "op-fail", fmt.Sprintf("probe=%s", probeNote))
		logger.Warn("C-001P: product user %q cannot SSH to %s with --os-user-password (will reset via root/sudo if allowed)", user, h.Host)
		if !autoFix {
			return fmt.Errorf("host %s: product user %q password does not match --os-user-password (precheck; auto-fix disabled)", h.Host, user)
		}
		fixHosts = append(fixHosts, h.Host)
	}

	if len(fixHosts) > 0 {
		dbLogPhase(&runner.StepContext{Logger: logger, CurrentStepID: yacPasswordEnsureStepID}, "plan",
			fmt.Sprintf("reset_hosts=%d targets=%v", len(fixHosts), fixHosts))
	}

	for i, host := range fixHosts {
		var he HostExec
		for _, h := range hosts {
			if h.Host == host {
				he = h
				break
			}
		}
		hctx := hostExecStepContext(he, params, logger)
		dbLogPhase(hctx, "reset-start", fmt.Sprintf("%d/%d host=%s", i+1, len(fixHosts), host))

		pa := commonos.CheckPrivilegedAccess(hctx)
		if !pa.Allowed {
			dbLogPhase(hctx, "reset-fail", "privileged=no")
			return fmt.Errorf("host %s: cannot reset password for %q: %s", host, user, pa.Message)
		}
		via := "sudo"
		if pa.ViaRoot {
			via = "root"
		}
		logger.Info("C-001P: resetting password for %s@%s (via %s)", user, host, via)
		dbLogPhase(hctx, "reset-priv", fmt.Sprintf("via=%s login_user=%s", via, pa.User))

		dbLogPhase(hctx, "reset-cmd", commonos.ProductUserPasswordShellCmdLabel(user))
		if err := commonos.SetProductUserPassword(hctx, user, password); err != nil {
			dbLogPhase(hctx, "reset-fail", runner.TruncateForLog(err.Error(), 120))
			return fmt.Errorf("host %s: failed to set product user password: %w", host, err)
		}
		dbLogPhase(hctx, "reset-done", fmt.Sprintf("user=%s", user))

		dbLogPhase(hctx, "op-start", "reprobe after reset")
		ok, probeNote, probeErr := probeProductUserPasswordSSH(host, sshPort, user, password, logger)
		if probeErr != nil {
			dbLogPhase(hctx, "op-fail", fmt.Sprintf("reprobe err=%s", runner.TruncateForLog(probeErr.Error(), 120)))
			return fmt.Errorf("host %s: re-probe after password reset: %w", host, probeErr)
		}
		if !ok {
			dbLogPhase(hctx, "op-fail", fmt.Sprintf("reprobe=%s", probeNote))
			return fmt.Errorf("host %s: password still invalid for %q after reset; check sshd PasswordAuthentication and user account", host, user)
		}
		dbLogPhase(hctx, "op-done", "reprobe=ok")
		logger.Info("C-001P: password reset and verified for %s@%s", user, host)
	}

	targetIPs := getParamStringSliceFromParams(params, "target_ips")
	if len(targetIPs) == 0 {
		for _, h := range hosts {
			targetIPs = append(targetIPs, h.Host)
		}
	}
	if err := probeYACPasswordMeshFromFirstNode(hosts[0], params, user, password, targetIPs, logger); err != nil {
		return err
	}

	logger.ConsoleWithType(yacPasswordEnsureStepID, "Ensure product user password (YAC)", firstHost, "success", "", "", 0)
	logger.Info("C-001P: product user password ready on all YAC nodes")
	return nil
}

// probeProductUserPasswordSSH 从控制端以密码 SSH 探测产品用户（ssh.NewExecutor + runner.ProbePasswordSSH）。
func probeProductUserPasswordSSH(host string, port int, user, password string, logger *logging.Logger) (bool, string, error) {
	return runner.ProbePasswordSSH(logger, yacPasswordEnsureStepID, ssh.Config{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
	})
}

func probeYACPasswordMeshFromFirstNode(first HostExec, params map[string]interface{}, user, password string, targetIPs []string, logger *logging.Logger) error {
	if len(targetIPs) == 0 {
		return nil
	}
	rootCtx := hostExecStepContext(first, params, logger)
	dbLogPhase(rootCtx, "plan", fmt.Sprintf("mesh-probe from=%s peers=%d", first.Host, len(targetIPs)))

	result, _ := rootCtx.Execute("command -v sshpass 2>/dev/null || echo NOT_FOUND", false)
	if result != nil && strings.Contains(result.GetStdout(), "NOT_FOUND") {
		rootCtx.Logger.Warn("C-001P: sshpass not found on %s; skip mesh SSH probe (yasboot ce gen may still require sshpass on first node)", first.Host)
		dbLogPhase(rootCtx, "mesh-skip", "reason=sshpass_missing")
		return nil
	}

	pwdQ := commonos.ShellSingleQuote(password)
	for _, ip := range targetIPs {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		testCmd := fmt.Sprintf("sshpass -p %s ssh -o StrictHostKeyChecking=no -o PreferredAuthentications=password -o PubkeyAuthentication=no -o ConnectTimeout=8 %s@%s 'echo SSH_OK' 2>&1",
			pwdQ, user, ip)
		dbLogPhase(rootCtx, "op-start", fmt.Sprintf("mesh-probe peer=%s", ip))
		res, _ := rootCtx.Execute(testCmd, false)
		stdout := ""
		exitCode := -1
		if res != nil {
			stdout = res.GetStdout()
			exitCode = res.GetExitCode()
		}
		if !strings.Contains(stdout, "SSH_OK") {
			dbLogPhase(rootCtx, "op-fail", fmt.Sprintf("mesh-probe peer=%s exit=%d", ip, exitCode))
			return fmt.Errorf("host %s: mesh SSH probe failed for %s@%s (yasboot ce gen uses same path); output=%q; install openssh-clients/sshpass on first node or fix password/ssh policy",
				first.Host, user, ip, strings.TrimSpace(stdout))
		}
		dbLogPhase(rootCtx, "op-done", fmt.Sprintf("mesh-probe peer=%s", ip))
	}
	logger.Info("C-001P: mesh SSH probe OK from %s to %d target(s)", first.Host, len(targetIPs))
	return nil
}

func hostExecStepContext(h HostExec, params map[string]interface{}, logger *logging.Logger) *runner.StepContext {
	return &runner.StepContext{
		Executor:      &c001RunnerExecutor{e: h.Executor},
		Logger:        logger,
		Params:        params,
		CurrentStepID: yacPasswordEnsureStepID,
		Results:       make(map[string]interface{}),
	}
}

// c001RunnerExecutor 将 ExecutorForC001 适配为 runner.Executor（仅 Execute/Host/Close）。
type c001RunnerExecutor struct {
	e ExecutorForC001
}

func (a *c001RunnerExecutor) Execute(cmd string, sudo bool) (runner.ExecResult, error) {
	r, err := a.e.Execute(cmd, sudo)
	if err != nil {
		return nil, err
	}
	return &c001ExecResultAdapter{r: r}, nil
}

type c001ExecResultAdapter struct {
	r ExecResultForC001
}

func (a *c001ExecResultAdapter) GetStdout() string {
	if a.r == nil {
		return ""
	}
	return a.r.GetStdout()
}

func (a *c001ExecResultAdapter) GetStderr() string {
	return ""
}

func (a *c001ExecResultAdapter) GetExitCode() int {
	if a.r == nil {
		return -1
	}
	return a.r.GetExitCode()
}

func (a *c001ExecResultAdapter) GetDuration() time.Duration {
	return 0
}

func (a *c001RunnerExecutor) Host() string {
	return a.e.Host()
}

func (a *c001RunnerExecutor) Close() error {
	return nil
}

func (a *c001RunnerExecutor) Upload(localPath, remotePath string, uploadCtx *ssh.UploadContext) error {
	return fmt.Errorf("upload not supported in C-001P adapter")
}

func getParamIntFromParams(params map[string]interface{}, key string, def int) int {
	if params == nil {
		return def
	}
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return def
	}
}
