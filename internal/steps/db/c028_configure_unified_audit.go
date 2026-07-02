package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// auditEnableParamSQL 在 CDB$ROOT 开启统一审计（实例级参数）。
const auditEnableParamSQL = `ALTER SYSTEM SET UNIFIED_AUDITING=true`

// auditPoliciesSQL 在各 PDB（或非 CDB 实例）创建并启用审计策略（installer.md §5.5.1.1）。
const auditPoliciesSQL = `CREATE AUDIT POLICY UP1 PRIVILEGES CREATE ANY TABLE, CREATE TABLE, ALTER ANY TABLE, DROP ANY TABLE, GRANT ANY PRIVILEGE, GRANT ANY OBJECT PRIVILEGE, GRANT ANY ROLE, CREATE USER, ALTER USER, DROP USER, DROP ANY ROLE, AUDIT SYSTEM;
CREATE AUDIT POLICY UP2 ACTIONS DROP TABLE, DROP ROLE, CREATE AUDIT POLICY, ALTER AUDIT POLICY, DROP AUDIT POLICY, AUDIT, NOAUDIT;
CREATE AUDIT POLICY UP3 ACTIONS LOGON, LOGOFF;
AUDIT POLICY UP3 BY SYS;
AUDIT POLICY UP1;
AUDIT POLICY UP2`

// auditPolicySQL 非 CDB 模式下一律在实例内执行的完整审计初始化（参数 + 策略）。
const auditPolicySQL = auditEnableParamSQL + `;
` + auditPoliciesSQL

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

// StepC028ConfigureUnifiedAudit 开启统一审计、创建策略与清理作业（--db-unified-audit，默认关闭）。
// CDB 模式：UNIFIED_AUDITING 参数与调度作业在 CDB$ROOT；审计策略在每个 --db-pdb 内创建。
// 非 CDB：参数、策略与作业均在实例内执行。
func StepC028ConfigureUnifiedAudit() *runner.Step {
	return &runner.Step{
		ID:          "C-028",
		Name:        "Configure Unified Audit",
		Description: "Enable unified auditing, audit policies, and purge scheduler jobs",
		Tags:        []string{"db", "audit", "security"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("db_unified_audit", false) {
				return fmt.Errorf("unified audit not enabled (--db-unified-audit=false), skipping")
			}
			if ctxCDBEnabled(ctx) {
				names, err := pdbNamesFromCtx(ctx)
				if err != nil {
					return fmt.Errorf("invalid --db-pdb for unified audit: %w", err)
				}
				if len(names) == 0 {
					return fmt.Errorf("multitenant unified audit requires at least one --db-pdb")
				}
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
			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)
			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile := resolveDBEnvFile(ctx, hctx)

			runCDBSQL := func(label, sql string) error {
				_, err := dbRunSQLPhase(hctx, user, envFile, clusterName, label, sql, true)
				return err
			}
			runPDBSQL := func(pdbName, label, sql string) error {
				_, err := dbRunSQLInPDBPhase(hctx, user, envFile, clusterName, pdbName, label, sql, true)
				return err
			}

			runAuditJobs := func(labelPrefix string) error {
				prefix := labelPrefix
				if prefix != "" {
					prefix += "-"
				}
				if err := runCDBSQL(prefix+"audit-scheduler-job", auditSchedulerJobSQL); err != nil {
					return fmt.Errorf("audit scheduler job creation failed: %w", err)
				}
				hctx.Logger.Info("Audit scheduler job created in CDB$ROOT")
				if err := runCDBSQL(prefix+"audit-purge-job", auditPurgeJobSQL); err != nil {
					return fmt.Errorf("audit purge job creation failed: %w", err)
				}
				hctx.Logger.Info("Unified audit purge job created in CDB$ROOT")
				return nil
			}

			if ctxCDBEnabled(hctx) {
				dbLogPhase(ctx, "plan", "op=audit-cdb-param+pdb-policies")
				hctx.Logger.Info("Enabling UNIFIED_AUDITING in CDB$ROOT...")
				if err := runCDBSQL("audit-enable-param", auditEnableParamSQL); err != nil {
					return fmt.Errorf("UNIFIED_AUDITING parameter setup failed: %w", err)
				}
				hctx.Logger.Info("UNIFIED_AUDITING enabled in CDB$ROOT")
				if err := runAuditJobs("cdb"); err != nil {
					return err
				}
				return forEachPDBTarget(hctx, func(pdbName string) error {
					hctx.Logger.Info("Creating audit policies in PDB %s...", pdbName)
					if err := runPDBSQL(pdbName, pdbName+"-audit-policies", auditPoliciesSQL); err != nil {
						return fmt.Errorf("audit policy setup failed in PDB %s: %w", pdbName, err)
					}
					hctx.Logger.Info("Audit policies created and enabled in PDB %s", pdbName)
					return nil
				})
			}

			dbLogPhase(ctx, "plan", "sqls=3 op=audit-policy+scheduler+purge")
			hctx.Logger.Info("Enabling unified auditing in CDB$ROOT...")
			if err := runCDBSQL("audit-policy", auditPolicySQL); err != nil {
				return fmt.Errorf("audit policy setup failed: %w", err)
			}
			hctx.Logger.Info("Audit policies created and enabled")
			return runAuditJobs("")
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
				return fmt.Errorf("unified auditing verification failed in CDB$ROOT: %w", err)
			}
			out := ""
			if res != nil {
				out = strings.ToUpper(strings.TrimSpace(res.Stdout))
			}
			if !strings.Contains(out, "TRUE") {
				return fmt.Errorf("UNIFIED_AUDITING is not enabled in CDB$ROOT; query output: %s", strings.TrimSpace(res.Stdout))
			}
			hctx.Logger.Info("Verified UNIFIED_AUDITING is enabled in CDB$ROOT")
			return nil
		},
	}
}

// resolveDBEnvFile 优先使用 C-023 写入的 env_file，否则按用户家目录与端口推导。
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
