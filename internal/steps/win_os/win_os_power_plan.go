package win_os

import (
	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

func stepPowerPlan() *runner.Step {
	return &runner.Step{
		Name:        "Power Plan",
		Description: "Set high performance power plan",
		Tags:        []string{"win-os", "win-os-both", "power"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			profile := commonwin.ProfileMssql()
			if p, ok := ctx.Params["win_os_profile_struct"].(commonwin.Profile); ok {
				profile = p
			}
			if !commonwin.ShouldApplyPowerPlan(ctx, profile) {
				return runner.NewStepSkippedError("power plan skip")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-013 power plan")
			guid, changed, err := commonwin.ApplyPowerPlan(ctx)
			if err != nil {
				return err
			}
			ctx.SetResult("os_power_plan_guid", guid)
			ctx.SetResult("os_power_plan_changed", changed)
			return nil
		},
	}
}
