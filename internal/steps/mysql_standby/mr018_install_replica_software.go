package mysql_standby

import (
	"fmt"

	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
	mysqlsteps "github.com/yinstall/internal/steps/mysql"
)

// StepMR018InstallReplicaSoftware runs M-* software steps on replica after MR-007 planning.
func StepMR018InstallReplicaSoftware() *runner.Step {
	return &runner.Step{
		ID:          "MR-018",
		Name:        "Install Replica Software",
		Description: "Run M-* software steps on replica (--stage software or all)",
		Tags:        []string{"mysql-standby", "replica", "mysql-software"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			stage := standbyStage(ctx)
			if !commonmysql.StandbyIncludesSoftwareInstall(stage) {
				return runner.NewStepSkippedError(fmt.Sprintf("standby stage %q does not install software", stage))
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			standbyLogPhase(ctx, "plan", "MR-018 install replica software")
			ctx.Params["mysql_stage"] = commonmysql.StageSoftware
			steps := mysqlsteps.GetSoftwareSteps()
			for _, step := range steps {
				ctx.CurrentStepID = step.ID
				ctx.Logger.Info("MR-018 running embedded %s", step.ID)
				result := runner.RunStep(step, ctx)
				if !result.Success && !result.Skipped {
					return fmt.Errorf("embedded step %s failed: %w", step.ID, result.Error)
				}
			}
			return nil
		},
	}
}
