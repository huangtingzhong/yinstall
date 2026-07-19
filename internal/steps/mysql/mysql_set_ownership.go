package mysql

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepSetOwnership chowns mysql base tree to product user (Linux only).
func stepSetOwnership() *runner.Step {
	return &runner.Step{
		Name:        "Set Ownership",
		Description: "chown mysql:mysql on MYSQL_BASE tree",
		Tags:        []string{"mysql", "ownership", "mysql-software"},
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetTargetPlatform() != PlatformLinux {
				return nil
			}
			_, err := layoutFromCtx(ctx)
			if err != nil {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, err)
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.GetTargetPlatform() != PlatformLinux {
				return nil
			}
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return err
			}
			user := ctx.GetParamString("os_user", "mysql")
			group := ctx.GetParamString("os_group", "mysql")
			mysqlLogPhase(ctx, "plan", fmt.Sprintf("M-006 chown %s:%s %s", user, group, layout.Base))
			cmd := fmt.Sprintf("chown -R %s:%s %s", user, group, commonos.ShellSingleQuote(layout.Base))
			_, err = ctx.ExecuteWithCheck(cmd, true)
			return err
		},
	}
}
