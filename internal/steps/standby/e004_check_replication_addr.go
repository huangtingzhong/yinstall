// e004_check_replication_addr.go - 检查主库 replication_addr 参数
// 本步骤验证主库是否配置了 replication_addr 参数，这是备库连接主库的必要配置

package standby

import (
	"fmt"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// StepE001BCheckReplicationAddr 检查主库 replication_addr 参数步骤
func StepE004CheckReplicationAddr() *runner.Step {
	return &runner.Step{
		ID:          "E-004",
		Name:        "Check Replication Address",
		Description: "Verify primary database has replication_addr configured",
		Tags:        []string{"standby", "primary", "replication"},

		PreCheck: func(ctx *runner.StepContext) error {
			if strings.TrimSpace(ctx.GetParamString("primary_env_file", "")) != "" {
				return nil
			}
			if strings.TrimSpace(ctx.GetParamString("db_cluster_name", "")) == "" {
				return fmt.Errorf("db_cluster_name is required unless primary_env_file is set")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			primaryUser := GetPrimaryOSUser(ctx)

			ctx.Logger.Info("Checking primary database REPLICATION_ADDR parameter")
			ctx.Logger.Info("  Primary user: %s", primaryUser)

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
			ctx.Logger.Info("  Cluster: %s", clusterName)

			// Query REPLICATION_ADDR using yasql
			ctx.Logger.Info("Querying REPLICATION_ADDR parameter...")
			sql := "SELECT name, value FROM v$parameter WHERE name = 'REPLICATION_ADDR';"

			result, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, primaryUser, envFile, clusterName, sql, true)
			if err != nil {
				return fmt.Errorf("failed to query REPLICATION_ADDR: %w", err)
			}

			ctx.Logger.Info("Query result:")
			ctx.Logger.Info("%s", result.Stdout)

			// Check if REPLICATION_ADDR is configured
			// Expected output format: REPLICATION_ADDR                                                 10.10.10.130:1889
			// or: REPLICATION_ADDR | 10.10.10.130:1889
			// Empty or NULL value means not configured
			isConfigured := false
			replicationAddr := ""

			lines := strings.Split(result.Stdout, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				// Skip header lines
				if strings.Contains(strings.ToLower(line), "name") && strings.Contains(strings.ToLower(line), "value") {
					continue
				}
				if strings.Contains(line, "---") {
					continue
				}
				// Parse value from output
				// Format 1: REPLICATION_ADDR | 192.168.1.100:1688 (with pipe separator)
				// Format 2: REPLICATION_ADDR                                                 10.10.10.130:1889 (space separated)
				if strings.Contains(strings.ToUpper(line), "REPLICATION_ADDR") {
					var value string
					// Try pipe separator first
					if strings.Contains(line, "|") {
						parts := strings.Split(line, "|")
						if len(parts) >= 2 {
							value = strings.TrimSpace(parts[1])
						}
					} else {
						// Try space-separated format
						// Find REPLICATION_ADDR and extract the value after it
						upperLine := strings.ToUpper(line)
						idx := strings.Index(upperLine, "REPLICATION_ADDR")
						if idx >= 0 {
							// Extract everything after REPLICATION_ADDR
							remaining := line[idx+len("REPLICATION_ADDR"):]
							// Remove leading/trailing whitespace and extract value
							remaining = strings.TrimSpace(remaining)
							// Value is the last non-empty field (after multiple spaces)
							fields := strings.Fields(remaining)
							if len(fields) > 0 {
								value = fields[len(fields)-1]
							}
						}
					}
					if value != "" && !strings.EqualFold(value, "null") && !strings.EqualFold(value, "none") {
						isConfigured = true
						replicationAddr = value
						break // Found it, no need to continue
					}
				}
			}

			if !isConfigured {
				ctx.Logger.Error("╔════════════════════════════════════════════════════════════════╗")
				ctx.Logger.Error("║  ERROR: REPLICATION_ADDR parameter is NOT configured!          ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  The REPLICATION_ADDR parameter is REQUIRED for standby        ║")
				ctx.Logger.Error("║  database to connect to primary database.                      ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  To configure REPLICATION_ADDR on primary database:            ║")
				ctx.Logger.Error("║  1. Connect to primary database as SYS:                       ║")
				ctx.Logger.Error("║     yasql sys/<password>@%s                         ║", clusterName)
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  2. Set REPLICATION_ADDR parameter:                            ║")
				ctx.Logger.Error("║     ALTER SYSTEM SET REPLICATION_ADDR = '<IP>:<PORT>';         ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║     Example:                                                   ║")
				ctx.Logger.Error("║     ALTER SYSTEM SET REPLICATION_ADDR = '192.168.1.100:1688';  ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  3. Verify configuration:                                      ║")
				ctx.Logger.Error("║     SELECT name, value FROM v$parameter                        ║")
				ctx.Logger.Error("║     WHERE name = 'REPLICATION_ADDR';                           ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  Note: Use the primary database's IP address and port          ║")
				ctx.Logger.Error("║        that standby can reach.                                 ║")
				ctx.Logger.Error("╚════════════════════════════════════════════════════════════════╝")
				return fmt.Errorf("REPLICATION_ADDR parameter is not configured")
			}

			ctx.Logger.Info("✓ Primary database REPLICATION_ADDR is configured: %s", replicationAddr)

			// Store REPLICATION_ADDR for later use
			ctx.SetResult("primary_replication_addr", replicationAddr)

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
