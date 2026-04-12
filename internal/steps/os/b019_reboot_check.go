package os

import (
	"github.com/yinstall/internal/runner"
)

// StepB019RebootCheck Reboot check (optional)
func StepB019RebootCheck() *runner.Step {
	return &runner.Step{
		ID:          "B-019",
		Name:        "Reboot Check",
		Description: "Check if reboot is required for changes to take effect",
		Tags:        []string{"os", "reboot"},
		Optional:    true,

		Action: func(ctx *runner.StepContext) error {
			needsReboot := ctx.GetParamBool("needs_reboot", false)
			if needsReboot {
				ctx.Logger.Info("NOTICE: System reboot is required for some changes to take effect")
				ctx.Logger.Info("Please reboot the system manually and run verification")
			}
			return nil
		},
	}
}
