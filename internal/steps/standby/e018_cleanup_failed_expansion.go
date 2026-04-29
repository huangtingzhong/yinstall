// e018_cleanup_failed_expansion.go - 清理失败扩容产物
// 本步骤为危险操作，用于清理失败的扩容产物

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE018CleanupFailedExpansion 清理失败扩容产物步骤
func StepE018CleanupFailedExpansion() *runner.Step {
	return &runner.Step{
		ID:          "E-018",
		Name:        "Cleanup Failed Expansion",
		Description: "Cleanup failed expansion artifacts (DANGEROUS)",
		Tags:        []string{"standby", "cleanup", "dangerous"},
		Dangerous:   true,
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			// This step requires explicit --force-steps E-018 (or global -F) or --standby-cleanup-on-failure
			cleanupOnFailure := ctx.GetParamBool("standby_cleanup_on_failure", false)
			isForce := ctx.IsForceStep()

			if !cleanupOnFailure && !isForce {
				return fmt.Errorf("cleanup step requires --force-steps E-018 (or global -F) or --standby-cleanup-on-failure flag")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			user := ctx.GetParamString("os_user", "yashan")
			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			homeDir, err := commonos.GetUserHomeDir(ctx, user)
			if err != nil {
				homeDir = fmt.Sprintf("/home/%s", user)
			}
			envFile := commonos.DetermineEnvFile(homeDir, beginPort)

			ctx.Logger.Info("WARNING: Starting cleanup of failed expansion")
			ctx.Logger.Info("This operation will remove standby nodes from the cluster")

			// Get cluster status to find node IDs
			result, err := commonos.ExecuteAsUserWithEnv(ctx, user, envFile, fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), true)
			if err != nil {
				return fmt.Errorf("failed to get cluster status: %w", err)
			}

			ctx.Logger.Info("Current cluster status:")
			for _, line := range strings.Split(result.GetStdout(), "\n") {
				if line != "" {
					ctx.Logger.Info("  %s", line)
				}
			}

			// Find standby node IDs (format: 1-2, 1-3, etc.)
			// This is a simplified implementation - in production, parse the actual output
			ctx.Logger.Info("To cleanup failed expansion, run the following command on primary:")
			ctx.Logger.Info("  yasboot node remove -c %s -n <node_id> --clean", clusterName)
			ctx.Logger.Info("")
			ctx.Logger.Info("Where <node_id> is the standby node ID (e.g., 1-2)")
			ctx.Logger.Info("")
			ctx.Logger.Info("After cleanup, you can retry the expansion by running yinstall standby again")

			// Note: Actual cleanup implementation would parse the cluster status
			// and execute yasboot node remove for each failed standby node
			// This is intentionally not automated to prevent accidental data loss

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			ctx.Logger.Info("Cleanup guidance provided")
			ctx.Logger.Info("Please verify cluster status manually before retrying expansion")
			return nil
		},
	}
}
