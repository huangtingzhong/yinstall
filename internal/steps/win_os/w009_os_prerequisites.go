package win_os

import (
	"strings"

	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

func StepW009OSPrerequisites() *runner.Step {
	return &runner.Step{
		ID:          "W-009",
		Name:        "OS Prerequisites",
		Description: ".NET, PowerShell, disk space, product-specific checks",
		Tags:        []string{"win-os", "win-os-both", "prereq"},
		PreCheck: func(ctx *runner.StepContext) error {
			profile := strings.ToLower(ctx.GetParamString("win_os_profile", "mssql"))
			switch profile {
			case "mysql":
				return commonwin.CheckMySQLPrerequisites(ctx)
			case "mssql", "":
				return commonwin.CheckMssqlPrerequisites(ctx)
			default:
				return commonwin.CheckBasePrerequisites(ctx)
			}
		},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-009 prerequisites ok")
			if commonwin.TestPendingReboot(ctx) {
				ctx.Logger.Info("Pending reboot detected on %s (reboot manually if required before SQL install)", ctx.Executor.Host())
			}
			return nil
		},
	}
}
