package mysql_standby

import (
	commonmysql "github.com/yinstall/internal/common/mysql"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepConfigureReplicationSource runs CHANGE REPLICATION SOURCE on replica.
func stepConfigureReplicationSource() *runner.Step {
	return &runner.Step{
		Name:        "Configure Replication Source",
		Description: "CHANGE REPLICATION SOURCE on replica",
		Tags:        []string{"mysql-standby", "replica"},
		PreCheck:    skipUnlessStandbyReplicationStage,
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			standbyLogPhase(ctx, "plan", "MR-013 configure replication source")
			layout, err := replicaLayout(ctx)
			if err != nil {
				return err
			}
			ver, _ := ctx.Results["primary_mysql_version"].(string)
			if ver == "" {
				ver = ctx.GetParamString("primary_mysql_version", "8.0.46")
			}
			opts := commonmysql.ReplicationOpts{
				PrimaryHost:  primaryHost(ctx),
				PrimaryPort:  primaryPort(ctx),
				RepUser:      repUser(ctx),
				RepPassword:  repPassword(ctx),
				Channel:      channelName(ctx),
				Version:      ver,
				GetPublicKey: true,
			}
			sql := commonmysql.BuildChangeReplicationSource(opts)
			ctx.LogScriptPreview("sql", "replication", sql)
			password := ctx.GetParamString("mysql_root_password", "")
			if err := commonsql.ExecuteMysqlSQL(ctx, layout, password, sql); err != nil {
				return err
			}
			filter := buildReplicationFilterSQL(ctx)
			if filter != "" {
				return commonsql.ExecuteMysqlSQL(ctx, layout, password, filter)
			}
			return nil
		},
	}
}

func buildReplicationFilterSQL(ctx *runner.StepContext) string {
	do := splitCSV(ctx.GetParamString("replicate_do_db", ""))
	ignore := splitCSV(ctx.GetParamString("replicate_ignore_db", ""))
	if len(do) == 0 && len(ignore) == 0 {
		return ""
	}
	return commonmysql.BuildReplicationFilter(do, ignore)
}
