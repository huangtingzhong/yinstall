package db

import (
	"fmt"
	"os"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepC014ShowClusterStatus Display cluster status at the end of installation
func StepC028ShowClusterStatus() *runner.Step {
	return &runner.Step{
		ID:          "C-028",
		Name:        "Show Cluster Status",
		Description: "Display cluster status information",
		Tags:        []string{"db", "status", "display"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			// Always run this step
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			// 只在第一个节点执行（对于单机版）或主节点（对于YAC）
			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)

			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")

			// 获取环境变量文件路径
			envFile := ""
			if envFileVal, ok := ctx.Results["env_file"]; ok {
				if envFileStr, ok := envFileVal.(string); ok {
					envFile = envFileStr
				}
			}

			// 如果没有存储的环境变量文件，使用默认的 .bashrc
			if envFile == "" {
				envFile = fmt.Sprintf("/home/%s/.bashrc", user)
			}

			hctx.Logger.Info("Displaying cluster status for cluster: %s", clusterName)

			// 执行 yasboot cluster status 命令
			cmd := fmt.Sprintf("su - %s -c 'source %s && yasboot cluster status -c %s -d'", user, envFile, clusterName)
			result, _ := hctx.Execute(cmd, false)

			if result != nil && result.GetExitCode() == 0 {
				output := result.GetStdout()
				if output != "" {
					// 输出到日志
					hctx.Logger.Info("========== Cluster Status ==========")
					for _, line := range strings.Split(output, "\n") {
						line = strings.TrimSpace(line)
						if line != "" {
							hctx.Logger.Info("%s", line)
						}
					}
					hctx.Logger.Info("=====================================")

					// 同时输出到终端标准输出
					fmt.Fprintf(os.Stdout, "\n========== Cluster Status ==========\n")
					for _, line := range strings.Split(output, "\n") {
						line = strings.TrimSpace(line)
						if line != "" {
							fmt.Fprintf(os.Stdout, "%s\n", line)
						}
					}
					fmt.Fprintf(os.Stdout, "=====================================\n\n")
				} else {
					hctx.Logger.Warn("Cluster status command returned empty output")
				}
			} else {
				errMsg := "Failed to get cluster status"
				if result != nil {
					if result.GetStderr() != "" {
						errMsg = result.GetStderr()
					} else if result.GetStdout() != "" {
						errMsg = result.GetStdout()
					}
				}
				hctx.Logger.Warn("Failed to get cluster status: %s", errMsg)
				return fmt.Errorf("failed to get cluster status: %s", errMsg)
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
