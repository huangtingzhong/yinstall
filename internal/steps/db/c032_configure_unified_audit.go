package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// auditPolicySQL 开启统一审计并创建/启用审计策略（installer.md §5.5.1.1）。
const auditPolicySQL = `ALTER SYSTEM SET UNIFIED_AUDITING=true;
CREATE AUDIT POLICY UP1 PRIVILEGES CREATE ANY TABLE, CREATE TABLE, ALTER ANY TABLE, DROP ANY TABLE, GRANT ANY PRIVILEGE, GRANT ANY OBJECT PRIVILEGE, GRANT ANY ROLE, CREATE USER, ALTER USER, DROP USER, DROP ANY ROLE, AUDIT SYSTEM;
CREATE AUDIT POLICY UP2 ACTIONS DROP TABLE, DROP ROLE, CREATE AUDIT POLICY, ALTER AUDIT POLICY, DROP AUDIT POLICY, AUDIT, NOAUDIT;
CREATE AUDIT POLICY UP3 ACTIONS LOGON, LOGOFF;
AUDIT POLICY UP3 BY SYS;
AUDIT POLICY UP1;
AUDIT POLICY UP2`

// auditSchedulerJobSQL 定时更新审计归档时间戳（installer.md §5.5.1.2）。
const auditSchedulerJobSQL = `BEGIN
DBMS_SCHEDULER.CREATE_JOB (
'update_audit_archive_time',
'PLSQL_BLOCK',
'BEGIN DBMS_AUDIT_MGMT.SET_LAST_ARCHIVE_TIMESTAMP(DBMS_AUDIT_MGMT.AUDIT_TRAIL_UNIFIED, sysdate-270);END;' ,
0,
SYSDATE,
'sysdate+1',
NULL,
'DEFAULT_JOB_CLASS',
TRUE,
FALSE,
'update audit archive time');
END;
/`

// auditPurgeJobSQL 创建统一审计 trail 清理作业（installer.md §5.5.1.2）。
const auditPurgeJobSQL = `BEGIN
DBMS_AUDIT_MGMT.CREATE_PURGE_JOB (
DBMS_AUDIT_MGMT.AUDIT_TRAIL_UNIFIED,
SYSDATE + 5/24,
'sysdate + 1',
'audit_job',
TRUE);
END;
/`

// StepC032ConfigureUnifiedAudit 开启统一审计、创建策略与清理作业（--db-unified-audit，默认关闭）。
// 须在 C-024（环境变量）之后执行。
func StepC032ConfigureUnifiedAudit() *runner.Step {
	return &runner.Step{
		ID:          "C-032",
		Name:        "Configure Unified Audit",
		Description: "Enable unified auditing, audit policies, and purge scheduler jobs",
		Tags:        []string{"db", "audit", "security"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("db_unified_audit", false) {
				return fmt.Errorf("unified audit not enabled (--db-unified-audit=false), skipping")
			}

			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			yasbootPath := path.Join(stageDir, "bin", "yasboot")
			result, err := ctx.Execute(fmt.Sprintf("test -f %s", yasbootPath), false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("yasboot not found at %s, database may not be deployed yet", yasbootPath))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "sqls=3 op=audit-policy+scheduler+purge")
			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)

			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile := resolveDBEnvFile(ctx, hctx)

			hctx.Logger.Info("Enabling unified auditing and creating audit policies...")

			if _, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "audit-policy", auditPolicySQL, true); err != nil {
				return fmt.Errorf("audit policy setup failed: %w", err)
			}
			hctx.Logger.Info("Audit policies created and enabled")

			hctx.Logger.Info("Creating audit archive timestamp scheduler job...")
			if _, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "audit-scheduler-job", auditSchedulerJobSQL, true); err != nil {
				return fmt.Errorf("audit scheduler job creation failed: %w", err)
			}
			hctx.Logger.Info("Audit scheduler job created")

			hctx.Logger.Info("Creating unified audit purge job...")
			if _, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "audit-purge-job", auditPurgeJobSQL, true); err != nil {
				return fmt.Errorf("audit purge job creation failed: %w", err)
			}
			hctx.Logger.Info("Unified audit purge job created")

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("db_unified_audit", false) {
				return nil
			}

			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)

			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile := resolveDBEnvFile(ctx, hctx)

			checkSQL := `SELECT value FROM v$parameter WHERE name = 'UNIFIED_AUDITING'`
			res, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "verify-unified-auditing", checkSQL, false)
			if err != nil {
				if res != nil && !runner.CommandExitLogged(err) {
					commonsql.ReportSQLFailure(hctx, checkSQL, res)
				}
				return fmt.Errorf("unified auditing verification query failed: %w", err)
			}

			out := ""
			if res != nil {
				out = strings.ToUpper(strings.TrimSpace(res.Stdout))
			}
			if !strings.Contains(out, "TRUE") {
				return fmt.Errorf("UNIFIED_AUDITING is not enabled after C-032; query output: %s", strings.TrimSpace(res.Stdout))
			}

			hctx.Logger.Info("Verified UNIFIED_AUDITING is enabled")
			return nil
		},
	}
}

// resolveDBEnvFile 优先使用 C-024 写入的 env_file，否则按用户家目录与端口推导。
func resolveDBEnvFile(ctx *runner.StepContext, hctx *runner.StepContext) string {
	if envFileVal, ok := ctx.Results["env_file"]; ok {
		if envFileStr, ok := envFileVal.(string); ok && envFileStr != "" {
			return envFileStr
		}
	}

	user := hctx.GetParamString("os_user", "yashan")
	beginPort := hctx.GetParamInt("db_begin_port", 1688)
	homeDir, err := commonos.GetUserHomeDir(hctx, user)
	if err != nil {
		homeDir = fmt.Sprintf("/home/%s", user)
	}
	return commonos.DetermineEnvFile(homeDir, beginPort)
}
