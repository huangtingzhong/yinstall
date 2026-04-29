// e010_check_and_cleanup_existing_nodes.go - 检查并清理已存在的节点
// 本步骤在主库执行，检查目标备库节点是否已经在集群中，如果存在则清理

package standby

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

const (
	hostRemoveWaitOnRemoving    = 5 * time.Second
	hostRemoveRemovingWaitCount = 3 // host remove 报 removing 后：每次 sleep 再查 yasagent/cluster 状态，共 3 次轮询
)

func yasbootHostRemoveOutputInRemoving(output string) bool {
	low := strings.ToLower(output)
	return strings.Contains(low, "in removing hosts") || strings.Contains(low, "cannot remove it")
}

// primaryClusterIPToHostID 在主库上合并查询 yasagent status 与 cluster status -d，得到 IP -> hostid。
func primaryClusterIPToHostID(ctx *runner.StepContext, primaryUser, envFile, clusterName string, logDetail bool) map[string]string {
	ipToHostID := make(map[string]string)
	agentCmd := fmt.Sprintf("yasboot process yasagent status -c %s", clusterName)
	if logDetail {
		ctx.Logger.Info("Querying yasagent status: %s", agentCmd)
	}
	agentRes, _ := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, primaryUser, envFile, agentCmd)
	if agentRes != nil && agentRes.GetExitCode() == 0 && strings.TrimSpace(agentRes.GetStdout()) != "" {
		if logDetail {
			ctx.Logger.Info("Yasagent status output:\n%s", agentRes.GetStdout())
		}
		for ip, hid := range parseYasagentStatusIPToHostID(agentRes.GetStdout()) {
			ipToHostID[ip] = hid
			if logDetail {
				ctx.Logger.Info("Found host from yasagent status: IP=%s, hostid=%s", ip, hid)
			}
		}
	} else if logDetail {
		exit := -1
		if agentRes != nil {
			exit = agentRes.GetExitCode()
		}
		ctx.Logger.Warn("Yasagent status empty or failed (exit=%d); will still check yasboot cluster status for stale hosts", exit)
	}

	clusterCmd := fmt.Sprintf("yasboot cluster status -c %s -d", clusterName)
	if logDetail {
		ctx.Logger.Info("Querying cluster status: %s", clusterCmd)
	}
	clusterRes, _ := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, primaryUser, envFile, clusterCmd)
	if clusterRes != nil && clusterRes.GetExitCode() == 0 && strings.TrimSpace(clusterRes.GetStdout()) != "" {
		if logDetail {
			ctx.Logger.Info("Cluster status output:\n%s", clusterRes.GetStdout())
		}
		for ip, hid := range parseClusterStatusIPToHostID(clusterRes.GetStdout()) {
			if logDetail {
				if _, ok := ipToHostID[ip]; !ok {
					ctx.Logger.Info("Found host from cluster status (yasagent may be gone on host): IP=%s, hostid=%s", ip, hid)
				}
			}
			ipToHostID[ip] = hid
		}
	} else if logDetail {
		exit := -1
		if clusterRes != nil {
			exit = clusterRes.GetExitCode()
		}
		ctx.Logger.Warn("Cluster status empty or failed (exit=%d)", exit)
	}
	return ipToHostID
}

// parseYasagentStatusIPToHostID parses `yasboot process yasagent status` table (listen in column ~4).
func parseYasagentStatusIPToHostID(output string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "| hostid") {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		hostID := strings.TrimSpace(parts[1])
		listenAddr := strings.TrimSpace(parts[4])
		if idx := strings.Index(listenAddr, ":"); idx > 0 {
			ip := listenAddr[:idx]
			if ip != "" && hostID != "" {
				out[ip] = hostID
			}
		}
	}
	return out
}

// parseClusterStatusIPToHostID parses `yasboot cluster status -d` table (listen_address / listen_addr column).
// 仅备机 yasboot clean 后，yasagent status 可能已无该节点，但集群元数据仍在 cluster status 中，必须据此做 host remove。
func parseClusterStatusIPToHostID(output string) map[string]string {
	out := make(map[string]string)
	hostCol, listenCol := -1, -1
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "+") {
			continue
		}
		parts := strings.Split(line, "|")
		fields := make([]string, 0, len(parts))
		for _, p := range parts {
			fields = append(fields, strings.TrimSpace(p))
		}
		if hostCol < 0 || listenCol < 0 {
			for i, f := range fields {
				if f == "hostid" {
					hostCol = i
				}
				if f == "listen_address" || f == "listen_addr" {
					listenCol = i
				}
			}
			if hostCol >= 0 && listenCol >= 0 {
				continue
			}
		}
		if hostCol < 0 || listenCol < 0 || len(fields) <= listenCol || len(fields) <= hostCol {
			continue
		}
		hostID := fields[hostCol]
		if hostID == "" || strings.EqualFold(hostID, "hostid") {
			continue
		}
		listen := fields[listenCol]
		if idx := strings.Index(listen, ":"); idx > 0 {
			out[listen[:idx]] = hostID
		}
	}
	return out
}

// StepE010CheckAndCleanupExistingNodes 检查并清理已存在的节点步骤
func StepE010CheckAndCleanupExistingNodes() *runner.Step {
	return &runner.Step{
		ID:          "E-010",
		Name:        "Check and Cleanup Existing Nodes",
		Description: "Check if standby targets already exist in cluster and cleanup if needed",
		Tags:        []string{"standby", "check", "cleanup"},

		PreCheck: func(ctx *runner.StepContext) error {
			// Read-only: detect whether targets already exist in cluster and report.
			targets := ctx.GetParamStringSlice("standby_targets")
			if len(targets) == 0 {
				return fmt.Errorf("standby_targets is required")
			}
			primaryUser := GetPrimaryOSUser(ctx)
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return err
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			ipToHostID := primaryClusterIPToHostID(ctx, primaryUser, envFile, clusterName, false)
			for _, target := range targets {
				target = strings.TrimSpace(target)
				if target == "" {
					continue
				}
				if hostID, exists := ipToHostID[target]; exists {
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepID:      "E-010",
						StepName:    "Check and Cleanup Existing Nodes",
						Host:        ctx.Executor.Host(),
						Severity:    runner.PrecheckSeverityWarn,
						Code:        "PC.STANDBY.EXISTS_IN_CLUSTER",
						Message:     fmt.Sprintf("standby target already exists in cluster: %s (hostid=%s); apply will run 'yasboot host remove' to clean it up", target, hostID),
						Remediation: "confirm this host should be re-added as standby; otherwise adjust --targets/standby_targets",
					})
				}
			}
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
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return err
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			ctx.Logger.Info("Checking if standby targets already exist in cluster: %s", clusterName)
			ctx.Logger.Info("  Standby targets: %v", targets)

			ipToHostID := primaryClusterIPToHostID(ctx, primaryUser, envFile, clusterName, true)

			if len(ipToHostID) == 0 {
				ctx.Logger.Info("No host IP mapping from yasagent/cluster status, no cleanup needed")
				return nil
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

				cleanupResult, err := runYasbootOnPrimaryWithEnvFile(ctx, primaryUser, envFile, cleanupCmd)
				if err != nil {
					output := ""
					if cleanupResult != nil {
						output = YasbootCombinedOutput(cleanupResult.GetStdout(), cleanupResult.GetStderr())
					}
					if !yasbootHostRemoveOutputInRemoving(output) {
						return fmt.Errorf("failed to remove host %s (hostid: %s): %w", targetIP, hostID, err)
					}
					// 仅一次 host remove；处于 removing 时通过轮询 yasagent/cluster 状态确认目标是否已从集群视图中消失
					removedByAsync := false
					for waitRound := 0; waitRound < hostRemoveRemovingWaitCount; waitRound++ {
						ctx.Logger.Warn("host remove reports node in removing state; after %v re-querying yasagent/cluster to see if target still exists (%d/%d). yasboot: %s",
							hostRemoveWaitOnRemoving, waitRound+1, hostRemoveRemovingWaitCount, strings.TrimSpace(output))
						time.Sleep(hostRemoveWaitOnRemoving)
						refreshed := primaryClusterIPToHostID(ctx, primaryUser, envFile, clusterName, false)
						if _, still := refreshed[targetIP]; !still {
							ctx.Logger.Info("after re-query, target IP %s is no longer in cluster IP map - treating async removal as done; continuing", targetIP)
							removedByAsync = true
							err = nil
							break
						}
						ctx.Logger.Warn("after re-query, target %s is still in cluster (hostid=%s)", targetIP, refreshed[targetIP])
					}
					if !removedByAsync {
						ctx.Logger.Error("host remove still in removing state after %d waits and re-queries; target still in cluster: %s (hostid=%s)",
							hostRemoveRemovingWaitCount, targetIP, hostID)
						return fmt.Errorf("host remove failed for %s (%s): still in removing or still listed in cluster after %d polls (%s each): %w; output: %s",
							targetIP, hostID, hostRemoveRemovingWaitCount, hostRemoveWaitOnRemoving, err, strings.TrimSpace(output))
					}
				}

				if err == nil && cleanupResult != nil && cleanupResult.GetExitCode() == 0 && strings.TrimSpace(cleanupResult.GetStdout()) != "" {
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
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				ctx.Logger.Warn("Post-check: sync cluster name from env file: %v", err)
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			existingIPs := make(map[string]bool)
			agentRes, _ := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, primaryUser, envFile, fmt.Sprintf("yasboot process yasagent status -c %s", clusterName))
			if agentRes != nil && agentRes.GetExitCode() == 0 {
				for ip := range parseYasagentStatusIPToHostID(agentRes.GetStdout()) {
					existingIPs[ip] = true
				}
			}
			clusterRes, _ := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, primaryUser, envFile, fmt.Sprintf("yasboot cluster status -c %s -d", clusterName))
			if clusterRes != nil && clusterRes.GetExitCode() == 0 {
				for ip := range parseClusterStatusIPToHostID(clusterRes.GetStdout()) {
					existingIPs[ip] = true
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
