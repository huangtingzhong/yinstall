package mssql

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func printMssqlInstallSummary(ctx *runner.StepContext, stepID string) error {
	if ctx == nil || ctx.Logger == nil || ctx.DryRun || ctx.Precheck {
		return nil
	}
	if stepID == "" {
		stepID = ctx.CurrentStepID
	}

	profile := commonmssql.BuildInstanceProfile(ctx)
	layout := commonmssql.ResolveLayoutFromContext(ctx)
	if entry, ok := commonmssql.RegistryEntryFromContext(ctx); ok {
		layout = commonmssql.EnrichLayoutProgramPathsFromRegistry(layout, entry)
	}
	if !ctx.DryRun && !ctx.Precheck {
		if enriched, err := commonmssql.EnrichLayoutWithInstalledPaths(ctx, layout); err == nil {
			layout = enriched
			if entry, ok := commonmssql.RegistryEntryFromContext(ctx); ok {
				layout = commonmssql.EnrichLayoutProgramPathsFromRegistry(layout, entry)
			}
		}
	}

	host := commonmssql.TargetHost(ctx)
	remoteServer := commonmssql.RemoteSQLServerAddress(host, profile.Port, profile.Instance)
	saPassword := commonmssql.DisplaySAPassword(ctx)
	profilePath := commonmssql.InstanceProfilePathFromResults(ctx)
	if profilePath == "" {
		if p, err := commonmssql.ResolveInstanceProfilePath(ctx, profile.Port); err == nil {
			profilePath = p
		}
	}
	engineSvc, agentSvc := commonmssql.SqlEngineAndAgentServiceNames(profile.Instance)

	notice := func(msg string) {
		ctx.Logger.ConsoleNotice(stepID, msg)
	}

	notice(fmt.Sprintf("========== MSSQL Install Summary (%s) ==========", host))
	notice("[Deployment]")
	notice(fmt.Sprintf("  topology=%s  instance=%s  port=%d", commonmssql.InstallSummaryTopology(ctx), profile.Instance, profile.Port))
	if product := commonmssql.InstallSummaryProductLine(ctx, layout); product != "" {
		notice(fmt.Sprintf("  product=%s", product))
	}
	if media := commonmssql.SetupMediaLabel(ctx); media != "" {
		notice(fmt.Sprintf("  setup_media=%s", media))
	}
	if ctx.GetParamBool("skip_os", false) {
		notice("  skip_os=true")
	}

	notice("[Connection]")
	notice(fmt.Sprintf("  host=%s  port=%d  instance=%s", host, profile.Port, profile.Instance))
	notice(fmt.Sprintf("  remote_server=%s  local_server=%s", remoteServer, profile.Server))
	if commonmssql.UsesIntegratedSqlcmdAuth(ctx) {
		notice("  auth=Windows Authentication (-E)")
		notice(fmt.Sprintf("  sqlcmd_example=%s", commonmssql.SqlcmdConnectionExample(ctx, remoteServer)))
		if saPassword != "" && saPassword != "(not configured)" {
			notice("  sa_login=sa (configured; use -U sa for SQL auth)")
			notice(fmt.Sprintf("  sa_password=%s", saPassword))
			notice(fmt.Sprintf("  sqlcmd_sa_example=%s", commonmssql.SqlcmdSAConnectionExample(ctx, remoteServer)))
		}
	} else {
		notice("  auth=SQL Server Authentication")
		notice("  login=sa")
		notice(fmt.Sprintf("  password=%s", saPassword))
		notice(fmt.Sprintf("  sqlcmd_example=%s", commonmssql.SqlcmdConnectionExample(ctx, remoteServer)))
	}

	if verOut, err := commonmssql.QuerySqlcmdScalarOptional(ctx, "install summary product version", commonmssql.SQLProductVersionSQL()); err == nil {
		ver, edition, level := commonmssql.ParseSQLProductVersion(verOut)
		if ver != "" {
			notice(fmt.Sprintf("  version=%s  edition=%s  level=%s", ver, edition, level))
		}
	}

	if mb, ok := commonmssql.MaxServerMemorySummaryMB(ctx); ok {
		notice(fmt.Sprintf("  max_server_memory_mb=%d", mb))
	}
	if haPort := ctx.GetParamInt("mssql_ha_endpoint_port", 0); haPort > 0 {
		notice(fmt.Sprintf("  ha_endpoint_port=%d", haPort))
	}

	notice("[Paths]")
	notice(fmt.Sprintf("  yinstall_dir=%s", layout.AdminBase))
	if layout.ProgramDir != "" {
		notice(fmt.Sprintf("  program_dir=%s", layout.ProgramDir))
	}
	if layout.SharedDir != "" {
		notice(fmt.Sprintf("  shared_dir=%s", layout.SharedDir))
	}
	if layout.InstanceDir != "" {
		notice(fmt.Sprintf("  instance_dir=%s", layout.InstanceDir))
	}
	if layout.Base != "" {
		notice(fmt.Sprintf("  instance_root=%s", layout.Base))
	}
	if profile.DataDir != "" {
		notice(fmt.Sprintf("  data_dir=%s", profile.DataDir))
	} else if layout.DataDir != "" {
		notice(fmt.Sprintf("  data_dir=%s", layout.DataDir))
	} else {
		notice("  data_dir=(SQL Server default under Program Files)")
	}
	if profile.LogDir != "" && profile.LogDir != profile.DataDir {
		notice(fmt.Sprintf("  log_dir=%s", profile.LogDir))
	}
	if profile.BackupDir != "" {
		notice(fmt.Sprintf("  backup_dir=%s", profile.BackupDir))
	} else if layout.BackupDir != "" {
		notice(fmt.Sprintf("  backup_dir=%s", layout.BackupDir))
	}
	if profilePath != "" {
		notice(fmt.Sprintf("  profile_env=%s  (dot-source on target: . '%s')", profilePath, profilePath))
	}
	if profile.SQLCmdPath != "" {
		notice(fmt.Sprintf("  sqlcmd=%s", profile.SQLCmdPath))
	}

	notice("[Health]")
	engineStatus, _ := commonmssql.QueryWindowsServiceStatus(ctx, engineSvc)
	agentStatus, _ := commonmssql.QueryWindowsServiceStatus(ctx, agentSvc)
	portOK := commonmssql.ProbeTCPPortListening(ctx, profile.Port)
	verifyOK := commonmssql.SqlcmdVerifySucceeded(ctx)
	notice(fmt.Sprintf("  engine_service=%s status=%s %s", engineSvc, engineStatus, commonmssql.SummaryOKLabel(engineStatus == commonmssql.InstanceServiceRunning)))
	notice(fmt.Sprintf("  agent_service=%s status=%s %s", agentSvc, agentStatus, commonmssql.SummaryOKLabel(agentStatus == commonmssql.InstanceServiceRunning)))
	notice(fmt.Sprintf("  tcp_port_%d=%s", profile.Port, commonmssql.SummaryOKLabel(portOK)))
	notice(fmt.Sprintf("  sqlcmd_verify=%s", commonmssql.SummaryOKLabel(verifyOK)))

	if dbOut, err := commonmssql.QuerySqlcmdScalarOptional(ctx, "install summary user databases", commonmssql.UserDatabaseListSQL()); err == nil {
		dbs := commonmssql.ParseUserDatabaseList(dbOut)
		if len(dbs) == 0 {
			notice("[User Databases] (none)")
		} else {
			notice("[User Databases]")
			for _, db := range dbs {
				notice(fmt.Sprintf("  %s  state=%s  recovery=%s", db.Name, db.State, db.RecoveryModel))
			}
		}
	} else {
		notice("[User Databases] (query skipped)")
	}

	notice("[Service]")
	notice(fmt.Sprintf("  engine_service=%s", engineSvc))
	notice(fmt.Sprintf("  agent_service=%s", agentSvc))
	notice(fmt.Sprintf("  start_cmd=net start %s & net start %s", engineSvc, agentSvc))
	notice(fmt.Sprintf("  stop_cmd=net stop %s & net stop %s", agentSvc, engineSvc))
	notice(fmt.Sprintf("  status_cmd=sc query %s", engineSvc))
	if setupRoot, _ := ctx.Results["mssql_setup_root"].(string); strings.TrimSpace(setupRoot) != "" {
		notice(fmt.Sprintf("  setup_root=%s", strings.TrimSpace(setupRoot)))
	}

	notice("====================================================")
	return nil
}

func hasCustomSQLScript(ctx *runner.StepContext) bool {
	return ctx != nil && strings.TrimSpace(ctx.GetParamString("mssql_custom_sql_script", "")) != ""
}
