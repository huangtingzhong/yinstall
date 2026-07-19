package mssql_ag

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func printAGStatusSummary(ctx *runner.StepContext) error {
	if ctx == nil || ctx.Logger == nil {
		return nil
	}
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	stepID := ctx.CurrentStepID
	ag := commonmssql.AGName(ctx)
	printSummaryNotice(ctx, stepID, fmt.Sprintf("======== MSSQL Always On Status (%s) ========", ag))

	replicaOut, err := commonmssql.QuerySqlcmdScalar(ctx, "AG status replicas", commonmssql.AGStatusReplicaSQL(ag))
	if err != nil {
		return err
	}
	replicas := commonmssql.ParseAGReplicaStatus(replicaOut)
	if len(replicas) == 0 {
		printSummaryNotice(ctx, stepID, "[Replicas] (none)")
	} else {
		printSummaryNotice(ctx, stepID, "[Replicas]")
		for _, r := range replicas {
			printSummaryNotice(ctx, stepID, fmt.Sprintf("  %s  role=%s  connected=%s  health=%s",
				r.ServerName, r.Role, r.Connected, r.SyncHealth))
		}
	}

	dbOut, err := commonmssql.QuerySqlcmdScalar(ctx, "AG status databases", commonmssql.AGStatusDatabaseSQL(ag))
	if err != nil {
		return err
	}
	dbs := commonmssql.ParseAGDatabaseStatus(dbOut)
	if len(dbs) == 0 {
		printSummaryNotice(ctx, stepID, "[Databases] (none in AG)")
	} else {
		printSummaryNotice(ctx, stepID, "[Databases]")
		for _, d := range dbs {
			printSummaryNotice(ctx, stepID, fmt.Sprintf("  %s  sync=%s  health=%s", d.Name, d.SyncState, d.SyncHealth))
		}
	}

	if commonmssql.AGListenerEnabled(ctx) {
		listenerOut, err := commonmssql.QuerySqlcmdScalar(ctx, "AG status listeners", commonmssql.AGStatusListenerSQL(ag))
		if err != nil {
			return err
		}
		listeners := commonmssql.ParseAGListenerStatus(listenerOut)
		if len(listeners) == 0 {
			printSummaryNotice(ctx, stepID, "[Listeners] (pending or not created)")
		} else {
			printSummaryNotice(ctx, stepID, "[Listeners]")
			for _, l := range listeners {
				addr := l.IP
				if addr == "" {
					addr = "(no IP)"
				}
				printSummaryNotice(ctx, stepID, fmt.Sprintf("  %s  %s:%s", l.DNSName, addr, l.Port))
			}
		}
	} else {
		printSummaryNotice(ctx, stepID, "[Listeners] (none — no --mssql-ag-listener-ip)")
	}

	printWSFCResourceSummary(ctx, stepID, ag)
	printSummaryNotice(ctx, stepID, "======== end status summary ========")
	return nil
}

func printWSFCResourceSummary(ctx *runner.StepContext, stepID, agName string) {
	if ctx == nil || ctx.Logger == nil || ctx.DryRun || ctx.Precheck {
		return
	}
	present, err := commonmssql.WSFCClusterPresent(ctx)
	if err != nil || !present {
		return
	}
	stdout, err := commonmssql.RunHAPowerShellScalar(ctx, "WSFC resource status", commonmssql.WSFCClusterStatusPS(agName))
	if err != nil || strings.TrimSpace(stdout) == "" {
		printSummaryNotice(ctx, stepID, "[WSFC Resources] (query failed or empty)")
		return
	}
	lines := commonmssql.ParseWSFCResourceLines(stdout)
	if len(lines) == 0 {
		printSummaryNotice(ctx, stepID, "[WSFC Resources] (none)")
		return
	}
	printSummaryNotice(ctx, stepID, "[WSFC Resources]")
	for _, line := range lines {
		switch line.Kind {
		case "CLUSTER":
			printSummaryNotice(ctx, stepID, fmt.Sprintf("  cluster %s  state=%s", line.Name, line.State))
		case "GROUP":
			printSummaryNotice(ctx, stepID, fmt.Sprintf("  group %s  state=%s", line.Name, line.State))
		case "RESOURCE":
			printSummaryNotice(ctx, stepID, fmt.Sprintf("  resource %s  type=%s  state=%s",
				line.Name, line.Type, line.State))
		}
	}
}

func printSummaryNotice(ctx *runner.StepContext, stepID, message string) {
	if ctx != nil && ctx.Logger != nil {
		ctx.Logger.ConsoleNotice(stepID, message)
	}
}
