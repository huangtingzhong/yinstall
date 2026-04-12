package os

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepB030TriggerUdev Trigger udev rules (YAC)
func StepB030TriggerUdev() *runner.Step {
	return &runner.Step{
		ID:          "B-030",
		Name:        "Trigger Udev Rules",
		Description: "Apply udev rules",
		Tags:        []string{"os", "yac", "udev"},
		Optional:    true, // 单机环境下不需要多路径/udev，可以跳过

		PreCheck: func(ctx *runner.StepContext) error {
			// YAC 模式下需要触发 udev 规则
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if isYACMode {
				return nil
			}

			// 非 YAC 模式：检查是否显式启用
			enabled := ctx.GetParamBool("yac_multipath_enable", false)
			needMultipath := ctx.GetParamBool("yac_need_multipath", false)

			if !enabled && !needMultipath {
				return fmt.Errorf("multipath/udev not enabled and not required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ctx.Execute("udevadm control --reload-rules", true)
			_, err := ctx.ExecuteWithCheck("/sbin/udevadm trigger --type=devices --action=change", true)
			return err
		},
	}
}
