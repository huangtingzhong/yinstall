package mysql_standby

import (
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
	mysqlsteps "github.com/yinstall/internal/steps/mysql"
)

// stepInstallReplicaInstance runs M-* instance steps on replica (excluding M-010).
func stepInstallReplicaInstance() *runner.Step {
	return &runner.Step{
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
			return runner.RunEmbeddedSteps(ctx, "MR-008", steps)
		},
	}
}
