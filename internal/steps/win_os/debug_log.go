package win_os

import "github.com/yinstall/internal/runner"

func winOSLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, msg)
}
