// exec.go - 命令执行公共函数
// 提供智能用户切换的命令执行逻辑

package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// ExecuteAsUser 以指定用户身份执行命令
// 自动判断当前用户，如果已经是目标用户则直接执行，否则使用 su 切换
//
// 参数：
//   - executor: 命令执行器
//   - targetUser: 目标用户名
//   - command: 要执行的命令
//   - showOutput: 是否显示输出
//
// 返回：
//   - 执行结果和错误
func ExecuteAsUser(ctx *runner.StepContext, targetUser string, command string, showOutput bool) (runner.ExecResult, error) {
	result, _ := ctx.Execute("whoami", false)
	currentUser := strings.TrimSpace(result.GetStdout())

	var cmd string
	if currentUser == targetUser {
		cmd = command
	} else {
		escapedCmd := strings.ReplaceAll(command, "'", "'\"'\"'")
		cmd = fmt.Sprintf("su - %s -c '%s'", targetUser, escapedCmd)
	}

	return ctx.Execute(cmd, showOutput)
}

// ExecuteAsUserWithCheck 以指定用户身份执行命令（带错误检查）
// 自动判断当前用户，如果已经是目标用户则直接执行，否则使用 su 切换
// 如果命令执行失败（退出码非0），返回错误
//
// 参数：
//   - executor: 命令执行器
//   - targetUser: 目标用户名
//   - command: 要执行的命令
//   - showOutput: 是否显示输出
//
// 返回：
//   - 执行结果和错误
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

// ExecuteAsUserWithEnv 以指定用户身份执行命令（带环境变量加载）
// 自动判断当前用户，如果已经是目标用户则直接执行，否则使用 su 切换
// 执行前会先 source 指定的环境变量文件
//
// 参数：
//   - executor: 命令执行器
//   - targetUser: 目标用户名
//   - envFile: 环境变量文件路径（如 /home/yashan/.bashrc）
//   - command: 要执行的命令
//   - showOutput: 是否显示输出
//
// 返回：
//   - 执行结果和错误
func ExecuteAsUserWithEnv(ctx *runner.StepContext, targetUser string, envFile string, command string, showOutput bool) (runner.ExecResult, error) {
	result, _ := ctx.Execute("whoami", false)
	currentUser := strings.TrimSpace(result.GetStdout())

	var cmd string
	if currentUser == targetUser {
		cmd = fmt.Sprintf("source %s && %s", envFile, command)
	} else {
		fullCmd := fmt.Sprintf("source %s && %s", envFile, command)
		escapedCmd := strings.ReplaceAll(fullCmd, "'", "'\"'\"'")
		cmd = fmt.Sprintf("su - %s -c '%s'", targetUser, escapedCmd)
	}

	return ctx.Execute(cmd, showOutput)
}

// ExecuteAsUserWithEnvCheck 以指定用户身份执行命令（带环境变量加载和错误检查）
// 自动判断当前用户，如果已经是目标用户则直接执行，否则使用 su 切换
// 执行前会先 source 指定的环境变量文件
// 如果命令执行失败（退出码非0），返回错误
//
// 参数：
//   - executor: 命令执行器
//   - targetUser: 目标用户名
//   - envFile: 环境变量文件路径（如 /home/yashan/.bashrc）
//   - command: 要执行的命令
//   - showOutput: 是否显示输出
//
// 返回：
//   - 执行结果和错误
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
