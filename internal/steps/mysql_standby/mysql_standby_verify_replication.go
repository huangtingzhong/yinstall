package mysql_standby

import (
	"fmt"
	"strings"

	commonmysql "github.com/yinstall/internal/common/mysql"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepVerifyReplication verifies replication threads are running.
func stepVerifyReplication() *runner.Step {
	return &runner.Step{
		Name:        "Verify Replication",
		Description: "SHOW REPLICA STATUS summary",
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
			sql := "SHOW REPLICA STATUS\\G"
			if !commonmysql.UsesReplicationSourceSyntax(ver) {
				sql = "SHOW SLAVE STATUS\\G"
			}
			out, err := commonsql.QueryMysqlSQL(ctx, layout, ctx.GetParamString("mysql_root_password", ""), sql)
			if err != nil {
				return err
			}
			ctx.Logger.Info("Replication status:\n%s", out)

			fields := parseReplicaStatusVertical(out)
			if len(fields) == 0 {
				return fmt.Errorf("empty replication status output")
			}

			io := replicaStatusField(fields, "Replica_IO_Running", "Slave_IO_Running")
			sqlRun := replicaStatusField(fields, "Replica_SQL_Running", "Slave_SQL_Running")
			if !replicaThreadRunning(io) || !replicaThreadRunning(sqlRun) {
				printMysqlStandbySummary(ctx, layout.Port, fields)
				return fmt.Errorf("replication not running (IO=%q SQL=%q)", io, sqlRun)
			}

			printMysqlStandbySummary(ctx, layout.Port, fields)
			return nil
		},
	}
}

func parseReplicaStatusVertical(out string) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if key != "" {
			fields[key] = val
		}
	}
	return fields
}

func replicaStatusField(fields map[string]string, keys ...string) string {
	for _, key := range keys {
		if val, ok := fields[key]; ok {
			return val
		}
	}
	return ""
}

func replicaThreadRunning(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "YES", "ON", "1":
		return true
	default:
		return false
	}
}

func emptyAs(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}
