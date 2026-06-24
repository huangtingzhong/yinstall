package mysql_standby

import (
	"fmt"
	"strings"

	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
)

func printMysqlStandbySummary(ctx *runner.StepContext, replicaPort int, fields map[string]string) {
	if ctx == nil || ctx.Logger == nil {
		return
	}
	host := commonmysql.TargetHost(ctx)
	primaryHostVal := primaryHost(ctx)
	primaryPortVal := primaryPort(ctx)

	io := replicaStatusField(fields, "Replica_IO_Running", "Slave_IO_Running")
	sqlRun := replicaStatusField(fields, "Replica_SQL_Running", "Slave_SQL_Running")
	lag := replicaStatusField(fields, "Seconds_Behind_Source", "Seconds_Behind_Master")
	ioState := replicaStatusField(fields, "Replica_IO_State", "Slave_IO_State")
	lastErr := replicaStatusField(fields, "Last_Error", "Last_SQL_Error")
	sourceHost := replicaStatusField(fields, "Source_Host", "Master_Host")
	sourcePort := replicaStatusField(fields, "Source_Port", "Master_Port")

	notice := func(msg string) {
		ctx.Logger.ConsoleNotice("MR-015", msg)
	}

	notice(fmt.Sprintf("======== MySQL Replication Summary (replica %s) ========", host))
	notice("[Primary]")
	notice(fmt.Sprintf("  host=%s  port=%d", primaryHostVal, primaryPortVal))
	notice("  login=root")
	notice(fmt.Sprintf("  password=%s", displayPrimaryRootPassword(ctx)))
	notice(fmt.Sprintf("  connect_example=mysql -h %s -P %d -uroot -p", primaryHostVal, primaryPortVal))

	notice("[Replica]")
	notice(fmt.Sprintf("  host=%s  port=%d", host, replicaPort))
	notice("  login=root")
	notice(fmt.Sprintf("  password=%s", commonmysql.DisplayRootPassword(ctx)))
	notice(fmt.Sprintf("  connect_example=mysql -h %s -P %d -uroot -p", host, replicaPort))

	if layout, err := replicaLayout(ctx); err == nil {
		notice("[Replica Paths]")
		if layout.Home != "" {
			notice(fmt.Sprintf("  mysql_home=%s", layout.Home))
		}
		if layout.Data != "" {
			notice(fmt.Sprintf("  mysql_data=%s", layout.Data))
		}
		if layout.Other != "" {
			notice(fmt.Sprintf("  mysql_other=%s", layout.Other))
			notice(fmt.Sprintf("  config=%s", cnfPathForLayout(layout, ctx.GetTargetPlatform())))
		}
	}

	notice("[Replication]")
	notice(fmt.Sprintf("  sync_method=%s", syncMethod(ctx)))
	notice(fmt.Sprintf("  user=%s  password=%s", repUser(ctx), commonmysql.DisplayReplicationPassword(ctx)))
	notice(fmt.Sprintf("  channel=%s", emptyAs(channelName(ctx), "(default)")))
	if sourceHost != "" || sourcePort != "" {
		notice(fmt.Sprintf("  source=%s:%s", emptyAs(sourceHost, primaryHostVal), emptyAs(sourcePort, fmt.Sprintf("%d", primaryPortVal))))
	}
	notice(fmt.Sprintf("  Replica_IO_Running=%s", emptyAs(io, "Unknown")))
	notice(fmt.Sprintf("  Replica_SQL_Running=%s", emptyAs(sqlRun, "Unknown")))
	notice(fmt.Sprintf("  Seconds_Behind_Source=%s", emptyAs(lag, "NULL")))
	if ioState != "" {
		notice(fmt.Sprintf("  Replica_IO_State=%s", ioState))
	}
	if lastErr != "" {
		notice(fmt.Sprintf("  Last_Error=%s", lastErr))
	}
	if ctx.GetParamBool("enable_semi_sync", false) {
		notice("  semi_sync=enabled")
	}
	notice("======== end replication summary ========")
}

func displayPrimaryRootPassword(ctx *runner.StepContext) string {
	pwd := strings.TrimSpace(ctx.GetParamString("primary_root_password", ""))
	if pwd == "" {
		return "(not configured)"
	}
	if logging.RedactSensitive() {
		return "***REDACTED***"
	}
	return pwd
}
