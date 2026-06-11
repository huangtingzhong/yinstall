package mysql_standby

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepMR010InstallClonePluginReplica installs clone plugin on replica.
func StepMR010InstallClonePluginReplica() *runner.Step {
	return &runner.Step{
		ID:          "MR-010",
		Name:        "Install Clone Plugin (Replica)",
		Description: "Install clone plugin on replica",
		Tags:        []string{"mysql-standby", "replica"},
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
			standbyLogPhase(ctx, "plan", "MR-010 install clone plugin on replica")
			layout, err := replicaLayout(ctx)
			if err != nil {
				return err
			}
			return ensureClonePlugin(ctx, layout, ctx.GetParamString("mysql_root_password", ""))
		},
	}
}
