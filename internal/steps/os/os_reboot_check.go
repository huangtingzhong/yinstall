package os

import (
	"github.com/yinstall/internal/runner"
)

// stepRebootCheck 重启相关检查（可选）
func stepRebootCheck() *runner.Step {
	return &runner.Step{
		Name:        "Reboot Check",
		Description: "Check if reboot is required for changes to take effect",
		Tags:        []string{"os", "reboot"},
		Optional:    true,

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-019: Reboot Check")
			needsReboot := ctx.GetParamBool("needs_reboot", false)
			if needsReboot {
				ctx.Logger.Info("NOTICE: System reboot is required for some changes to take effect")
				ctx.Logger.Info("Please reboot the system manually and run verification")
			}
			return nil
		},
	}
}
