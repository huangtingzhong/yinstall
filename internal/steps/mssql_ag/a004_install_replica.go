package mssql_ag

import (
	"fmt"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
	mssqlsteps "github.com/yinstall/internal/steps/mssql"
)

// StepA004InstallReplica runs MS-* install steps on replica (embedded install).
func StepA004InstallReplica() *runner.Step {
	return &runner.Step{
		ID:          "A-004",
		Name:        "Install Replica SQL",
		Description: "Run MS-* steps on replica after A-003 media planning",
		Tags:        []string{"mssql-ha", "replica", "install"},
		PreCheck: func(ctx *runner.StepContext) error {
			if commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("A-004 runs on replica only")
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
			mshLogPhase(ctx, "plan", fmt.Sprintf("A-004 install replica (%d MS steps)", len(steps)))
			if err := runner.RunEmbeddedSteps(ctx, "A-004", steps); err != nil {
				return err
			}
			return nil
		},
	}
}
