package mysql_standby

import (
	"fmt"

	commonmysql "github.com/yinstall/internal/common/mysql"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepCleanupFailedReplica resets replica on failure when enabled.
func stepCleanupFailedReplica() *runner.Step {
	return &runner.Step{
		Name:        "Cleanup Failed Replica",
		Description: "RESET REPLICA ALL on failure",
		Tags:        []string{"mysql-standby", "replica"},
		Optional:    true,
		Dangerous:   true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("standby_cleanup_on_failure", false) {
				return fmt.Errorf("skipped: standby_cleanup_on_failure false")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			layout, err := replicaLayout(ctx)
			if err != nil {
				return err
			}
			ver := ctx.GetParamString("primary_mysql_version", "8.0.46")
			password := ctx.GetParamString("mysql_root_password", "")
			stop := commonmysql.BuildStopReplica(ver, channelName(ctx))
			reset := commonmysql.BuildResetReplicaAll(ver, channelName(ctx))
			_ = commonsql.ExecuteMysqlSQL(ctx, layout, password, stop)
			return commonsql.ExecuteMysqlSQL(ctx, layout, password, reset)
		},
	}
}
