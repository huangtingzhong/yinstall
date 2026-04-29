package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC025VerifyInstall 验证安装结果与连通性
func StepC025VerifyInstall() *runner.Step {
	return &runner.Step{
		ID:          "C-025",
		Name:        "Verify Installation",
		Description: "Verify database installation and connectivity",
		Tags:        []string{"db", "verify"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			// 本步必须执行（不做跳过条件）
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
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

				hctx.Logger.Info("Step 2: Checking database connectivity...")
				result, _ = commonos.ExecuteAsUserWithEnv(hctx, user, envFile, `yasql / as sysdba -c "SELECT 1 FROM dual;"`, false)
				if result != nil && result.GetExitCode() == 0 {
					hctx.Logger.Info("Database connectivity: OK")
				} else {
					result, _ = commonos.ExecuteAsUserWithEnv(hctx, user, envFile, `echo "SELECT 1 FROM dual;" | yasql / as sysdba`, false)
					if result != nil && result.GetExitCode() == 0 {
						hctx.Logger.Info("Database connectivity: OK")
					} else {
						hctx.Logger.Warn("Database connectivity check failed")
						if result != nil {
							hctx.Logger.Info("Output: %s", result.GetStdout())
							hctx.Logger.Info("Error: %s", result.GetStderr())
						}
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
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
