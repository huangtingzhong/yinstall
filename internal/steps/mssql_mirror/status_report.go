package mssql_mirror

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func printMirrorStatusSummary(ctx *runner.StepContext) error {
	if ctx == nil || ctx.Logger == nil {
		return nil
	}
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	const stepID = "M-014"
	primary := commonmssql.ResolvePrimaryHost(ctx)
	replicas := commonmssql.ReplicaHosts(ctx)
	secondary := ""
	if len(replicas) > 0 {
		secondary = strings.TrimSpace(replicas[0])
	}
	localPort := commonmssql.LocalHAEndpointPort(ctx)
	primarySQLPort := commonmssql.SQLPortForHost(ctx, primary)
	secondarySQLPort := commonmssql.SQLPortForHost(ctx, secondary)
	primaryEndpointPort := commonmssql.HAEndpointPortForHost(ctx, primary)
	secondaryEndpointPort := commonmssql.HAEndpointPortForHost(ctx, secondary)
	primaryInst := commonmssql.InstanceNameForHost(ctx, primary)
	secondaryInst := commonmssql.InstanceNameForHost(ctx, secondary)
	saPassword := commonmssql.DisplaySAPassword(ctx)

	printSummaryNotice(ctx, stepID, "======== MSSQL Database Mirroring Summary ========")
	printSummaryNotice(ctx, stepID, "[Topology]")
	printSummaryNotice(ctx, stepID, fmt.Sprintf("  primary=%s  secondary=%s", primary, secondary))
	printSummaryNotice(ctx, stepID, fmt.Sprintf("  local_mirror_endpoint_port=%d", localPort))
	if secondary != "" {
		printSummaryNotice(ctx, stepID, fmt.Sprintf("  partner_on_primary=%s",
			commonmssql.MirrorPartnerAddress(secondary, secondaryEndpointPort)))
		printSummaryNotice(ctx, stepID, fmt.Sprintf("  partner_on_secondary=%s",
			commonmssql.MirrorPartnerAddress(primary, primaryEndpointPort)))
	}

	printSummaryNotice(ctx, stepID, "[Connection]")
	if primary != "" {
		printSummaryNotice(ctx, stepID, fmt.Sprintf("  primary_server=%s",
			commonmssql.RemoteSQLServerAddress(primary, primarySQLPort, primaryInst)))
	}
	if secondary != "" {
		printSummaryNotice(ctx, stepID, fmt.Sprintf("  secondary_server=%s",
			commonmssql.RemoteSQLServerAddress(secondary, secondarySQLPort, secondaryInst)))
	}
	printSummaryNotice(ctx, stepID, fmt.Sprintf("  auth=%s", commonmssql.DisplaySqlcmdAuth(ctx)))
	if !commonmssql.UsesIntegratedSqlcmdAuth(ctx) {
		printSummaryNotice(ctx, stepID, "  login=sa")
		printSummaryNotice(ctx, stepID, fmt.Sprintf("  password=%s", saPassword))
	}
	if primary != "" {
		addr := commonmssql.RemoteSQLServerAddress(primary, primarySQLPort, primaryInst)
		printSummaryNotice(ctx, stepID, fmt.Sprintf("  sqlcmd_example=%s", commonmssql.SqlcmdConnectionExample(ctx, addr)))
	}

	if epOut, err := commonmssql.QuerySqlcmdScalarOptional(ctx, "mirror endpoint status", commonmssql.MirrorEndpointStatusSQL()); err == nil {
		eps := commonmssql.ParseMirrorEndpointStatus(epOut)
		if len(eps) == 0 {
			printSummaryNotice(ctx, stepID, "[Mirror Endpoint] (not found)")
		} else {
			printSummaryNotice(ctx, stepID, "[Mirror Endpoint]")
			for _, ep := range eps {
				printSummaryNotice(ctx, stepID, fmt.Sprintf("  %s  port=%s  state=%s", ep.Name, ep.Port, ep.State))
			}
		}
	}

	if workDir := commonmssql.MirrorWorkDir(ctx); workDir != "" {
		printSummaryNotice(ctx, stepID, "[Work Directory]")
		printSummaryNotice(ctx, stepID, fmt.Sprintf("  mirror_work_dir=%s", workDir))
		printSummaryNotice(ctx, stepID, fmt.Sprintf("  cert_dir=%s", commonmssql.MirrorCertDir(ctx)))
	}

	if dbOut, err := commonmssql.QuerySqlcmdScalarOptional(ctx, "mirror status databases", commonmssql.MirrorMirroredDatabaseStatusSQL()); err == nil {
		rows := commonmssql.ParseMirrorDatabaseStatus(dbOut)
		if len(rows) == 0 {
			printSummaryNotice(ctx, stepID, "[Mirrored Databases] (none active)")
		} else {
			printSummaryNotice(ctx, stepID, "[Mirrored Databases]")
			synced := 0
			for _, r := range rows {
				mirror := commonmssql.MirrorStateLabel(r.State)
				if r.State == "4" {
					synced++
				}
				partner := r.Partner
				if partner == "" {
					partner = "-"
				}
				role := r.Role
				if role == "" {
					role = "-"
				}
				safety := r.SafetyLevel
				if safety == "" {
					safety = "-"
				}
				printSummaryNotice(ctx, stepID, fmt.Sprintf("  %s  state=%s  mirroring=%s  role=%s  safety=%s  partner=%s",
					r.Name, r.DBState, mirror, role, safety, partner))
			}
			printSummaryNotice(ctx, stepID, fmt.Sprintf("  summary=%d database(s), %d SYNCHRONIZED", len(rows), synced))
			if synced < len(rows) {
				printSummaryNotice(ctx, stepID, "  note=some databases are not SYNCHRONIZED yet")
			}
		}
	} else {
		printSummaryNotice(ctx, stepID, "[Mirrored Databases] (query skipped)")
	}

	printSummaryNotice(ctx, stepID, "  failover_hint=ALTER DATABASE [<db>] SET PARTNER FAILOVER;  (run on principal)")
	printSummaryNotice(ctx, stepID, "======== end mirror summary ========")
	return nil
}

func printSummaryNotice(ctx *runner.StepContext, stepID, message string) {
	if ctx != nil && ctx.Logger != nil {
		ctx.Logger.ConsoleNotice(stepID, message)
	}
}
