package mssql

import "github.com/yinstall/internal/runner"

func mssqlLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, msg)
}
