package mysql_standby

import (
	commonmysql "github.com/yinstall/internal/common/mysql"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepStartReplica runs START REPLICA on replica.
func stepStartReplica() *runner.Step {
	return &runner.Step{
		Name:        "Start Replica",
		Description: "START REPLICA on replica",
		Tags:        []string{"mysql-standby", "replica"},
		PreCheck:    skipUnlessStandbyReplicationStage,
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			layout, err := replicaLayout(ctx)
			if err != nil {
				return err
			}
			ver := ctx.GetParamString("primary_mysql_version", "8.0.46")
			sql := commonmysql.BuildStartReplica(ver, channelName(ctx))
			return commonsql.ExecuteMysqlSQL(ctx, layout, ctx.GetParamString("mysql_root_password", ""), sql)
		},
	}
}
