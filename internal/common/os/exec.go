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
	if ctx.GetParamBool("sudo", false) {
		return fmt.Sprintf("sudo -n -iu %s bash -lc %s", targetUser, ShellSingleQuote(command)), nil
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
	return ctx.Execute(cmd, showOutput)
}

// ExecuteAsUserWithCheck 以指定用户身份执行命令（带退出码检查）。
// 若命令退出码非 0，返回错误。
//
// 参数含义同 ExecuteAsUser。
func ExecuteAsUserWithCheck(ctx *runner.StepContext, targetUser string, command string, showOutput bool) (runner.ExecResult, error) {
	result, err := ExecuteAsUser(ctx, targetUser, command, showOutput)
	if err != nil {
		return result, err
	}
	if result.GetExitCode() != 0 {
		return result, fmt.Errorf("command failed with exit code %d: %s", result.GetExitCode(), result.GetStderr())
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
	return ctx.Execute(cmd, showOutput)
}

// ExecuteAsUserWithEnvCheck 以指定用户身份执行命令（先加载环境文件 + 检查退出码）。
// 若命令退出码非 0，返回错误。
//
// 参数含义同 ExecuteAsUserWithEnv。
func ExecuteAsUserWithEnvCheck(ctx *runner.StepContext, targetUser string, envFile string, command string, showOutput bool) (runner.ExecResult, error) {
	result, err := ExecuteAsUserWithEnv(ctx, targetUser, envFile, command, showOutput)
	if err != nil {
		return result, err
	}
	if result.GetExitCode() != 0 {
		return result, fmt.Errorf("command failed with exit code %d: %s", result.GetExitCode(), result.GetStderr())
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
