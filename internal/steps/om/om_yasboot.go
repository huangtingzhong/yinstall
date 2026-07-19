// om_yasboot.go - OM L1 原语: yasboot process yasom / host add 封装 (DRY)
package om

import (
	"fmt"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// RunYasboot 以产品用户执行 yasboot 命令。
// 注意: ~/.yasboot/<cluster>.env 是 TOML, 不可 source; PATH 从 base_path 解析。
func RunYasboot(ctx *runner.StepContext, cmd string, check bool) (runner.ExecResult, error) {
	user := omProductUser(ctx)
	envFile := omEnvFile(ctx)
	full := fmt.Sprintf(
		`BP=""; if [ -f %s ]; then BP=$(awk -F= '/^base_path/{gsub(/[" ]/,"",$2); print $2; exit}' %s); fi; `+
			`if [ -n "$BP" ] && [ -x "$BP/bin/yasboot" ]; then export PATH="$BP/bin:$PATH"; fi; %s`,
		shellSingleQuote(envFile), shellSingleQuote(envFile), cmd)
	if check {
		return commonos.ExecuteAsUserWithCheck(ctx, user, full, true)
	}
	return commonos.ExecuteAsUser(ctx, user, full, true)
}

func shellSingleQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'"'"'`) + `'`
}

// YasomStatus 执行 process yasom status 并解析。
func YasomStatus(ctx *runner.StepContext) ([]YasomHostRow, string, error) {
	cluster := omClusterName(ctx)
	cmd := fmt.Sprintf("yasboot process yasom status -c %s", cluster)
	res, err := RunYasboot(ctx, cmd, false)
	out := ""
	if res != nil {
		out = res.GetStdout() + "\n" + res.GetStderr()
	}
	if err != nil && (res == nil || res.GetExitCode() != 0) {
		return nil, out, fmt.Errorf("yasboot process yasom status failed: %w", err)
	}
	rows := ParseYasomStatus(out)
	if len(rows) == 0 {
		return nil, out, fmt.Errorf("failed to parse yasom status output")
	}
	return rows, out, nil
}

// RecoverYasom 在本机 recover primary|secondary。
func RecoverYasom(ctx *runner.StepContext, role, listen string, force bool) error {
	role = strings.TrimSpace(role)
	if role != "primary" && role != "secondary" {
		return fmt.Errorf("invalid yasom role %q", role)
	}
	cluster := omClusterName(ctx)
	cmd := fmt.Sprintf("yasboot process yasom recover -c %s --role %s -l %s", cluster, role, listen)
	if force {
		cmd += " -f"
	}
	omLogPhase(ctx, "yasom-recover-start", role+" "+listen)
	_, err := RunYasboot(ctx, cmd, true)
	if err != nil {
		return err
	}
	omLogPhase(ctx, "yasom-recover-done", role+" "+listen)
	return nil
}

// StopYasom 停止本机/集群主 yasom (在 CUR 上执行)。
func StopYasom(ctx *runner.StepContext) error {
	cluster := omClusterName(ctx)
	cmd := fmt.Sprintf("yasboot process yasom stop -c %s", cluster)
	omLogPhase(ctx, "yasom-stop-start", cluster)
	_, err := RunYasboot(ctx, cmd, true)
	if err != nil {
		return err
	}
	omLogPhase(ctx, "yasom-stop-done", cluster)
	return nil
}

// StartYasom 启动本机 yasom 进程 (stop 后 metadata 仍为 primary 时用 start, 勿用 recover)。
func StartYasom(ctx *runner.StepContext) error {
	cluster := omClusterName(ctx)
	cmd := fmt.Sprintf("yasboot process yasom start -c %s", cluster)
	omLogPhase(ctx, "yasom-start-start", cluster)
	_, err := RunYasboot(ctx, cmd, true)
	if err != nil {
		return err
	}
	omLogPhase(ctx, "yasom-start-done", cluster)
	return nil
}

// SyncYasom 同步 OM env 到全节点。
func SyncYasom(ctx *runner.StepContext, force bool) error {
	cluster := omClusterName(ctx)
	cmd := fmt.Sprintf("yasboot process yasom sync -c %s", cluster)
	if force {
		cmd += " -f"
	}
	omLogPhase(ctx, "yasom-sync-start", cluster)
	_, err := RunYasboot(ctx, cmd, true)
	if err != nil {
		return err
	}
	omLogPhase(ctx, "yasom-sync-done", cluster)
	return nil
}

// CleanYasom 清理本机 yasom 残留。
func CleanYasom(ctx *runner.StepContext, force bool) error {
	cluster := omClusterName(ctx)
	cmd := fmt.Sprintf("yasboot process yasom clean -c %s", cluster)
	if force {
		cmd += " -f"
	}
	omLogPhase(ctx, "yasom-clean-start", cluster)
	_, err := RunYasboot(ctx, cmd, false)
	if err != nil {
		return err
	}
	omLogPhase(ctx, "yasom-clean-done", cluster)
	return nil
}

// WaitSecondarySynced 轮询 status 直至 NEW 与 primary max_seq 对齐。
func WaitSecondarySynced(ctx *runner.StepContext, newIP, newListen string, timeout, interval time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultSyncWaitTimeout
	}
	if interval <= 0 {
		interval = DefaultSyncWaitInterval
	}
	deadline := time.Now().Add(timeout)
	var last error
	for {
		rows, _, err := YasomStatus(ctx)
		if err != nil {
			last = err
		} else {
			last = SecondarySynced(rows, newIP, newListen)
			if last == nil {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if last == nil {
				last = fmt.Errorf("secondary sync wait timed out")
			}
			return fmt.Errorf("secondary not synced before promote: %w", last)
		}
		time.Sleep(interval)
	}
}

// HostAddForOM 在 CUR stage 上 gen hosts_add 并 host add (M2)。
func HostAddForOM(ctx *runner.StepContext, newIP, password string) error {
	cluster := omClusterName(ctx)
	user := omProductUser(ctx)
	stage := omStageDir(ctx)
	port := omBeginPort(ctx)
	installPath := strings.TrimSpace(ctx.GetParamString("db_install_path", ""))
	if installPath == "" {
		installPath = fmt.Sprintf("/data/%s/yasdb_home", user)
	}
	dataPath := strings.TrimSpace(ctx.GetParamString("db_data_path", ""))
	if dataPath == "" {
		dataPath = fmt.Sprintf("/data/%s/yasdb_data", user)
	}
	logPath := strings.TrimSpace(ctx.GetParamString("db_log_path", ""))
	if logPath == "" {
		logPath = fmt.Sprintf("/data/%s/log", user)
	}
	if password == "" {
		password = strings.TrimSpace(ctx.GetParamString("os_user_password", ""))
	}
	if password == "" {
		return fmt.Errorf("os user password required for host add (set --os-user-password)")
	}

	genCmd := fmt.Sprintf(
		"cd %s && yasboot config node gen -c %s -u %s -p %s --ip %s --port 22 -i %s --data-path %s --log-path %s --begin-port %d --node 1 -f",
		stage, cluster, user, password, newIP, installPath, dataPath, logPath, port)
	omLogPhase(ctx, "host-gen-start", newIP)
	if _, err := RunYasboot(ctx, genCmd, true); err != nil {
		return fmt.Errorf("yasboot config node gen for OM host failed: %w", err)
	}
	omLogPhase(ctx, "host-gen-done", newIP)

	addCmd := fmt.Sprintf("cd %s && yasboot host add -c %s -t hosts_add.toml --force", stage, cluster)
	omLogPhase(ctx, "host-add-start", newIP)
	res, err := RunYasboot(ctx, addCmd, false)
	out := ""
	if res != nil {
		out = res.GetStdout() + "\n" + res.GetStderr()
	}
	if err != nil || (res != nil && res.GetExitCode() != 0) {
		// 冲突但已在集群: 可接受
		if strings.Contains(out, "hostid") && strings.Contains(strings.ToLower(out), "conflict") {
			ctx.Logger.Warn("host add reported hostid conflict; verifying status")
		} else {
			msg := "yasboot host add failed"
			if err != nil {
				msg = fmt.Sprintf("%s: %v", msg, err)
			} else if res != nil {
				msg = fmt.Sprintf("%s: exit=%d", msg, res.GetExitCode())
			}
			return fmt.Errorf("%s\n%s", msg, out)
		}
	}
	omLogPhase(ctx, "host-add-done", newIP)
	return nil
}

// IpchangeYasom 同机改 yasom 监听 IP (yasboot ipchange yasom)。
func IpchangeYasom(ctx *runner.StepContext, tomlPath, newIP string) error {
	newIP = strings.TrimSpace(newIP)
	if newIP == "" {
		return fmt.Errorf("new IP is required for ipchange yasom")
	}
	cmd := fmt.Sprintf("yasboot ipchange yasom --new-ip %s", newIP)
	if t := strings.TrimSpace(tomlPath); t != "" {
		cmd = fmt.Sprintf("yasboot ipchange yasom -t %s --new-ip %s", t, newIP)
	}
	omLogPhase(ctx, "yasom-ipchange-start", newIP)
	_, err := RunYasboot(ctx, cmd, true)
	if err != nil {
		return err
	}
	omLogPhase(ctx, "yasom-ipchange-done", newIP)
	return nil
}

// EnsureYasbootPathInBashrc 将 base_path/bin 写入产品用户 .bashrc (幂等)。
func EnsureYasbootPathInBashrc(ctx *runner.StepContext) error {
	user := omProductUser(ctx)
	envFile := omEnvFile(ctx)
	cmd := fmt.Sprintf(
		`BP=$(awk -F= '/^base_path/{gsub(/[" ]/,"",$2); print $2; exit}' %s 2>/dev/null); `+
			`if [ -z "$BP" ]; then BP=$(ls -d /data/%s/yasdb_home/*/bin 2>/dev/null | head -1 | xargs dirname 2>/dev/null); fi; `+
			`if [ -n "$BP" ]; then grep -qF "$BP/bin" /home/%s/.bashrc 2>/dev/null || echo "export PATH=$BP/bin:$PATH" >> /home/%s/.bashrc; fi`,
		shellSingleQuote(envFile), user, user, user)
	_, err := ctx.Execute(cmd, true)
	return err
}
