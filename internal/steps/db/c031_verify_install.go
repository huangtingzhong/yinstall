package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// StepC031VerifyInstall 验证安装结果与连通性
func StepC031VerifyInstall() *runner.Step {
	return &runner.Step{
		ID:          "C-031",
		Name:        "Verify Installation",
		Description: "Verify database installation and connectivity",
		Tags:        []string{"db", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			// 本步必须执行（不做跳过条件）
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			hosts := ctx.HostsToRun()
			dbLogPhase(ctx, "plan", fmt.Sprintf("hosts=%d checks=4-per-host", len(hosts)))
			for _, th := range hosts {
				dbLogPhase(ctx, "host-start", fmt.Sprintf("host=%s", th.Host))
				hctx := ctx.ForHost(th)
				user := hctx.GetParamString("os_user", "yashan")
				clusterName := hctx.GetParamString("db_cluster_name", "yashandb")

				// 获取环境变量文件路径
				envFile := ""
				if envFileVal, ok := ctx.Results["env_file"]; ok {
					if envFileStr, ok := envFileVal.(string); ok {
						envFile = envFileStr
						hctx.Logger.Info("Using environment file from context: %s", envFile)
					}
				}

				// 如果没有存储的环境变量文件，使用默认的 .bashrc
				if envFile == "" {
					beginPort := hctx.GetParamInt("db_begin_port", 1688)
					homeDir, err := commonos.GetUserHomeDir(hctx, user)
					if err != nil {
						homeDir = fmt.Sprintf("/home/%s", user)
					}
					envFile = commonos.DetermineEnvFile(homeDir, beginPort)
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
				processes := []string{"yasom", "yasagent", "yasdb"}
				for _, proc := range processes {
					result, _ = hctx.Execute(fmt.Sprintf("pgrep -x %s", proc), false)
					if result != nil && result.GetExitCode() == 0 {
						pids := strings.TrimSpace(result.GetStdout())
						hctx.Logger.Info("  %s: running (PID: %s)", proc, strings.Replace(pids, "\n", ",", -1))
					} else {
						hctx.Logger.Info("  %s: not running", proc)
					}
				}

				hctx.Logger.Info("Step 4: Checking listening ports...")
				beginPort := hctx.GetParamInt("db_begin_port", 1688)
				// 使用精确匹配避免误匹配（如 1688 不会匹配到 16888）
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
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
