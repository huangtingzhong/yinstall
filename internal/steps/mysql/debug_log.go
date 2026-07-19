package mysql

import (
	"github.com/yinstall/internal/runner"
)

func mysqlLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, runner.StepMsg(ctx, msg))
}
