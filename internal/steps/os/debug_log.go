// debug_log.go - OS 安装步骤 debug phase 封装（委托 runner.StepContext.LogPhase）
package os

import "github.com/yinstall/internal/runner"

func osLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, msg)
}
