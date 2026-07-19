package mysql_standby

import (
	"fmt"
	"strings"

	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

// stepCheckPrimaryReplicationReady verifies GTID/binlog and collects primary cnf.
func stepCheckPrimaryReplicationReady() *runner.Step {
	return &runner.Step{
		Name:        "Check Primary Replication Ready",
		Description: "Verify GTID, binlog, server_id; collect primary version and cnf",
		Tags:        []string{"mysql-standby", "primary"},
		PreCheck: func(ctx *runner.StepContext) error {
			if primaryRootPassword(ctx) == "" && !ctx.DryRun && !ctx.Precheck {
				return fmt.Errorf("primary_root_password is required")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "MR-002 primary replication ready")
			if ctx.DryRun || ctx.Precheck {
				ctx.SetResult("primary_mysql_version", "8.0.46")
				return nil
			}
			layout := primaryLayout(ctx)
			if _, err := commonmysql.ResolveMysqlToolBin(ctx, layout, "mysql"); err != nil {
				return fmt.Errorf("mysql client: %w", err)
			}
			ver, err := queryPrimarySQL(ctx, "SELECT VERSION()")
			if err != nil {
				return fmt.Errorf("query VERSION(): %w", err)
			}
			ver = parseSQLScalar(ver)
			ctx.SetResult("primary_mysql_version", ver)
			ctx.Logger.Info("Primary MySQL version: %s", ver)

			sid, err := serverIDFromSQL(ctx)
			if err != nil {
				return fmt.Errorf("query server_id: %w", err)
			}
			ctx.SetResult("primary_server_id", sid)

			for _, q := range []struct {
				key, sql, want string
			}{
				{"gtid_mode", "SELECT @@gtid_mode", "ON"},
				{"log_bin", "SELECT @@log_bin", "1"},
				{"binlog_format", "SELECT @@binlog_format", "ROW"},
			} {
				out, err := queryPrimarySQL(ctx, q.sql)
				if err != nil {
					return err
				}
				val := strings.ToUpper(parseSQLScalar(out))
				if q.key == "gtid_mode" && val != "ON" {
					ctx.Logger.Warn("primary gtid_mode=%s (MR-003 may enable)", val)
				}
				if q.key == "log_bin" && val != "1" && val != "ON" {
					return fmt.Errorf("primary log_bin must be enabled")
				}
				if q.key == "binlog_format" && val != "ROW" {
					ctx.Logger.Warn("primary binlog_format=%s (recommend ROW)", val)
				}
			}

			platform := ctx.GetTargetPlatform()
			cnfPath := cnfPathForLayout(layout, platform)
			content, err := readRemoteFile(ctx, cnfPath)
			if err != nil {
				return fmt.Errorf("read primary cnf %s: %w", cnfPath, err)
			}
			ctx.SetResult("primary_cnf_path", cnfPath)
			ctx.SetResult("primary_cnf_content", content)
			ctx.SetResult("primary_platform", platform)
			ctx.SetResult("primary_layout", layout)
			return nil
		},
	}
}
