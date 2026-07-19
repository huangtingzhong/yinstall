package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepSetTimezone 设置系统时区
func stepSetTimezone() *runner.Step {
	return &runner.Step{
		Name:        "Set Timezone",
		Description: "Configure system timezone",
		Tags:        []string{"os", "time"},
		Optional:    false,

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-007: Set Timezone")
			timezone := commonos.ResolveOSTimezone(ctx.GetParamString("os_timezone", ""))
			if !ctx.IsForceStep() {
				cur, _ := ctx.Execute("timedatectl show --property=Timezone --value 2>/dev/null || true", false)
				if cur != nil && strings.TrimSpace(cur.GetStdout()) == timezone {
					ctx.Logger.Info("Timezone already %s, skipping (use -f %s to force)", timezone, ctx.CurrentStepID)
					osLogPhase(ctx, "skip", "already_configured=timezone")
					return nil
				}
			}
			cmd := fmt.Sprintf("timedatectl set-timezone '%s'", timezone)
			_, err := ctx.ExecuteWithCheck(cmd, true)
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			timezone := commonos.ResolveOSTimezone(ctx.GetParamString("os_timezone", ""))
			result, _ := ctx.Execute("timedatectl show --property=Timezone --value 2>/dev/null || timedatectl | grep 'Time zone'", false)
			if !strings.Contains(result.GetStdout(), timezone) {
				return fmt.Errorf("timezone not set correctly, expected %s", timezone)
			}
			return nil
		},
	}
}
