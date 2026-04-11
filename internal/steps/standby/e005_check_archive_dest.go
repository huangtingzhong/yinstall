// e002a_check_archive_dest.go - 检查归档路径是否已包含目标端
// 本步骤检查主库的归档路径配置，如果已包含目标端IP，说明备库已配置，直接报错退出

package standby

import (
	"fmt"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// StepE002ACheckArchiveDest 检查归档路径是否已包含目标端步骤
func StepE005CheckArchiveDest() *runner.Step {
	return &runner.Step{
		ID:          "E-005",
		Name:        "Check Archive Destination",
		Description: "Check if archive destination already contains standby target IPs",
		Tags:        []string{"standby", "archive", "check"},

		PreCheck: func(ctx *runner.StepContext) error {
			targets := ctx.GetParamStringSlice("standby_targets")
			if len(targets) == 0 {
				return fmt.Errorf("standby_targets is required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			primaryUser := GetPrimaryOSUser(ctx)
			targets := ctx.GetParamStringSlice("standby_targets")

			ctx.Logger.Info("Checking if archive destination already contains standby targets")
			ctx.Logger.Info("  Standby targets: %v", targets)
			ctx.Logger.Info("  Primary user: %s", primaryUser)

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

			ctx.Logger.Info("  Cluster: %s", clusterName)

			// Query archive destination parameters for each target IP
			// Use LIKE '%ip%' to check if archive_dest parameters contain the target IP
			foundTargets := []string{}
			allArchiveDests := []string{}

			for _, target := range targets {
				target = strings.TrimSpace(target)
				if target == "" {
					continue
				}

				ctx.Logger.Info("Checking if archive destination contains target IP: %s", target)
				// Query archive_dest parameters that contain this target IP
				sql := fmt.Sprintf("SELECT name, value FROM v$parameter WHERE name LIKE 'ARCHIVE_DEST%%' AND value LIKE '%%%s%%';", target)

				result, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, primaryUser, envFile, clusterName, sql, true)
				if err != nil {
					ctx.Logger.Warn("Failed to query archive destination for target %s: %v", target, err)
					continue
				}

				if result == nil || result.Stdout == "" {
					ctx.Logger.Info("No archive destination found containing target IP: %s", target)
					continue
				}

				ctx.Logger.Info("Archive destination query result for target %s:", target)
				ctx.Logger.Info("%s", result.Stdout)

				// Parse archive destinations from output
				lines := strings.Split(result.Stdout, "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					// Skip header lines
					if strings.Contains(strings.ToLower(line), "name") ||
						strings.Contains(strings.ToLower(line), "value") ||
						strings.Contains(line, "---") ||
						strings.Contains(line, "====") {
						continue
					}
					// Parse value from output like: ARCHIVE_DEST_1 | SERVICE=standby1@10.10.10.135:1688
					parts := strings.Split(line, "|")
					if len(parts) >= 2 {
						paramName := strings.TrimSpace(parts[0])
						value := strings.TrimSpace(parts[1])
						if value != "" && !strings.EqualFold(value, "null") && !strings.EqualFold(value, "none") {
							// Found archive destination containing this target IP
							foundTargets = append(foundTargets, target)
							archiveDest := fmt.Sprintf("%s = %s", paramName, value)
							allArchiveDests = append(allArchiveDests, archiveDest)
							ctx.Logger.Error("Found target IP %s in archive destination: %s = %s", target, paramName, value)
							break // Found for this target, no need to check more lines
						}
					}
				}
			}

			if len(foundTargets) > 0 {
				ctx.Logger.Error("╔════════════════════════════════════════════════════════════════╗")
				ctx.Logger.Error("║  ERROR: Standby targets already configured in archive destination! ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  The following standby target(s) are already configured:      ║")
				for _, target := range foundTargets {
					ctx.Logger.Error("║    - %s", target)
				}
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  Archive destination configuration:                            ║")
				for _, dest := range allArchiveDests {
					ctx.Logger.Error("║    %s", dest)
				}
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  Action required:                                                ║")
				ctx.Logger.Error("║  1. If you want to reconfigure standby, first remove the        ║")
				ctx.Logger.Error("║     existing archive destination configuration.                 ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  2. Or use a different standby target IP address.              ║")
				ctx.Logger.Error("╚════════════════════════════════════════════════════════════════╝")
				return fmt.Errorf("standby targets %v already configured in archive destination", foundTargets)
			}

			ctx.Logger.Info("✓ No standby targets found in archive destination configuration")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
