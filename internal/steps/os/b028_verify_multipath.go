package os

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepB028VerifyMultipath Verify multipath devices (YAC)
func StepB028VerifyMultipath() *runner.Step {
	return &runner.Step{
		ID:          "B-028",
		Name:        "Verify Multipath",
		Description: "Verify multipath device configuration",
		Tags:        []string{"os", "yac", "multipath"},
		Optional:    true, // 单机环境下不需要多路径，可以跳过

		PreCheck: func(ctx *runner.StepContext) error {
			// YAC 模式下需要验证 multipath
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if isYACMode {
				return nil
			}

			// 非 YAC 模式：检查是否显式启用
			enabled := ctx.GetParamBool("yac_multipath_enable", false)
			needMultipath := ctx.GetParamBool("yac_need_multipath", false)

			if !enabled && !needMultipath {
				return fmt.Errorf("multipath not enabled and not required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			result, err := ctx.ExecuteWithCheck("multipath -ll", true)
			if err != nil {
				return err
			}
			ctx.SetResult("multipath_status", result.GetStdout())
			if result.GetStdout() != "" {
				ctx.Logger.Info("Multipath devices:\n%s", result.GetStdout())
			} else {
				ctx.Logger.Info("No multipath devices configured")
			}

			return nil
		},
	}
}
