// debug_log.go - Standby 扩容步骤 debug phase 封装
package standby

import "github.com/yinstall/internal/runner"

func standbyLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, runner.StepMsg(ctx, msg))
}
