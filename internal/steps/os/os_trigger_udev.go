package os

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// stepTriggerUdev 触发 udev 重新加载/应用规则（YAC）
func stepTriggerUdev() *runner.Step {
	return &runner.Step{
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
			osLogPhase(ctx, "plan", "B-030: Trigger Udev Rules")
			changed, _ := ctx.Results["yac_udev_changed"].(bool)
			if !ctx.IsForceStep() && !changed {
				ctx.Logger.Info("Udev rules unchanged, skipping reload/trigger (use -f %s to force)", ctx.CurrentStepID)
				osLogPhase(ctx, "skip", "already_configured=udev_trigger")
				return nil
			}
			ctx.Execute("udevadm control --reload-rules", true)
			_, err := ctx.ExecuteWithCheck("/sbin/udevadm trigger --type=devices --action=change", true)
			return err
		},
	}
}
