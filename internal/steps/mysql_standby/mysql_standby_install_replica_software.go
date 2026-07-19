package mysql_standby

import (
	"fmt"

	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
	mysqlsteps "github.com/yinstall/internal/steps/mysql"
)

// stepInstallReplicaSoftware runs M-* software steps on replica after MR-007 planning.
func stepInstallReplicaSoftware() *runner.Step {
	return &runner.Step{
		Name:        "Install Replica Software",
		Description: "Run M-* software steps on replica (--stage software or all)",
		Tags:        []string{"mysql-standby", "replica", "mysql-software"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			stage := standbyStage(ctx)
			if !commonmysql.StandbyIncludesSoftwareInstall(stage) {
				return runner.NewStepSkippedError(fmt.Sprintf("standby stage %q does not install software", stage))
			}
			if skipped, _ := ctx.Results["replica_install_skipped"].(bool); skipped {
				return runner.NewStepSkippedError("replica software already installed at primary version")
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
			return runner.RunEmbeddedSteps(ctx, "MR-018", steps)
		},
	}
}
