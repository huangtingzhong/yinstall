// debug_log.go - YCM 安装步骤 debug phase 封装
package ycm

import "github.com/yinstall/internal/runner"

func ycmLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, msg)
}
