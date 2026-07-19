package mssql_ag

import (
	"github.com/yinstall/internal/runner"
)

func mshLogPhase(ctx *runner.StepContext, phase, detail string) {
	if ctx != nil {
		ctx.LogPhase(phase, runner.StepMsg(ctx, detail))
	}
}
