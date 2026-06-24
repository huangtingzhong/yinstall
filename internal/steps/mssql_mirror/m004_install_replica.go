package mssql_mirror

import (
	"fmt"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
	mssqlsteps "github.com/yinstall/internal/steps/mssql"
)

// StepM004InstallReplica runs MS-* install steps on replica (embedded install).
func StepM004InstallReplica() *runner.Step {
	return &runner.Step{
		ID:          "M-004",
		Name:        "Install Replica SQL",
		Description: "Run MS-* steps on replica after M-003 media planning",
		Tags:        []string{"mssql-ha", "replica", "install"},
		PreCheck: func(ctx *runner.StepContext) error {
			if commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("M-004 runs on replica only")
			}
			stage := haStage(ctx)
			if !commonmssql.HAIncludesSoftwareInstall(stage) {
				return runner.NewStepSkippedError(fmt.Sprintf("ha stage %q skips replica install", stage))
			}
			if skipped, _ := ctx.Results["replica_install_skipped"].(bool); skipped {
				return runner.NewStepSkippedError("replica SQL already matches primary")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			installStage := commonmssql.StageAll
			if stage := haStage(ctx); stage == commonmssql.HAStageSoftware {
				installStage = commonmssql.StageSoftware
			}
			steps := mssqlsteps.GetInstallStepsForStage(installStage)
			if len(steps) == 0 {
				return fmt.Errorf("no MS-* steps for install stage %q", installStage)
			}
			mshLogPhase(ctx, "plan", fmt.Sprintf("M-004 install replica (%d MS steps)", len(steps)))
			if err := runner.RunEmbeddedSteps(ctx, "M-004", steps); err != nil {
				return err
			}
			return nil
		},
	}
}
