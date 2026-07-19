// collect_util.go - collect 步骤通用工具函数
// 集中封装归档目录路径计算、JSON/文本写入、命令执行结果保存、错误收集等共享能力，
// 避免各步骤重复相同逻辑。
package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// 归档目录与路径

// collectRootDir 从 ctx.Params 读取 output_dir 键，返回本次采集根目录。
func collectRootDir(ctx *runner.StepContext) string {
	return ctx.GetParamString("output_dir", "")
}

// collectHostDir 返回当前主机的归档子目录：<output_dir>/hosts/<host>/
func collectHostDir(ctx *runner.StepContext) string {
	host := ctx.Executor.Host()
	// 将 IP:port 中的冒号替换为下划线，保证目录名合法
	safeHost := strings.NewReplacer(":", "_").Replace(host)
	return filepath.Join(collectRootDir(ctx), "hosts", safeHost)
}

// 文件写入

// writeJSON 将 data 序列化为缩进 JSON 后写入本地 path（控制端文件操作）。
// 自动创建父目录。
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

// writeTextFile 将纯文本内容写入本地 path（控制端文件操作）。
// 自动创建父目录。
func writeTextFile(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// 命令超时（秒；0 表示不限制，由 collect CLI 注入 ctx.Params）

func collectCmdTimeout(ctx *runner.StepContext) time.Duration {
	sec := ctx.GetParamInt("collect_cmd_timeout", 30)
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func collectSQLTimeout(ctx *runner.StepContext) time.Duration {
	sec := ctx.GetParamInt("collect_sql_timeout", 30)
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func collectLogTimeout(ctx *runner.StepContext) time.Duration {
	sec := ctx.GetParamInt("collect_log_timeout", 60)
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// collectTimeoutExitCode 是 SSH session 超时时返回的退出码（与 GNU timeout 124 保持一致）。
const collectTimeoutExitCode = 124

// isCollectTimeoutExit 判断退出码是否为超时（SSH session 超时返回 124）。
func isCollectTimeoutExit(exitCode int) bool { return exitCode == collectTimeoutExitCode }

func truncateCmdForLog(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if len(cmd) <= 120 {
		return cmd
	}
	return cmd[:120] + "..."
}

// collectLogPhase 写入结构化 debug 里程碑（仅 debug 文件，不进终端）。
func collectLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, runner.StepMsg(ctx, msg))
}

// collectOutputStats 返回 stdout 体量摘要（字节数、行数），用于 op-done / query-done 等。
func collectOutputStats(stdout string) string {
	s := strings.TrimSpace(stdout)
	if s == "" {
		return "bytes=0 lines=0"
	}
	lines := strings.Count(s, "\n") + 1
	return fmt.Sprintf("bytes=%d lines=%d", len(s), lines)
}

// collectSQLLabel 返回 SQL 首行摘要（用于 query-start debug）。
func collectSQLLabel(sql string) string {
	line := strings.TrimSpace(sql)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if len(line) > 80 {
		return line[:80] + "..."
	}
	return line
}

// collectDestLabel 返回相对主机归档目录的短 dest 路径（用于 debug）。
func collectDestLabel(ctx *runner.StepContext, destPath string) string {
	if ctx == nil {
		return filepath.Base(destPath)
	}
	hostDir := collectHostDir(ctx)
	if rel, err := filepath.Rel(hostDir, destPath); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(destPath)
}

func warnCommandTimeout(ctx *runner.StepContext, cmd string, timeout time.Duration, exitCode int) {
	if timeout <= 0 || !isCollectTimeoutExit(exitCode) {
		return
	}
	appendWarning(ctx, fmt.Sprintf("command timed out after %ds: %s", int(timeout.Seconds()), truncateCmdForLog(cmd)))
}

// contextualExecutor 由 collectExecAdapter（internal/cli/collect.go）实现。
// collect 步骤通过类型断言调用此接口，以施加 SSH session 超时（方案D）。
// 接口定义在此包内，不对外暴露。
type contextualExecutor interface {
	ExecuteCtx(ctx context.Context, cmd string, sudo bool) (runner.ExecResult, error)
}

// collectExecute 执行远端命令；timeout>0 时通过 SSH session 超时（方案D）控制最长执行时间。
// SSH/Local 实时写 debug；未挂接流式时事后 LogCommandResult。
func collectExecute(ctx *runner.StepContext, cmd string, sudo bool, timeout time.Duration) (runner.ExecResult, error) {
	if ctx == nil || ctx.Executor == nil {
		return nil, fmt.Errorf("executor not available")
	}
	host := ctx.Executor.Host()
	stepID := ctx.CurrentStepID
	if ctx.Logger != nil {
		ctx.Logger.LogCommandStart(host, stepID, cmd)
	}
	finish := runner.BindCommandDebugStream(ctx.Executor, ctx.Logger, host, stepID)
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

	finish(result, err, time.Since(start))
	if result != nil && timeout > 0 && isCollectTimeoutExit(result.GetExitCode()) {
		warnCommandTimeout(ctx, cmd, timeout, result.GetExitCode())
	}
	return result, err
}

// collectExecuteAsUserWithEnv 以产品用户 source env 后执行命令（SSH session 超时）。
// 通过 commonos.BuildAsUserEnvCommand 构造完整命令字符串，再由 collectExecute 施加超时。
// 此方式对 heredoc 命令（如 yasql <<EOF...）完全兼容（方案D 核心优势）。
func collectExecuteAsUserWithEnv(ctx *runner.StepContext, osUser, envFile, command string, timeout time.Duration) (runner.ExecResult, error) {
	fullCmd, err := commonos.BuildAsUserEnvCommand(ctx, osUser, envFile, command)
	if err != nil {
		return nil, err
	}
	return collectExecute(ctx, fullCmd, false, timeout)
}

// collectRunSQL 以 osUser 身份执行 yasql sysdba 查询（临时文件方式）。
// 将 SQL 写入本地临时文件，上传到目标机后用 yasql -f 执行，执行完毕后清理。
// 此方式完全避免 heredoc + bash -c 的 shell 解析冲突，同时保留 SSH session 超时保护。
func collectRunSQL(ctx *runner.StepContext, osUser, envFile, sql string, timeout time.Duration) (string, error) {
	// 1. 在控制端写临时 SQL 文件（内容无需转义，由文件系统传输保证原文）
	localTmp, err := os.CreateTemp("", "collect_sql_*.sql")
	if err != nil {
		return "", fmt.Errorf("create local tmp sql file: %w", err)
	}
	localTmpName := localTmp.Name()
	defer os.Remove(localTmpName)

	// yasql -f 要求 SQL 语句以分号结尾才会执行；对缺失分号的 SQL 自动补加
	sqlContent := strings.TrimRight(strings.TrimSpace(sql), ";") + ";\n"
	if _, err := localTmp.WriteString(sqlContent); err != nil {
		localTmp.Close()
		return "", fmt.Errorf("write local tmp sql: %w", err)
	}
	localTmp.Close()

	// 2. 上传到目标机 /tmp/
	remotePath := fmt.Sprintf("/tmp/.collect_sql_%d.sql", time.Now().UnixNano())
	ctx.LogScriptPreview("sql", "remote="+remotePath, sql)
	if err := ctx.Executor.Upload(localTmpName, remotePath, ctx.UploadContext()); err != nil {
		return "", fmt.Errorf("upload sql file: %w", err)
	}

	// 3. 执行 yasql -S -f <remotePath>（无 heredoc，无 shell 解析风险）
	yasqlCmd := fmt.Sprintf("yasql -S / as sysdba -f %s", remotePath)
	result, execErr := collectExecuteAsUserWithEnv(ctx, osUser, envFile, yasqlCmd, timeout)

	// 4. 清理远端临时文件（不计入超时，单独执行）
	_, _ = collectExecute(ctx, fmt.Sprintf("rm -f %s", remotePath), false, 0)

	if execErr != nil {
		return "", execErr
	}
	if result == nil {
		return "", nil
	}
	stdout := result.GetStdout()
	if isCollectTimeoutExit(result.GetExitCode()) {
		return stdout, fmt.Errorf("yasql timed out after %ds", int(timeout.Seconds()))
	}
	if result.GetExitCode() != 0 {
		stderr := strings.TrimSpace(result.GetStderr())
		return stdout, fmt.Errorf("yasql exit=%d: %s", result.GetExitCode(), stderr)
	}
	return stdout, nil
}

// collectRunShell 以 SSH 登录用户执行 shell 脚本（临时文件方式，与 collectRunSQL 对称）。
// 将脚本内容写入本地临时文件，上传后用 bash 执行，完毕后清理。
// 此方式避免多行脚本内嵌 bash -c '...' 时的引号/换行脆弱性。
func collectRunShell(ctx *runner.StepContext, script string, sudo bool, timeout time.Duration) (string, error) {
	localTmp, err := os.CreateTemp("", "collect_sh_*.sh")
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

	remotePath := fmt.Sprintf("/tmp/.collect_sh_%d.sh", time.Now().UnixNano())
	ctx.LogScriptPreview("shell", "remote="+remotePath, script)
	if err := ctx.Executor.Upload(localTmpName, remotePath, ctx.UploadContext()); err != nil {
		return "", fmt.Errorf("upload shell file: %w", err)
	}

	// chmod 755：确保目标机上 bash 可读，as_db_user 场景下目标用户也能读取
	_, _ = collectExecute(ctx, fmt.Sprintf("chmod 755 %s", remotePath), false, 0)

	result, execErr := collectExecute(ctx, fmt.Sprintf("bash %s", remotePath), sudo, timeout)

	// 清理远端临时文件（不计入超时）
	_, _ = collectExecute(ctx, fmt.Sprintf("rm -f %s", remotePath), false, 0)

	if result != nil && isCollectTimeoutExit(result.GetExitCode()) {
		return result.GetStdout(), fmt.Errorf("shell script timed out after %ds", int(timeout.Seconds()))
	}
	stdout := ""
	if result != nil {
		stdout = result.GetStdout()
	}
	return stdout, execErr
}

// collectRunShellAsUser 以产品用户 source env 后执行 shell 脚本（临时文件方式）。
// 同 collectRunShell，但以 DB OS 用户身份运行（支持访问 YASDB_HOME、yasql 等）。
func collectRunShellAsUser(ctx *runner.StepContext, osUser, envFile, script string, timeout time.Duration) (string, error) {
	localTmp, err := os.CreateTemp("", "collect_sh_*.sh")
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

	remotePath := fmt.Sprintf("/tmp/.collect_sh_%d.sh", time.Now().UnixNano())
	ctx.LogScriptPreview("shell", "remote="+remotePath+" user="+osUser, script)
	if err := ctx.Executor.Upload(localTmpName, remotePath, ctx.UploadContext()); err != nil {
		return "", fmt.Errorf("upload shell file: %w", err)
	}

	// chmod 755：确保产品用户（非 root）可以读取并执行
	_, _ = collectExecute(ctx, fmt.Sprintf("chmod 755 %s", remotePath), false, 0)

	result, execErr := collectExecuteAsUserWithEnv(ctx, osUser, envFile, fmt.Sprintf("bash %s", remotePath), timeout)

	// 清理远端临时文件
	_, _ = collectExecute(ctx, fmt.Sprintf("rm -f %s", remotePath), false, 0)

	if result != nil && isCollectTimeoutExit(result.GetExitCode()) {
		return result.GetStdout(), fmt.Errorf("shell script timed out after %ds", int(timeout.Seconds()))
	}
	stdout := ""
	if result != nil {
		stdout = result.GetStdout()
	}
	return stdout, execErr
}

// 命令执行与保存

// runAndSave 在目标主机执行 cmd，将 stdout 写入控制端 destPath（使用 --collect-cmd-timeout）。
func runAndSave(ctx *runner.StepContext, cmd string, destPath string, sudo ...bool) error {
	useSudo := false
	if len(sudo) > 0 {
		useSudo = sudo[0]
	}
	return runAndSaveWithTimeout(ctx, cmd, destPath, collectCmdTimeout(ctx), useSudo)
}

// runAndSaveWithTimeout 指定超时上限的 runAndSave；timeout=0 表示不限制。
func runAndSaveWithTimeout(ctx *runner.StepContext, cmd string, destPath string, timeout time.Duration, sudo bool) error {
	dest := collectDestLabel(ctx, destPath)
	collectLogPhase(ctx, "op-start", fmt.Sprintf("dest=%s cmd=%s", dest, truncateCmdForLog(cmd)))

	result, _ := collectExecute(ctx, cmd, sudo, timeout)
	output := ""
	if result != nil {
		output = strings.TrimRight(result.GetStdout(), "\n")
	}
	exitCode := 0
	if result != nil {
		exitCode = result.GetExitCode()
	}
	stats := collectOutputStats(output)
	timedOut := isCollectTimeoutExit(exitCode)

	if exitCode != 0 && !timedOut {
		stderr := ""
		if result != nil {
			stderr = strings.TrimSpace(result.GetStderr())
		}
		msg := fmt.Sprintf("cmd=%q exit=%d stderr=%s", cmd, exitCode, stderr)
		appendWarning(ctx, msg)
		collectLogPhase(ctx, "op-fail",
			fmt.Sprintf("dest=%s exit=%d %s err=%s", dest, exitCode, stats, stderr))
	} else if timedOut {
		collectLogPhase(ctx, "op-fail",
			fmt.Sprintf("dest=%s exit=%d %s err=timeout", dest, exitCode, stats))
	}

	if output == "" && exitCode != 0 && !timedOut {
		return fmt.Errorf("command failed: %s", cmd)
	}
	if err := writeTextFile(destPath, output+"\n"); err != nil {
		collectLogPhase(ctx, "op-fail", fmt.Sprintf("dest=%s write_err=%v", dest, err))
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	if exitCode == 0 || timedOut {
		collectLogPhase(ctx, "op-done", fmt.Sprintf("dest=%s exit=%d %s", dest, exitCode, stats))
	}
	return nil
}

// runAndSaveAsUser 以产品用户 source env 后执行命令并保存输出（使用指定超时，0=不限制）。
func runAndSaveAsUser(ctx *runner.StepContext, osUser, envFile, cmd, destPath string, timeout time.Duration) error {
	dest := collectDestLabel(ctx, destPath)
	collectLogPhase(ctx, "op-start", fmt.Sprintf("dest=%s user=%s cmd=%s", dest, osUser, truncateCmdForLog(cmd)))

	result, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile, cmd, timeout)
	output := ""
	if result != nil {
		output = strings.TrimRight(result.GetStdout(), "\n")
	}
	exitCode := 0
	if result != nil {
		exitCode = result.GetExitCode()
	}
	stats := collectOutputStats(output)
	timedOut := isCollectTimeoutExit(exitCode)

	if exitCode != 0 && !timedOut {
		stderr := ""
		if result != nil {
			stderr = strings.TrimSpace(result.GetStderr())
		}
		msg := fmt.Sprintf("cmd=%q exit=%d stderr=%s", cmd, exitCode, stderr)
		appendWarning(ctx, msg)
		collectLogPhase(ctx, "op-fail",
			fmt.Sprintf("dest=%s exit=%d %s err=%s", dest, exitCode, stats, stderr))
	} else if timedOut {
		collectLogPhase(ctx, "op-fail",
			fmt.Sprintf("dest=%s exit=%d %s err=timeout", dest, exitCode, stats))
	}

	if output == "" && exitCode != 0 && !timedOut {
		return fmt.Errorf("command failed: %s", cmd)
	}
	if err := writeTextFile(destPath, output+"\n"); err != nil {
		collectLogPhase(ctx, "op-fail", fmt.Sprintf("dest=%s write_err=%v", dest, err))
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	if exitCode == 0 || timedOut {
		collectLogPhase(ctx, "op-done", fmt.Sprintf("dest=%s exit=%d %s", dest, exitCode, stats))
	}
	return nil
}

// 错误与警告收集

const (
	keyCollectErrors   = "collect_errors"
	keyCollectWarnings = "collect_warnings"
)

// appendError 向 ctx.Results[collect_errors] 追加一条结构化错误记录。
func appendError(ctx *runner.StepContext, msg string) {
	stepID := ctx.CurrentStepID
	entry := map[string]string{"step": stepID, "level": "error", "message": msg}
	existing, _ := ctx.Results[keyCollectErrors].([]map[string]string)
	ctx.Results[keyCollectErrors] = append(existing, entry)
	ctx.Logger.Error("[%s] collect error: %s", stepID, msg)
}

// appendWarning 向 ctx.Results[collect_warnings] 追加一条结构化警告记录。
func appendWarning(ctx *runner.StepContext, msg string) {
	stepID := ctx.CurrentStepID
	entry := map[string]string{"step": stepID, "level": "warning", "message": msg}
	existing, _ := ctx.Results[keyCollectWarnings].([]map[string]string)
	ctx.Results[keyCollectWarnings] = append(existing, entry)
	ctx.Logger.Warn("[%s] collect warning: %s", stepID, msg)
}

// OS 系列判断

// osFamilyString 将 runner.OSInfo 转换为人类可读的 OS 系列字符串（英文）。
func osFamilyString(osInfo *runner.OSInfo) string {
	if osInfo == nil {
		return "unknown"
	}
	switch {
	case osInfo.IsRHEL7:
		return "rhel7"
	case osInfo.IsRHEL8:
		return "rhel8"
	case osInfo.IsKylin:
		return "kylin"
	case osInfo.IsUOS:
		return "uos"
	default:
		return "other"
	}
}

// db 环境发现辅助

// getCollectOSUser 从 ctx.Params 读取产品用户名（collect 子命令注入）。
func getCollectOSUser(ctx *runner.StepContext) string {
	return ctx.GetParamString("os_user", "yashan")
}

// getCollectEnvFile 返回 R-004 发现并写入 Results 的 env_file 路径。
// 若 R-004 未运行，则返回空字符串。
func getCollectEnvFile(ctx *runner.StepContext) string {
	v, _ := ctx.Results["env_file"].(string)
	return v
}

// getCollectClusterName 返回 R-004 发现的 cluster name。
func getCollectClusterName(ctx *runner.StepContext) string {
	v, _ := ctx.Results["cluster_name"].(string)
	return v
}
