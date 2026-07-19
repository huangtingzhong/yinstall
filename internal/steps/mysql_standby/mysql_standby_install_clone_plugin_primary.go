package mysql_standby

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// stepInstallClonePluginPrimary installs clone plugin on primary.
func stepInstallClonePluginPrimary() *runner.Step {
	return &runner.Step{
		Name:        "Install Clone Plugin (Primary)",
		Description: "Install clone plugin on primary when sync-method=clone",
		Tags:        []string{"mysql-standby", "primary"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if err := skipUnlessStandbyReplicationStage(ctx); err != nil {
				return err
			}
			if syncMethod(ctx) != "clone" {
				return fmt.Errorf("skipped: sync_method is not clone")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			standbyLogPhase(ctx, "plan", "MR-005 install clone plugin on primary")
			layout := primaryLayout(ctx)
			return ensureClonePlugin(ctx, layout, primaryRootPassword(ctx))
		},
	}
}
