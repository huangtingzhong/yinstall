package mysql_standby

import (
	"fmt"

	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
	mysqlsteps "github.com/yinstall/internal/steps/mysql"
)

// StepMR008InstallReplicaInstance runs M-* instance steps on replica (excluding M-010).
func StepMR008InstallReplicaInstance() *runner.Step {
	return &runner.Step{
		ID:          "MR-008",
		Name:        "Install Replica Instance",
		Description: "Run M-* instance steps (excluding M-010) on replica",
		Tags:        []string{"mysql-standby", "replica"},
		PreCheck:    skipUnlessStandbyReplicationStage,
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			standbyLogPhase(ctx, "plan", "MR-008 install replica instance")
			ctx.Params["mysql_stage"] = commonmysql.StageInstance
			ctx.Params["mysql_port"] = replicaPort(ctx)
			steps := mysqlsteps.GetInstanceSteps("M-002", "M-010")
			for _, step := range steps {
				ctx.CurrentStepID = step.ID
				ctx.Logger.Info("MR-008 running embedded %s", step.ID)
				result := runner.RunStep(step, ctx)
				if !result.Success && !result.Skipped {
					return fmt.Errorf("embedded step %s failed: %w", step.ID, result.Error)
				}
			}
			return nil
		},
	}
}
