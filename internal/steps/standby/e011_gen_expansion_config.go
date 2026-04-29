// e011_gen_expansion_config.go - 生成扩容配置文件
// 本步骤在主库执行 yasboot config node gen 生成扩容所需的配置文件
// 执行 yasboot 命令前会先 source 环境变量配置文件

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE011GenExpansionConfig 生成扩容配置文件步骤
func StepE011GenExpansionConfig() *runner.Step {
	return &runner.Step{
		ID:          "E-011",
		Name:        "Generate Expansion Config",
		Description: "Generate hosts_add.toml and cluster_add.toml configuration files",
		Tags:        []string{"standby", "config"},

		PreCheck: func(ctx *runner.StepContext) error {
			// 集群名由 CLI 入口 trySync 或 Action 内 GetPrimaryEnvFile+Sync 解析，此处不强制。
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
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return fmt.Errorf("failed to sync cluster name from primary env: %w", err)
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			ctx.Logger.Info("Using cluster name: %s", clusterName)

			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			ybSSHPort := ctx.YasbootRemoteSSHPort(22)

			ctx.Logger.Info("Generating expansion configuration files")
			ctx.Logger.Info("  Cluster: %s", clusterName)
			ctx.Logger.Info("  Standby targets: %s", targetsStr)
			ctx.Logger.Info("  Node count: %d", nodeCount)
			ctx.Logger.Info("  Begin port: %d", beginPort)
			ctx.Logger.Info("  Yasboot remote SSH port: %d", ybSSHPort)
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			// Check if host-id is provided or if we need to query it
			hostID := ctx.GetParamString("standby_host_id", "")
			targets := ctx.GetParamStringSlice("standby_targets")

			// Build yasboot config node gen command
			var genCmd string
			escapedPwd := commonos.ShellSingleQuote(password)
			if hostID != "" {
				ctx.Logger.Info("Using provided host-id: %s", hostID)
				genCmd = fmt.Sprintf(
					"cd %s && yasboot config node gen -c %s -u %s -p %s --host-ids %s --port %d --install-path %s --data-path %s --log-path %s --begin-port %d --node %d",
					stageDir, clusterName, primaryUser, escapedPwd,
					hostID,
					ybSSHPort,
					installPath, dataPath, logPath,
					beginPort,
					nodeCount)
			} else {
				genCmd = fmt.Sprintf(
					"cd %s && yasboot config node gen -c %s -u %s -p %s --ip %s --port %d --install-path %s --data-path %s --log-path %s --begin-port %d --node %d",
					stageDir, clusterName, primaryUser, escapedPwd,
					targetsStr,
					ybSSHPort,
					installPath, dataPath, logPath,
					beginPort,
					nodeCount)
			}

			extra := ctx.GetParamString("yasboot_extra_args", "")
			genCmd = commonos.YasbootAppendExtraArgs(genCmd, extra, false)
			if strings.TrimSpace(extra) != "" {
				ctx.Logger.Info("yasboot config node gen: appending extra args: %s", strings.TrimSpace(extra))
			}

			// Run with primary env sourced.
			ctx.Logger.Info("Running: yasboot config node gen ...")
			result, err := commonos.ExecuteAsUserWithEnvCheck(ctx, primaryUser, envFile, genCmd, true)
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
						statusResult, statusErr := commonos.ExecuteAsUserWithEnvCheck(ctx, primaryUser, envFile, statusCmd, true)
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
													genCmd = fmt.Sprintf(
														"cd %s && yasboot config node gen -c %s -u %s -p %s --host-ids %s --port %d --install-path %s --data-path %s --log-path %s --begin-port %d --node %d",
														stageDir, clusterName, primaryUser, escapedPwd,
														hostIDFromStatus,
														ybSSHPort,
														installPath, dataPath, logPath,
														beginPort,
														nodeCount)
													genCmd = commonos.YasbootAppendExtraArgs(genCmd, ctx.GetParamString("yasboot_extra_args", ""), false)
													result, err = commonos.ExecuteAsUserWithEnvCheck(ctx, primaryUser, envFile, genCmd, true)
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

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Info("Command output:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}
				if strings.Contains(strings.ToLower(result.GetStdout()), "scan failed") {
					ctx.Logger.Warn("yasboot output contains scan failed: check standby SSH, ~/.yasboot leftovers, or yasboot --force hints; if E-012 fails next, it is often non-empty paths on standby - run yinstall clean on the standby with the same paths as install")
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
				clusterName = ctx.GetParamString("db_cluster_name", "yashandb")
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
