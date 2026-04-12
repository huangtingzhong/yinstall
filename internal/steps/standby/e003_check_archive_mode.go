// e003_check_archive_mode.go - 检查主库归档模式
// 本步骤验证主库是否运行在归档模式，这是创建备库的前提条件

package standby

import (
	"fmt"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// StepE001ACheckArchiveMode 检查主库归档模式步骤
func StepE003CheckArchiveMode() *runner.Step {
	return &runner.Step{
		ID:          "E-003",
		Name:        "Check Archive Mode",
		Description: "Verify primary database is running in archive mode",
		Tags:        []string{"standby", "primary", "archive"},

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

			ctx.Logger.Info("Checking primary database archive mode")
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

			// Query archive mode using yasql
			ctx.Logger.Info("Querying log_mode from v$database...")
			sql := "SELECT log_mode FROM v$database;"

			result, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, primaryUser, envFile, clusterName, sql, true)
			if err != nil {
				return fmt.Errorf("failed to query archive mode: %w", err)
			}

			ctx.Logger.Info("Query result:")
			ctx.Logger.Info("%s", result.Stdout)

			// Check if archive mode is enabled
			// Expected output contains: LOG_MODE | ARCHIVELOG
			isArchiveMode := false
			outputLower := strings.ToLower(result.Stdout)

			if strings.Contains(outputLower, "archivelog") {
				isArchiveMode = true
			}

			if !isArchiveMode {
				ctx.Logger.Error("╔════════════════════════════════════════════════════════════════╗")
				ctx.Logger.Error("║  ERROR: Primary database is NOT running in archive mode!      ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  Archive mode is REQUIRED for standby database creation.      ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  To enable archive mode on primary database:                  ║")
				ctx.Logger.Error("║  1. Connect to primary database as SYS:                       ║")
				ctx.Logger.Error("║     yasql sys/<password>@%s                         ║", clusterName)
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  2. Shutdown database:                                         ║")
				ctx.Logger.Error("║     SHUTDOWN IMMEDIATE;                                        ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  3. Start in mount mode:                                       ║")
				ctx.Logger.Error("║     STARTUP MOUNT;                                             ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  4. Enable archive mode:                                       ║")
				ctx.Logger.Error("║     ALTER DATABASE ARCHIVELOG;                                 ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  5. Open database:                                             ║")
				ctx.Logger.Error("║     ALTER DATABASE OPEN;                                       ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  6. Verify archive mode:                                       ║")
				ctx.Logger.Error("║     SELECT log_mode FROM v$database;                            ║")
				ctx.Logger.Error("╚════════════════════════════════════════════════════════════════╝")
				return fmt.Errorf("primary database is not in archive mode")
			}

			ctx.Logger.Info("✓ Primary database is running in archive mode")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
