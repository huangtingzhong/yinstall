package standby

import (
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// runYasbootOnPrimaryWithEnvFile 在主库上以 primaryUser 执行：
//
//	source <envFile> && <cmd>
//
// 选择最安全的“非交互”切换用户策略：
// - 已是 primaryUser：直接执行
// - --sudo=true：sudo -n -iu primaryUser ...
// - 当前用户是 root：su - primaryUser -c ...
func runYasbootOnPrimaryWithEnvFile(ctx *runner.StepContext, primaryUser, envFile, cmd string) (runner.ExecResult, error) {
	return commonos.ExecuteAsUserWithEnvCheck(ctx, primaryUser, envFile, cmd, true)
}

// runYasbootOnPrimaryWithEnvFileNoCheck 与 runYasbootOnPrimaryWithEnvFile 类似，但不校验退出码。
// 仅用于 PostCheck / 展示类命令。
func runYasbootOnPrimaryWithEnvFileNoCheck(ctx *runner.StepContext, primaryUser, envFile, cmd string) (runner.ExecResult, error) {
	return commonos.ExecuteAsUserWithEnv(ctx, primaryUser, envFile, cmd, true)
}
