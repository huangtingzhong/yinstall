// stress_util.go - stressos 步骤通用工具函数
// 集中封装归档目录路径、超时计算、文件写入、命令执行、警告收集等共享能力。
// 命名与实现风格对齐 collect_util.go，复用相同设计模式。
package stressos

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

// embedFS 包含 embed/ 目录下全部内嵌文件（fio job 文件、shell 脚本等）。
//
//go:embed embed
var embedFS embed.FS

// ─── 路径计算 ────────────────────────────────────────────────────────────────

// stressRootDir 从 ctx.Params 读取 output_dir（控制端归档根目录）。
func stressRootDir(ctx *runner.StepContext) string {
	return ctx.GetParamString("output_dir", "")
}

// stressHostDir 返回当前主机的归档子目录：<output_dir>/hosts/<host>/
func stressHostDir(ctx *runner.StepContext) string {
	host := ctx.Executor.Host()
	safeHost := strings.NewReplacer(":", "_").Replace(host)
	return filepath.Join(stressRootDir(ctx), "hosts", safeHost)
}

// ─── 文件写入（控制端，all local path operations） ───────────────────────────

// writeTextFile 将纯文本内容写入控制端路径，自动创建父目录。
func writeTextFile(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// writeJSON 将 data 序列化为缩进 JSON 后写入控制端路径，自动创建父目录。
func writeJSON(path string, data interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	return os.WriteFile(path, b, 0o644)
}

// appendTextFile 向控制端文件追加文本（文件不存在则创建）。
func appendTextFile(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// ─── 超时参数 ────────────────────────────────────────────────────────────────

// stressCmdTimeout 返回默认每命令超时（来自 --stress-cmd-timeout）。
func stressCmdTimeout(ctx *runner.StepContext) time.Duration {
	sec := ctx.GetParamInt("stress_cmd_timeout", 600)
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// stressBenchTimeout 返回压测命令专用超时（duration + 60s buffer）。
func stressBenchTimeout(duration time.Duration) time.Duration {
	if duration <= 0 {
		return 0
	}
	return duration + 90*time.Second
}

// stressSourceBuildTimeout 返回源码编译脚本超时：max(stress_cmd_timeout*3, 45min)。
func stressSourceBuildTimeout(ctx *runner.StepContext) time.Duration {
	const minBuild = 45 * time.Minute
	base := stressCmdTimeout(ctx)
	if base <= 0 {
		return minBuild
	}
	triple := base * 3
	if triple < minBuild {
		return minBuild
	}
	return triple
}

// stressLogPhase 写入结构化 debug 里程碑（仅 debug 文件，不进终端）。
func stressLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, msg)
}

// stressSysbenchSummary 从 sysbench 输出提取关键指标行（events/s、total time、latency）。
func stressSysbenchSummary(stdout string) string {
	var parts []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "events per second:") ||
			strings.Contains(lower, "total time:") ||
			strings.Contains(lower, "latency min") {
			parts = append(parts, line)
		}
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) == 0 {
		return "summary=(no metrics in stdout)"
	}
	return "summary=" + strings.Join(parts, " | ")
}

// stressFIOSummary 从 fio 人类可读 stdout 提取 IOPS/BW/lat 摘要行。
func stressFIOSummary(stdout string) string {
	var parts []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "IOPS=") || strings.Contains(line, "BW=") ||
			strings.Contains(line, "lat (") {
			parts = append(parts, line)
		}
		if len(parts) >= 4 {
			break
		}
	}
	if len(parts) == 0 {
		return "summary=(no metrics in stdout)"
	}
	return "summary=" + strings.Join(parts, " | ")
}

// stressPingSummary 从 ping -q 输出提取 rtt / packet loss 摘要行。
func stressPingSummary(stdout string) string {
	var parts []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "rtt min/avg/max") ||
			strings.Contains(lower, "packet loss") ||
			strings.Contains(lower, "round-trip") {
			parts = append(parts, line)
		}
		if len(parts) >= 2 {
			break
		}
	}
	if len(parts) == 0 {
		return "summary=(no metrics in stdout)"
	}
	return "summary=" + strings.Join(parts, " | ")
}

// ─── 超时退出码（与 collect 保持一致） ──────────────────────────────────────

const stressTimeoutExitCode = 124

func isStressTimeoutExit(code int) bool { return code == stressTimeoutExitCode }

// ─── context-aware 执行器接口 ────────────────────────────────────────────────

// contextualExecutor 由 stressExecAdapter（internal/cli/stressos.go）实现。
// stressos 步骤通过类型断言调用此接口，以施加 SSH session 超时（方案D，与 collect 相同）。
type contextualExecutor interface {
	ExecuteCtx(ctx context.Context, cmd string, sudo bool) (runner.ExecResult, error)
}

// stressExecute 执行远端命令；timeout>0 时通过 SSH session 超时控制最长时间。
// 始终写入 LogCommandStart/LogCommandResult（与 ctx.Execute 一致），避免长耗时压测时 debug 无命令记录。
func stressExecute(ctx *runner.StepContext, cmd string, sudo bool, timeout time.Duration) (runner.ExecResult, error) {
	if ctx == nil || ctx.Executor == nil {
		return nil, fmt.Errorf("executor not available")
	}
	host := ctx.Executor.Host()
	stepID := ctx.CurrentStepID
	if ctx.Logger != nil {
		ctx.Logger.LogCommandStart(host, stepID, cmd)
	}
	start := time.Now()

	var result runner.ExecResult
	var err error
	if timeout > 0 {
		if ce, ok := ctx.Executor.(contextualExecutor); ok {
			goCtx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			result, err = ce.ExecuteCtx(goCtx, cmd, sudo)
		} else {
			result, err = ctx.Executor.Execute(cmd, sudo)
		}
	} else {
		result, err = ctx.Executor.Execute(cmd, sudo)
	}

	if ctx.Logger != nil {
		dur := time.Since(start)
		if result != nil {
			ctx.Logger.LogCommandResult(host, stepID,
				result.GetStdout(), result.GetStderr(), result.GetExitCode(), dur)
		} else if err != nil {
			ctx.Logger.LogCommandResult(host, stepID, "", err.Error(), -1, dur)
		}
	}
	if result != nil && timeout > 0 && isStressTimeoutExit(result.GetExitCode()) {
		appendWarning(ctx, ctx.CurrentStepID,
			fmt.Sprintf("command timed out after %ds: %s", int(timeout.Seconds()), truncateCmdForLog(cmd)))
	}
	return result, err
}

// stressRunShell 以临时文件方式执行 shell 脚本（与 collectRunShell 完全对称）。
// 避免多行脚本内嵌 bash -c 时的引号/换行问题。
func stressRunShell(ctx *runner.StepContext, script string, sudo bool, timeout time.Duration) (string, error) {
	localTmp, err := os.CreateTemp("", "stress_sh_*.sh")
	if err != nil {
		return "", fmt.Errorf("create local tmp shell file: %w", err)
	}
	localTmpName := localTmp.Name()
	defer os.Remove(localTmpName)

	if _, err := localTmp.WriteString(script + "\n"); err != nil {
		localTmp.Close()
		return "", fmt.Errorf("write local tmp shell: %w", err)
	}
	localTmp.Close()

	remotePath := fmt.Sprintf("/tmp/.stress_sh_%d.sh", time.Now().UnixNano())
	ctx.LogScriptPreview("shell", "remote="+remotePath, script)
	if err := ctx.Executor.Upload(localTmpName, remotePath, ctx.UploadContext()); err != nil {
		return "", fmt.Errorf("upload shell file: %w", err)
	}

	_, _ = stressExecute(ctx, fmt.Sprintf("chmod 755 %s", remotePath), false, 0)

	result, execErr := stressExecute(ctx, fmt.Sprintf("bash %s", remotePath), sudo, timeout)

	_, _ = stressExecute(ctx, fmt.Sprintf("rm -f %s", remotePath), false, 0)

	if result != nil && isStressTimeoutExit(result.GetExitCode()) {
		stdout := result.GetStdout()
		return stdout, fmt.Errorf("shell script timed out after %ds", int(timeout.Seconds()))
	}
	stdout := ""
	if result != nil {
		stdout = result.GetStdout()
	}
	return stdout, execErr
}

// stressRunFIO 上传 fio job 文件到目标机并执行，输出 JSON 和文本结果。
// 返回 json output 和 text output。
func stressRunFIO(ctx *runner.StepContext, jobContent, remoteJobPath, fioArgs string, timeout time.Duration) (string, error) {
	// 写本地临时文件
	localTmp, err := os.CreateTemp("", "stress_fio_*.fio")
	if err != nil {
		return "", fmt.Errorf("create local tmp fio file: %w", err)
	}
	localTmpName := localTmp.Name()
	defer os.Remove(localTmpName)

	if _, err := localTmp.WriteString(jobContent); err != nil {
		localTmp.Close()
		return "", fmt.Errorf("write local tmp fio: %w", err)
	}
	localTmp.Close()

	if err := ctx.Executor.Upload(localTmpName, remoteJobPath, ctx.UploadContext()); err != nil {
		return "", fmt.Errorf("upload fio job file: %w", err)
	}

	ctx.LogScriptPreview("fio", "remote="+remoteJobPath, jobContent)

	cmd := fmt.Sprintf("fio %s %s", remoteJobPath, fioArgs)
	result, execErr := stressExecute(ctx, cmd, false, timeout)

	// 清理远端 job 文件（不计超时）
	_, _ = stressExecute(ctx, fmt.Sprintf("rm -f %s", remoteJobPath), false, 0)

	stdout := ""
	if result != nil {
		stdout = result.GetStdout()
	}
	if execErr != nil {
		return stdout, execErr
	}
	if result != nil && result.GetExitCode() != 0 {
		stderr := ""
		if result != nil {
			stderr = strings.TrimSpace(result.GetStderr())
		}
		return stdout, fmt.Errorf("fio exit=%d: %s", result.GetExitCode(), stderr)
	}
	return stdout, nil
}

// readEmbedFIOJob 从内嵌 embed/scripts/fio/<name> 读取 fio job 文件模板内容。
func readEmbedFIOJob(name string) (string, error) {
	data, err := embedFS.ReadFile("embed/scripts/fio/" + name)
	if err != nil {
		return "", fmt.Errorf("read embedded fio job %s: %w", name, err)
	}
	return string(data), nil
}

// readEmbedShell 从内嵌 embed/scripts/shell/runtime/<name> 读取 shell 脚本内容。
func readEmbedShell(name string) (string, error) {
	data, err := embedFS.ReadFile("embed/scripts/shell/runtime/" + name)
	if err != nil {
		return "", fmt.Errorf("read embedded shell script %s: %w", name, err)
	}
	return string(data), nil
}

// applyFIOTemplate 将 fio job 文件模板中的占位符替换为实际参数。
func applyFIOTemplate(template string, vars map[string]string) string {
	result := template
	for k, v := range vars {
		result = strings.ReplaceAll(result, "%%"+k+"%%", v)
	}
	return result
}

// ─── 主机身份（对齐 collect R-010 / os/identity/summary.json） ───────────────

const keyStressConnectivityIdentity = "stress_connectivity_identity"

// stressOSFamily 返回目标机 OS 系列字符串（英文），与 collect osFamilyString 一致。
func stressOSFamily(ctx *runner.StepContext) string {
	if ctx.OSInfo == nil {
		return "unknown"
	}
	switch {
	case ctx.OSInfo.IsRHEL7:
		return "rhel7"
	case ctx.OSInfo.IsRHEL8:
		return "rhel8"
	case ctx.OSInfo.IsKylin:
		return "kylin"
	case ctx.OSInfo.IsUOS:
		return "uos"
	default:
		return "other"
	}
}

// stressProbe 在目标机执行只读命令并返回 TrimSpace 后的 stdout。
func stressProbe(ctx *runner.StepContext, cmd string) string {
	if ctx == nil || ctx.Executor == nil {
		return ""
	}
	r, _ := stressExecute(ctx, cmd, false, stressCmdTimeout(ctx))
	if r == nil || r.GetExitCode() != 0 {
		return ""
	}
	return strings.TrimSpace(r.GetStdout())
}

func stressSetIfEmpty(m map[string]string, key, val string) {
	val = strings.TrimSpace(val)
	if val == "" {
		return
	}
	if strings.TrimSpace(m[key]) == "" {
		m[key] = val
	}
}

// stressHostIdentity 构建主机身份摘要：优先 S-01 快照，再 OSInfo，最后远端探测兜底。
func stressHostIdentity(ctx *runner.StepContext) map[string]string {
	m := make(map[string]string)
	if raw, ok := ctx.Results[keyStressConnectivityIdentity]; ok {
		switch v := raw.(type) {
		case map[string]string:
			for k, val := range v {
				stressSetIfEmpty(m, k, val)
			}
		case map[string]interface{}:
			for k, val := range v {
				if s, ok := val.(string); ok {
					stressSetIfEmpty(m, k, s)
				}
			}
		}
	}
	if ctx.OSInfo != nil {
		o := ctx.OSInfo
		stressSetIfEmpty(m, "os_name", o.Name)
		stressSetIfEmpty(m, "os_version", o.Version)
		stressSetIfEmpty(m, "os_id", o.ID)
		stressSetIfEmpty(m, "os_kernel", o.Kernel)
		stressSetIfEmpty(m, "os_arch", o.Arch)
		stressSetIfEmpty(m, "pkg_manager", o.PkgManager)
		stressSetIfEmpty(m, "os_family", stressOSFamily(ctx))
		if o.Arch != "" {
			stressSetIfEmpty(m, "arch", o.Arch)
		}
	}
	stressSetIfEmpty(m, "hostname", stressProbe(ctx, "hostname -f"))
	stressSetIfEmpty(m, "hostname", stressProbe(ctx, "hostname"))
	stressSetIfEmpty(m, "uname", stressProbe(ctx, "uname -r"))
	stressSetIfEmpty(m, "arch", stressProbe(ctx, "uname -m"))
	stressSetIfEmpty(m, "cpu_cores", stressProbe(ctx, "nproc 2>/dev/null || grep -c processor /proc/cpuinfo"))
	stressSetIfEmpty(m, "memory_total", stressProbe(ctx, "free -h 2>/dev/null | grep Mem | awk '{print $2}'"))
	stressSetIfEmpty(m, "os_family", stressOSFamily(ctx))
	return m
}

// stressBuildMeta 生成 S-02 写入的 meta.json 内容（含 collect 对齐的身份字段）。
func stressBuildMeta(ctx *runner.StepContext, stressDirs []string) map[string]interface{} {
	id := stressHostIdentity(ctx)
	host := "unknown"
	if ctx != nil && ctx.Executor != nil {
		host = ctx.Executor.Host()
	}
	meta := map[string]interface{}{
		"host":        host,
		"started_at":  time.Now().UTC().Format(time.RFC3339),
		"status":      "in_progress",
		"stress_dirs": stressDirs,
	}
	for _, k := range []string{
		"hostname", "os_name", "os_version", "os_id", "os_kernel", "os_arch",
		"os_family", "arch", "pkg_manager", "cpu_cores", "memory_total", "uname",
	} {
		if v := id[k]; v != "" {
			meta[k] = v
		}
	}
	return meta
}

// ─── 错误与警告收集 ──────────────────────────────────────────────────────────

const (
	keyStressErrors   = "stress_errors"
	keyStressWarnings = "stress_warnings"
)

// appendError 向 ctx.Results[stress_errors] 追加一条错误记录。
func appendError(ctx *runner.StepContext, stepID, msg string) {
	entry := map[string]string{"step": stepID, "level": "error", "message": msg}
	existing, _ := ctx.Results[keyStressErrors].([]map[string]string)
	ctx.Results[keyStressErrors] = append(existing, entry)
	ctx.Logger.Error("[%s] stress error: %s", stepID, msg)
}

// appendWarning 向 ctx.Results[stress_warnings] 追加一条警告记录。
func appendWarning(ctx *runner.StepContext, stepID, msg string) {
	entry := map[string]string{"step": stepID, "level": "warning", "message": msg}
	existing, _ := ctx.Results[keyStressWarnings].([]map[string]string)
	ctx.Results[keyStressWarnings] = append(existing, entry)
	ctx.Logger.Warn("[%s] stress warning: %s", stepID, msg)
}

// truncateCmdForLog 将过长命令截断到 120 字符，用于 warning 信息。
func truncateCmdForLog(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if len(cmd) <= 120 {
		return cmd
	}
	return cmd[:120] + "..."
}

// ─── 参数读取辅助 ────────────────────────────────────────────────────────────

// getBool 读取 ctx.Params 中的 bool 参数，并提供默认值。
func getBool(ctx *runner.StepContext, key string, defaultVal bool) bool {
	return ctx.GetParamBool(key, defaultVal)
}

// getStr 读取 ctx.Params 中的 string 参数，并提供默认值。
func getStr(ctx *runner.StepContext, key, defaultVal string) string {
	return ctx.GetParamString(key, defaultVal)
}

// getInt 读取 ctx.Params 中的 int 参数，并提供默认值。
func getInt(ctx *runner.StepContext, key string, defaultVal int) int {
	return ctx.GetParamInt(key, defaultVal)
}
