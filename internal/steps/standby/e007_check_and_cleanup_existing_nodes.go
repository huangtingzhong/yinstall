// e003a_check_and_cleanup_existing_nodes.go - 检查并清理已存在的节点
// 本步骤在主库执行，检查目标备库节点是否已经在集群中，如果存在则清理

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE003ACheckAndCleanupExistingNodes 检查并清理已存在的节点步骤
func StepE007CheckAndCleanupExistingNodes() *runner.Step {
	return &runner.Step{
		ID:          "E-007",
		Name:        "Check and Cleanup Existing Nodes",
		Description: "Check if standby targets already exist in cluster and cleanup if needed",
		Tags:        []string{"standby", "check", "cleanup"},

		PreCheck: func(ctx *runner.StepContext) error {
			// Always run this check
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			primaryUser := GetPrimaryOSUser(ctx)
			targets := ctx.GetParamStringSlice("standby_targets")
			if len(targets) == 0 {
				return fmt.Errorf("standby_targets is required")
			}

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
			} else {
				// primary_env_file is not specified, use db_cluster_name parameter
				clusterName = ctx.GetParamString("db_cluster_name", "yashandb")
				ctx.Logger.Info("Using cluster name from parameter: %s", clusterName)
			}

			ctx.Logger.Info("Checking if standby targets already exist in cluster: %s", clusterName)
			ctx.Logger.Info("  Standby targets: %v", targets)

			// Query cluster status to get existing hosts
			statusCmd := fmt.Sprintf("yasboot process yasagent status -c %s", clusterName)
			ctx.Logger.Info("Querying cluster status: %s", statusCmd)
			result, err := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, statusCmd, true)
			if err != nil {
				// If command fails, assume cluster doesn't exist or no nodes yet, continue
				ctx.Logger.Warn("Failed to query cluster status (cluster may not exist or empty): %v", err)
				ctx.Logger.Info("Assuming targets are not in cluster, continuing...")
				return nil
			}

			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Info("Cluster status query returned non-zero exit code, assuming targets are not in cluster")
				return nil
			}

			// Parse output to extract host IPs
			output := result.GetStdout()
			if output == "" {
				ctx.Logger.Info("Cluster status output is empty, assuming targets are not in cluster")
				return nil
			}

			ctx.Logger.Info("Cluster status output:\n%s", output)

			// Parse the table output to extract hostid and IP mapping
			// Format: | hostid   | pid    | run_user | listen_address    | ...
			// We need to extract both hostid and IP addresses
			ipToHostID := make(map[string]string) // Map IP to hostid
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				// Skip header and separator lines
				if line == "" || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "| hostid") {
					continue
				}
				// Parse table row: | hostid | pid | run_user | listen_address | ...
				if strings.HasPrefix(line, "|") {
					parts := strings.Split(line, "|")
					if len(parts) >= 5 {
						// hostid is the 1st column (index 1), listen_address is the 4th column (index 4)
						hostID := strings.TrimSpace(parts[1])
						listenAddr := strings.TrimSpace(parts[4])
						// Extract IP from listen_address (format: IP:PORT)
						if idx := strings.Index(listenAddr, ":"); idx > 0 {
							ip := listenAddr[:idx]
							if ip != "" && hostID != "" {
								ipToHostID[ip] = hostID
								ctx.Logger.Info("Found existing host in cluster: IP=%s, hostid=%s", ip, hostID)
							}
						}
					}
				}
			}

			// Check if any target IPs are already in the cluster
			targetsToCleanup := make(map[string]string) // Map target IP to hostid
			for _, target := range targets {
				target = strings.TrimSpace(target)
				if hostID, exists := ipToHostID[target]; exists {
					targetsToCleanup[target] = hostID
					ctx.Logger.Warn("Target %s already exists in cluster with hostid %s", target, hostID)
				}
			}

			if len(targetsToCleanup) == 0 {
				ctx.Logger.Info("All standby targets are not in cluster, no cleanup needed")
				return nil
			}

			ctx.Logger.Warn("Found %d target(s) already in cluster, removing them...", len(targetsToCleanup))

			// Remove each existing target using yasboot host remove
			for targetIP, hostID := range targetsToCleanup {
				ctx.Logger.Info("Removing host %s (hostid: %s) from cluster %s", targetIP, hostID, clusterName)
				cleanupCmd := fmt.Sprintf("yasboot host remove -c %s --host-ids %s -f", clusterName, hostID)
				cleanupResult, err := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, cleanupCmd, true)
				if err != nil {
					// Check if error is due to node already in removing state
					if cleanupResult != nil {
						stdout := cleanupResult.GetStdout()
						stderr := cleanupResult.GetStderr()
						output := stdout + stderr
						if strings.Contains(strings.ToLower(output), "in removing hosts") ||
							strings.Contains(strings.ToLower(output), "cannot remove it") {
							ctx.Logger.Warn("Host %s (hostid: %s) is already in removing state, skipping removal", targetIP, hostID)
							continue
						}
					}
					return fmt.Errorf("failed to remove host %s (hostid: %s): %w", targetIP, hostID, err)
				}

				if cleanupResult != nil && cleanupResult.GetStdout() != "" {
					ctx.Logger.Info("Host removal output for %s:", targetIP)
					for _, line := range strings.Split(cleanupResult.GetStdout(), "\n") {
						line = strings.TrimSpace(line)
						if line != "" {
							ctx.Logger.Info("  %s", line)
						}
					}
				}
				ctx.Logger.Info("Successfully removed host %s (hostid: %s)", targetIP, hostID)
			}

			ctx.Logger.Info("Cleanup completed successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			// Verify cleanup was successful by checking cluster status again
			primaryUser := GetPrimaryOSUser(ctx)
			targets := ctx.GetParamStringSlice("standby_targets")

			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				ctx.Logger.Warn("Failed to get primary environment file for post-check: %v", err)
				return nil // PostCheck is optional
			}

			var clusterName string
			specifiedEnvFile := ctx.GetParamString("primary_env_file", "")
			if specifiedEnvFile != "" {
				clusterName, _ = ExtractClusterNameFromEnvFile(ctx, envFile)
			}
			if clusterName == "" {
				clusterName = ctx.GetParamString("db_cluster_name", "yashandb")
			}

			statusCmd := fmt.Sprintf("yasboot process yasagent status -c %s", clusterName)
			result, err := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, statusCmd, true)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				// If query fails, assume cleanup was successful (cluster may be empty now)
				ctx.Logger.Info("Post-check: Cluster status query failed or empty, assuming cleanup successful")
				return nil
			}

			output := result.GetStdout()
			existingIPs := make(map[string]bool)
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "| hostid") {
					continue
				}
				if strings.HasPrefix(line, "|") {
					parts := strings.Split(line, "|")
					if len(parts) >= 5 {
						listenAddr := strings.TrimSpace(parts[4])
						if idx := strings.Index(listenAddr, ":"); idx > 0 {
							ip := listenAddr[:idx]
							if ip != "" {
								existingIPs[ip] = true
							}
						}
					}
				}
			}

			// Check if any targets are still in cluster
			for _, target := range targets {
				target = strings.TrimSpace(target)
				if existingIPs[target] {
					ctx.Logger.Warn("Post-check: Target %s still exists in cluster after cleanup", target)
					// Don't fail, just warn - cleanup may take time
				}
			}

			return nil
		},
	}
}
