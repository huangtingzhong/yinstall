package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepB009ApplySysctl Apply sysctl config
func StepB009ApplySysctl() *runner.Step {
	return &runner.Step{
		ID:          "B-009",
		Name:        "Apply Sysctl Config",
		Description: "Apply kernel parameters",
		Tags:        []string{"os", "kernel"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			configFile := ctx.GetParamString("os_sysctl_file", "/etc/sysctl.d/yashandb.conf")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", configFile), false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("sysctl config file not found")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			_, err := ctx.ExecuteWithCheck("sysctl --system", true)
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("sysctl vm.swappiness", false)
			if !strings.Contains(result.GetStdout(), "0") {
				return fmt.Errorf("sysctl parameters not applied correctly")
			}
			return nil
		},
	}
}
