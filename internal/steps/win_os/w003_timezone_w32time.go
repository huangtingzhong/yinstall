package win_os

import (
	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

func StepW003TimezoneW32Time() *runner.Step {
	return &runner.Step{
		ID:          "W-003",
		Name:        "Timezone & W32Time",
		Description: "Configure timezone and time synchronization",
		Tags:        []string{"win-os", "win-os-both", "time"},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-003 timezone")
			return commonwin.ConfigureTimezoneAndTime(ctx)
		},
		PostCheck: func(ctx *runner.StepContext) error {
			res, err := ctx.Execute(`powershell -NoProfile -Command "(Get-Service w32time).Status"`, false)
			if err != nil {
				return err
			}
			if res != nil && res.GetStdout() != "" {
				ctx.Logger.Info("w32time status: %s", res.GetStdout())
			}
			return nil
		},
	}
}
