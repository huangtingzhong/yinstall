// debug_log.go - YMP 安装步骤 debug phase 封装
package ymp

import "github.com/yinstall/internal/runner"

func ympLogPhase(ctx *runner.StepContext, phase, msg string) {
	ctx.LogPhase(phase, msg)
}
