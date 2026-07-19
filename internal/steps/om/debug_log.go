// debug_log.go - OM 域 debug phase 封装
package om

import "github.com/yinstall/internal/runner"

func omLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, runner.StepMsg(ctx, msg))
}
