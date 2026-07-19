package mysql_standby

import (
	"github.com/yinstall/internal/runner"
)

func standbyLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, runner.StepMsg(ctx, msg))
}
