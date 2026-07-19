// 命令执行公共函数（用户切换封装）
// 提供智能用户切换的命令执行逻辑

package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func buildRunAsUserCommand(ctx *runner.StepContext, targetUser string, command string) (string, error) {
	currentUser, err := GetCurrentUser(ctx)
	if err != nil {
		return "", err
	}
	currentUser = strings.TrimSpace(currentUser)
	targetUser = strings.TrimSpace(targetUser)
	if targetUser == "" {
		return "", fmt.Errorf("target user is empty")
	}

	// 规则 1：已经是目标用户，直接执行。
	if currentUser == targetUser {
		return command, nil
	}

	// 规则 2：可用 sudo，则使用 sudo 切换到目标用户执行。
	// 注意：使用 `sudo -n`，避免因需要输入密码而卡住。
	// 勿用 `sudo -i`：-i 会把命令再交给登录 shell 的 -c，导致命令行中的 $$ 被展开为 PID
	//（默认 os 密码 aaBB11@@33$$ 经 C-014 yasboot -p 时 SSH 认证失败）。
	// 登录环境由 bash -lc 的 -l 提供；显式 cd ~ 对齐旧 sudo -i 的起始目录（否则 cwd 常留在 /root）。
	if ctx.GetParamBool("sudo", false) {
		return fmt.Sprintf("sudo -n -u %s bash -lc %s", targetUser, ShellSingleQuote("cd ~ && "+command)), nil
	}

	// 规则 3：当前用户是 root，则使用 su 切换到目标用户执行。
	if currentUser == "root" {
		return fmt.Sprintf("su - %s -c %s", targetUser, ShellSingleQuote(command)), nil
	}

	// 规则 4：无法进行非交互切换用户，给出明确指引。
	return "", fmt.Errorf(
		"cannot switch from user %q to %q non-interactively; please login as %q, or enable passwordless sudo and set --sudo=true",
		currentUser, targetUser, targetUser,
	)
}

// ExecuteAsUser 以指定用户身份执行命令。
// 自动判断当前用户：若已是目标用户则直接执行；否则按“sudo/root/su”策略切换后执行。
//
// 参数：
//   - targetUser：目标用户名
//   - command：要执行的命令（shell 命令字符串）
//   - showOutput：是否在终端展示输出
//
// 返回：
//   - 命令执行结果与错误
func ExecuteAsUser(ctx *runner.StepContext, targetUser string, command string, showOutput bool) (runner.ExecResult, error) {
	cmd, err := buildRunAsUserCommand(ctx, targetUser, command)
	if err != nil {
		return nil, err
	}
	_ = showOutput // reserved; do not pass as ctx.Execute(sudo) — user switch is already in cmd
	return ctx.Execute(cmd, false)
}

func commandFailureMessage(result runner.ExecResult) string {
	errMsg := result.GetStderr()
	if errMsg == "" {
		errMsg = result.GetStdout()
	}
	if errMsg == "" {
		errMsg = fmt.Sprintf("exit code %d", result.GetExitCode())
	}
	return strings.TrimSpace(errMsg)
}

// reportCommandFailure 与 runner.StepContext.ExecuteWithCheck 一致。
// logToTerminal=false 时不写 LogErrorExit（用于可恢复重试前的探测）。
func reportCommandFailure(ctx *runner.StepContext, cmd string, result runner.ExecResult, logToTerminal bool) error {
	errMsg := commandFailureMessage(result)
	if logToTerminal {
		ctx.Logger.LogErrorExit(
			ctx.Executor.Host(),
			ctx.CurrentStepID,
			"",
			cmd,
			result.GetStdout(),
			result.GetStderr(),
			result.GetExitCode(),
			errMsg,
		)
	}
	return runner.NewCommandExitError(result.GetExitCode(), errMsg, logToTerminal)
}

// LogTerminalCommandFailure 将已有 ExecResult 按 LogErrorExit 格式输出到终端（不返回 error、不重复执行命令）。
func LogTerminalCommandFailure(ctx *runner.StepContext, cmd string, result runner.ExecResult) {
	if ctx == nil || result == nil || result.GetExitCode() == 0 {
		return
	}
	errMsg := commandFailureMessage(result)
	ctx.Logger.LogErrorExit(
		ctx.Executor.Host(),
		ctx.CurrentStepID,
		"",
		cmd,
		result.GetStdout(),
		result.GetStderr(),
		result.GetExitCode(),
		errMsg,
	)
}

// ExecuteAsUserWithCheck 以指定用户身份执行命令（带退出码检查）。
// 若命令退出码非 0，返回错误。
//
// 参数含义同 ExecuteAsUser。
func ExecuteAsUserWithCheck(ctx *runner.StepContext, targetUser string, command string, showOutput bool) (runner.ExecResult, error) {
	cmd, err := buildRunAsUserCommand(ctx, targetUser, command)
	if err != nil {
		return nil, err
	}
	_ = showOutput // reserved; privilege switching is handled in cmd, not ctx.Execute(sudo)
	result, err := ctx.Execute(cmd, false)
	if err != nil {
		return result, err
	}
	if result != nil && result.GetExitCode() != 0 {
		return result, reportCommandFailure(ctx, cmd, result, true)
	}
	return result, nil
}

// ExecuteAsUserWithCheckQuiet 同 ExecuteAsUserWithCheck，但失败时不输出 LogErrorExit（供重试逻辑使用）。
func ExecuteAsUserWithCheckQuiet(ctx *runner.StepContext, targetUser string, command string, showOutput bool) (runner.ExecResult, error) {
	cmd, err := buildRunAsUserCommand(ctx, targetUser, command)
	if err != nil {
		return nil, err
	}
	_ = showOutput
	result, err := ctx.Execute(cmd, false)
	if err != nil {
		return result, err
	}
	if result != nil && result.GetExitCode() != 0 {
		return result, reportCommandFailure(ctx, cmd, result, false)
	}
	return result, nil
}

// ExecuteAsUserWithEnv 以指定用户身份执行命令（先加载环境文件）。
// 执行前会先 source 指定的环境变量文件，再执行 command。
//
// 参数：
//   - targetUser：目标用户名
//   - envFile：环境变量文件路径（例如 /home/yashan/.bashrc 或 ~/.port1988）
//   - command：要执行的命令
//   - showOutput：是否在终端展示输出
func ExecuteAsUserWithEnv(ctx *runner.StepContext, targetUser string, envFile string, command string, showOutput bool) (runner.ExecResult, error) {
	fullCmd := fmt.Sprintf("source %s && %s", envFile, command)

	cmd, err := buildRunAsUserCommand(ctx, targetUser, fullCmd)
	if err != nil {
		return nil, err
	}
	_ = showOutput
	return ctx.Execute(cmd, false)
}

// ExecuteAsUserWithEnvCheck 以指定用户身份执行命令（先加载环境文件 + 检查退出码）。
// 若命令退出码非 0，返回错误。
//
// 参数含义同 ExecuteAsUserWithEnv。
func ExecuteAsUserWithEnvCheck(ctx *runner.StepContext, targetUser string, envFile string, command string, showOutput bool) (runner.ExecResult, error) {
	fullCmd := fmt.Sprintf("source %s && %s", envFile, command)
	cmd, err := buildRunAsUserCommand(ctx, targetUser, fullCmd)
	if err != nil {
		return nil, err
	}
	_ = showOutput
	result, err := ctx.Execute(cmd, false)
	if err != nil {
		return result, err
	}
	if result != nil && result.GetExitCode() != 0 {
		return result, reportCommandFailure(ctx, cmd, result, true)
	}
	return result, nil
}

// ExecuteAsUserWithEnvCheckQuiet 同 ExecuteAsUserWithEnvCheck，失败时不输出 LogErrorExit。
func ExecuteAsUserWithEnvCheckQuiet(ctx *runner.StepContext, targetUser string, envFile string, command string, showOutput bool) (runner.ExecResult, error) {
	fullCmd := fmt.Sprintf("source %s && %s", envFile, command)
	cmd, err := buildRunAsUserCommand(ctx, targetUser, fullCmd)
	if err != nil {
		return nil, err
	}
	_ = showOutput
	result, err := ctx.Execute(cmd, false)
	if err != nil {
		return result, err
	}
	if result != nil && result.GetExitCode() != 0 {
		return result, reportCommandFailure(ctx, cmd, result, false)
	}
	return result, nil
}

// ExecuteAsUserWithEnvCheckCtx 以指定用户身份执行命令（带环境变量加载、错误检查和日志记录）
// 统一通过 ctx.Execute() 记录 DEBUG 日志
func ExecuteAsUserWithEnvCheckCtx(ctx *runner.StepContext, targetUser string, envFile string, command string, showOutput bool) (runner.ExecResult, error) {
	return ExecuteAsUserWithEnvCheck(ctx, targetUser, envFile, command, showOutput)
}

// ExecuteAsUserWithEnvCtx 以指定用户身份执行命令（带环境变量加载和日志记录）
// 统一通过 ctx.Execute() 记录 DEBUG 日志
func ExecuteAsUserWithEnvCtx(ctx *runner.StepContext, targetUser string, envFile string, command string, showOutput bool) (runner.ExecResult, error) {
	return ExecuteAsUserWithEnv(ctx, targetUser, envFile, command, showOutput)
}

// BuildAsUserEnvCommand 构造以指定用户执行带 env 的命令字符串（不执行）。
// collect 子命令用此函数构造命令后通过 SSH session 超时执行（方案D）。
func BuildAsUserEnvCommand(ctx *runner.StepContext, targetUser, envFile, command string) (string, error) {
	fullCmd := fmt.Sprintf("source %s && %s", envFile, command)
	return buildRunAsUserCommand(ctx, targetUser, fullCmd)
}

// GetCurrentUser 获取当前执行用户
func GetCurrentUser(ctx *runner.StepContext) (string, error) {
	result, err := ctx.Execute("whoami", false)
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	if result.GetExitCode() != 0 {
		return "", fmt.Errorf("failed to get current user: exit code %d", result.GetExitCode())
	}
	return strings.TrimSpace(result.GetStdout()), nil
}
