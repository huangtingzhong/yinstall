package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepB007SetTimezone Set timezone
func StepB007SetTimezone() *runner.Step {
	return &runner.Step{
		ID:          "B-007",
		Name:        "Set Timezone",
		Description: "Configure system timezone",
		Tags:        []string{"os", "time"},
		Optional:    false,

		Action: func(ctx *runner.StepContext) error {
			timezone := ctx.GetParamString("os_timezone", "Asia/Shanghai")
			cmd := fmt.Sprintf("timedatectl set-timezone '%s'", timezone)
			_, err := ctx.ExecuteWithCheck(cmd, true)
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			timezone := ctx.GetParamString("os_timezone", "Asia/Shanghai")
			result, _ := ctx.Execute("timedatectl show --property=Timezone --value 2>/dev/null || timedatectl | grep 'Time zone'", false)
			if !strings.Contains(result.GetStdout(), timezone) {
				return fmt.Errorf("timezone not set correctly, expected %s", timezone)
			}
			return nil
		},
	}
}
