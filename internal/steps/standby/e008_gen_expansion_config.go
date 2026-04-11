// e004_gen_expansion_config.go - 生成扩容配置文件
// 本步骤在主库执行 yasboot config node gen 生成扩容所需的配置文件
// 执行 yasboot 命令前会先 source 环境变量配置文件

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE008GenExpansionConfig 生成扩容配置文件步骤
func StepE008GenExpansionConfig() *runner.Step {
	return &runner.Step{
		ID:          "E-008",
		Name:        "Generate Expansion Config",
		Description: "Generate hosts_add.toml and cluster_add.toml configuration files",
		Tags:        []string{"standby", "config"},

		PreCheck: func(ctx *runner.StepContext) error {
			// Validate required parameters
			if ctx.GetParamString("db_cluster_name", "") == "" {
				return fmt.Errorf("db_cluster_name is required")
			}
			if ctx.GetParamString("os_user_password", "") == "" {
				return fmt.Errorf("os_user_password is required for yasboot config node gen")
			}
			targets := ctx.GetParamStringSlice("standby_targets")
			if len(targets) == 0 {
				return fmt.Errorf("standby_targets is required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			primaryUser := GetPrimaryOSUser(ctx)
			password := ctx.GetParamString("os_user_password", "")
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			dataPath := ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
			logPath := ctx.GetParamString("db_log_path", "/data/yashan/log")
			nodeCount := ctx.GetParamInt("standby_node_count", 1)
			targetsStr := ctx.GetParamString("standby_targets_str", "")

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			ctx.Logger.Info("Using primary environment file: %s", envFile)

			// Determine cluster name:
			// 1. If primary_env_file is specified, extract from environment file
			// 2. Otherwise, use db_cluster_name parameter
			var clusterName string
			specifiedEnvFile := ctx.GetParamString("primary_env_file", "")
			if specifiedEnvFile != "" {
				// primary_env_file is specified, extract cluster name from it
				clusterName, err = ExtractClusterNameFromEnvFile(ctx, envFile)
				if err != nil {
					return fmt.Errorf("failed to extract cluster name from specified environment file %s: %w", envFile, err)
				}
				ctx.Logger.Info("Extracted cluster name from specified environment file: %s", clusterName)
				// Update db_cluster_name parameter with extracted value for subsequent steps
				if ctx.Params == nil {
					ctx.Params = make(map[string]interface{})
				}
				ctx.Params["db_cluster_name"] = clusterName
				ctx.Logger.Info("Updated db_cluster_name parameter to: %s", clusterName)
			} else {
				// primary_env_file is not specified, use db_cluster_name parameter
				clusterName = ctx.GetParamString("db_cluster_name", "yashandb")
				ctx.Logger.Info("Using cluster name from parameter: %s", clusterName)
			}

			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			ctx.Logger.Info("Generating expansion configuration files")
			ctx.Logger.Info("  Cluster: %s", clusterName)
			ctx.Logger.Info("  Standby targets: %s", targetsStr)
			ctx.Logger.Info("  Node count: %d", nodeCount)
			ctx.Logger.Info("  Begin port: %d", beginPort)
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			// Check if host-id is provided or if we need to query it
			hostID := ctx.GetParamString("standby_host_id", "")
			targets := ctx.GetParamStringSlice("standby_targets")

			// Build yasboot config node gen command
			var genCmd string
			escapedPwd := commonos.ShellEscapeForSuC(password)
			if hostID != "" {
				ctx.Logger.Info("Using provided host-id: %s", hostID)
				genCmd = fmt.Sprintf(`cd %s && yasboot config node gen \
-c %s -u %s -p %s \
--host-ids %s --port 22 \
--install-path %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--node %d`,
					stageDir, clusterName, primaryUser, escapedPwd,
					hostID,
					installPath, dataPath, logPath,
					beginPort,
					nodeCount)
			} else {
				genCmd = fmt.Sprintf(`cd %s && yasboot config node gen \
-c %s -u %s -p %s \
--ip %s --port 22 \
--install-path %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--node %d`,
					stageDir, clusterName, primaryUser, escapedPwd,
					targetsStr,
					installPath, dataPath, logPath,
					beginPort,
					nodeCount)
			}

			// Execute as primary user with environment sourced
			ctx.Logger.Info("Running: yasboot config node gen ...")
			result, err := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, genCmd, true)
			if err != nil {
				// If command failed and error indicates host exists, try to get host-id and retry
				if result != nil {
					stdout := result.GetStdout()
					stderr := result.GetStderr()
					output := stdout + stderr
					if strings.Contains(output, "host") && strings.Contains(output, "exist") && strings.Contains(output, "--host-id") {
						ctx.Logger.Warn("Host exists, attempting to query host-id from cluster status")

						// Query cluster status to get host-id
						statusCmd := fmt.Sprintf("yasboot process yasagent status -c %s", clusterName)
						statusResult, statusErr := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, statusCmd, true)
						if statusErr == nil && statusResult != nil && statusResult.GetExitCode() == 0 {
							// Parse status output to extract host-id for the target IP
							statusOutput := statusResult.GetStdout()
							lines := strings.Split(statusOutput, "\n")
							for _, line := range lines {
								line = strings.TrimSpace(line)
								if strings.HasPrefix(line, "|") {
									parts := strings.Split(line, "|")
									if len(parts) >= 5 {
										hostIDFromStatus := strings.TrimSpace(parts[1])
										listenAddr := strings.TrimSpace(parts[4])
										// Extract IP from listen_address (format: IP:PORT)
										if idx := strings.Index(listenAddr, ":"); idx > 0 {
											ip := listenAddr[:idx]
											// Check if this IP matches any target
											for _, target := range targets {
												if ip == strings.TrimSpace(target) && hostIDFromStatus != "" {
													ctx.Logger.Info("Found host-id %s for IP %s, retrying with --host-ids", hostIDFromStatus, ip)
												genCmd = fmt.Sprintf(`cd %s && yasboot config node gen \
-c %s -u %s -p %s \
--host-ids %s --port 22 \
--install-path %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--node %d`,
														stageDir, clusterName, primaryUser, escapedPwd,
														hostIDFromStatus,
														installPath, dataPath, logPath,
														beginPort,
														nodeCount)
													result, err = commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile, genCmd, true)
													if err == nil {
														break
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
				if err != nil {
					return fmt.Errorf("failed to generate expansion config: %w", err)
				}
			}

			if result.GetStdout() != "" {
				ctx.Logger.Info("Command output:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}
			}

			ctx.Logger.Info("Expansion configuration generated successfully")

			// Store cluster name in context for PostCheck
			ctx.Results["extracted_cluster_name"] = clusterName

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")

			// Get cluster name from context (set in Action)
			var clusterName string
			if storedName, ok := ctx.Results["extracted_cluster_name"].(string); ok && storedName != "" {
				clusterName = storedName
			} else {
				// Fallback: use same logic as Action
				specifiedEnvFile := ctx.GetParamString("primary_env_file", "")
				if specifiedEnvFile != "" {
					envFile, err := GetPrimaryEnvFile(ctx)
					if err == nil {
						clusterName, _ = ExtractClusterNameFromEnvFile(ctx, envFile)
					}
				}
				if clusterName == "" {
					clusterName = ctx.GetParamString("db_cluster_name", "yashandb")
				}
			}

			// Check hosts_add.toml exists
			hostsAddFile := fmt.Sprintf("%s/hosts_add.toml", stageDir)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", hostsAddFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("hosts_add.toml not found at %s", hostsAddFile)
			}
			ctx.Logger.Info("Found: %s", hostsAddFile)

			// Check cluster_add.toml exists
			clusterAddFile := fmt.Sprintf("%s/%s_add.toml", stageDir, clusterName)
			result, _ = ctx.Execute(fmt.Sprintf("test -f %s", clusterAddFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("%s_add.toml not found at %s", clusterName, clusterAddFile)
			}
			ctx.Logger.Info("Found: %s", clusterAddFile)

			return nil
		},
	}
}
