package mssql_mirror

import (
	"github.com/yinstall/internal/runner"
)

func mshLogPhase(ctx *runner.StepContext, phase, detail string) {
	if ctx != nil {
		ctx.LogPhase(phase, detail)
	}
}
