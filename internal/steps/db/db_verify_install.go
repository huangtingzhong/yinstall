// db_verify_install.go - 验证安装结果与连通性
// --precheck/--dry-run：装前槽位是否空闲（缺 .portN 正常；存在则冲突）；apply：装后验库。

package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepVerifyInstall 验证安装结果与连通性
func stepVerifyInstall() *runner.Step {
	return &runner.Step{
		Name:        "Verify Installation",
		Description: "Verify database installation and connectivity",
		Tags:        []string{"db", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			// --precheck/--dry-run：只做装前槽位冲突探测，不验已装库连通性。
			if ctx.Precheck || ctx.DryRun {
				return precheckInstallSlotFree(ctx)
			}
			// apply：前面步骤已写出 env / 起库，PreCheck 仍做只读验收。
			return verifyInstallOnHosts(ctx)
		},

		Action: func(ctx *runner.StepContext) error {
			return verifyInstallOnHosts(ctx)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

// InstallSlotConflictReasons 根据观测事实汇总装前槽位冲突原因（纯逻辑，供单测与 PreCheck 共用）。
// 非 1688：~/.port<port> 存在即冲突；1688：不用 .bashrc 存在性，改看 ~/.yasboot/<cluster>_yasdb_home。
// 任一口径下本机 beginPort 已 LISTEN 也算冲突。
func InstallSlotConflictReasons(beginPort int, homeDir, clusterName string, portEnvExists, yasdbHomeExists, portListening bool) []string {
	var reasons []string
	homeDir = strings.TrimSpace(homeDir)
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		clusterName = "yashandb"
	}
	if beginPort != 1688 {
		if portEnvExists && homeDir != "" {
			reasons = append(reasons, fmt.Sprintf("env file already exists: %s", commonos.DetermineEnvFile(homeDir, beginPort)))
		}
	} else if yasdbHomeExists && homeDir != "" {
		reasons = append(reasons, fmt.Sprintf("yasdb home already exists: %s", path.Join(homeDir, ".yasboot", clusterName+"_yasdb_home")))
	}
	if portListening && beginPort > 0 {
		reasons = append(reasons, fmt.Sprintf("port %d already listening", beginPort))
	}
	return reasons
}

// precheckInstallSlotFree --precheck/--dry-run：目标端口/env 槽位应空闲；冲突默认 Error，force 降 Info 放行。
func precheckInstallSlotFree(ctx *runner.StepContext) error {
	hosts := ctx.HostsToRun()
	dbLogPhase(ctx, "plan", fmt.Sprintf("precheck-slot-free hosts=%d", len(hosts)))
	for _, th := range hosts {
		hctx := ctx.ForHost(th)
		user := hctx.GetParamString("os_user", "yashan")
		clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
		beginPort := hctx.GetParamInt("db_begin_port", 1688)

		homeDir, err := commonos.GetUserHomeDir(hctx, user)
		if err != nil {
			return err
		}

		portEnvExists := false
		yasdbHomeExists := false
		if beginPort != 1688 {
			envFile := commonos.DetermineEnvFile(homeDir, beginPort)
			res, _ := hctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(envFile)), false)
			portEnvExists = res != nil && res.GetExitCode() == 0
		} else {
			marker := path.Join(homeDir, ".yasboot", clusterName+"_yasdb_home")
			res, _ := hctx.Execute(fmt.Sprintf("test -e %s", commonos.ShellSingleQuote(marker)), false)
			yasdbHomeExists = res != nil && res.GetExitCode() == 0
		}

		listenRes, _ := hctx.Execute(fmt.Sprintf(
			"ss -tuln 2>/dev/null | grep -E ':%d([^0-9]|$)' >/dev/null || netstat -tuln 2>/dev/null | grep -E ':%d([^0-9]|$)' >/dev/null",
			beginPort, beginPort), false)
		portListening := listenRes != nil && listenRes.GetExitCode() == 0

		reasons := InstallSlotConflictReasons(beginPort, homeDir, clusterName, portEnvExists, yasdbHomeExists, portListening)
		if len(reasons) == 0 {
			hctx.Logger.Info("Install slot free on %s (port=%d)", th.Host, beginPort)
			continue
		}
		msg := strings.Join(reasons, "; ")
		if hctx.IsForceStep() {
			hctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Verify Installation",
				Host:        th.Host,
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.DB.INSTALL_SLOT_BUSY_FORCE",
				Message:     fmt.Sprintf("install slot busy on %s: %s; force set, precheck continues", th.Host, msg),
				Remediation: "Prefer yinstall clean or a free --db-port; force only if intentional overwrite/reuse.",
			})
			hctx.Logger.Info("Install slot busy on %s but force set: %s", th.Host, msg)
			continue
		}
		hctx.ReportPrecheckIssue(runner.PrecheckIssue{
			StepName:    "Verify Installation",
			Host:        th.Host,
			Severity:    runner.PrecheckSeverityError,
			Code:        "PC.DB.INSTALL_SLOT_BUSY",
			Message:     fmt.Sprintf("install slot busy on %s: %s", th.Host, msg),
			Remediation: fmt.Sprintf("Run yinstall clean, choose a free --db-port, or %s to acknowledge and continue precheck", hctx.ForceStepsHint()),
		})
		return fmt.Errorf("install slot busy on %s: %s", th.Host, msg)
	}
	return nil
}

// verifyInstallOnHosts 只读：cluster status / CDB(+PDB) 连通 / 关键进程 / 监听端口。
func verifyInstallOnHosts(ctx *runner.StepContext) error {
	hosts := ctx.HostsToRun()
	dbLogPhase(ctx, "plan", fmt.Sprintf("hosts=%d checks=4-per-host", len(hosts)))
	for _, th := range hosts {
		dbLogPhase(ctx, "host-start", fmt.Sprintf("host=%s", th.Host))
		hctx := ctx.ForHost(th)
		user := hctx.GetParamString("os_user", "yashan")
		clusterName := hctx.GetParamString("db_cluster_name", "yashandb")

		envFile := ""
		if envFileVal, ok := ctx.Results["env_file"]; ok {
			if envFileStr, ok := envFileVal.(string); ok {
				envFile = envFileStr
				hctx.Logger.Info("Using environment file from context: %s", envFile)
			}
		}
		if envFile == "" {
			beginPort := hctx.GetParamInt("db_begin_port", 1688)
			var err error
			envFile, err = commonos.ResolveEnvFileForUser(ctx, hctx, user, beginPort)
			if err != nil {
				return err
			}
			hctx.Logger.Info("Using derived environment file: %s", envFile)
		}

		hctx.Logger.Info("Verifying database installation...")

		hctx.Logger.Info("Step 1: Checking cluster status...")
		result, _ := commonos.ExecuteAsUserWithEnv(hctx, user, envFile, fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), false)
		if result != nil && result.GetExitCode() == 0 {
			hctx.Logger.Info("Cluster status: OK")
			for _, line := range strings.Split(result.GetStdout(), "\n") {
				line = strings.TrimSpace(line)
				if strings.Contains(line, "database_status") ||
					strings.Contains(line, "database_role") ||
					strings.Contains(line, "instance_status") {
					hctx.Logger.Info("  %s", line)
				}
			}
		} else {
			hctx.Logger.Warn("Failed to get cluster status")
		}

		hctx.Logger.Info("Step 2: Checking CDB connectivity...")
		if res, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "connectivity-dual", "SELECT 1 FROM dual", false); err != nil {
			if res != nil && !runner.CommandExitLogged(err) {
				commonsql.ReportSQLFailure(hctx, "SELECT 1 FROM dual", res)
			}
			return fmt.Errorf("CDB connectivity check failed on host %s: %w", th.Host, err)
		}
		hctx.Logger.Info("CDB connectivity: OK")

		if ctxCDBEnabled(hctx) {
			hctx.Logger.Info("Step 2b: Checking PDB connectivity...")
			if err := forEachPDBTarget(hctx, func(pdbName string) error {
				label := "connectivity-dual-" + pdbName
				if res, err := dbRunSQLInPDBPhase(hctx, user, envFile, clusterName, pdbName, label, "SELECT 1 FROM dual", false); err != nil {
					if res != nil && !runner.CommandExitLogged(err) {
						commonsql.ReportSQLFailure(hctx, "SELECT 1 FROM dual", res)
					}
					return fmt.Errorf("PDB %s connectivity check failed: %w", pdbName, err)
				}
				hctx.Logger.Info("  PDB %s connectivity: OK", pdbName)
				return nil
			}); err != nil {
				return fmt.Errorf("PDB connectivity check failed on host %s: %w", th.Host, err)
			}
		}

		hctx.Logger.Info("Step 3: Checking key processes...")
		beginPort := hctx.GetParamInt("db_begin_port", 1688)
		processes := []string{"yasom", "yasagent", "yasdb"}
		for _, proc := range processes {
			ok, pids := probeDBClusterProcess(hctx, clusterName, beginPort, proc)
			if ok {
				hctx.Logger.Info("  %s: running (PID: %s)", proc, strings.Join(pids, ","))
			} else {
				hctx.Logger.Info("  %s: not running", proc)
			}
		}

		hctx.Logger.Info("Step 4: Checking listening ports...")
		result, _ = hctx.Execute(fmt.Sprintf("ss -tuln | grep -E ':%d([^0-9]|$)'", beginPort), false)
		if result != nil && result.GetExitCode() == 0 {
			hctx.Logger.Info("  Port %d: listening", beginPort)
		} else {
			hctx.Logger.Info("  Port %d: not listening", beginPort)
		}

		hctx.Logger.Info("Installation verification completed")
		dbLogPhase(hctx, "host-done", fmt.Sprintf("host=%s", th.Host))
	}
	return nil
}
