// standby_check_archive_mode.go - 检查主库归档模式
// 本步骤验证主库是否运行在归档模式，这是创建备库的前提条件

package standby

import (
	"fmt"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepCheckArchiveMode 检查主库归档模式步骤
func stepCheckArchiveMode() *runner.Step {
	return &runner.Step{
		Name:        "Check Archive Mode",
		Description: "Verify primary database is running in archive mode",
		Tags:        []string{"standby", "primary", "archive"},

		PreCheck: func(ctx *runner.StepContext) error {
			return checkArchiveMode(ctx)
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Check Archive Mode")
			return checkArchiveMode(ctx)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

// checkArchiveMode 只读：yasql 查 log_mode，要求 ARCHIVELOG。
func checkArchiveMode(ctx *runner.StepContext) error {
	if strings.TrimSpace(ctx.GetParamString("primary_env_file", "")) == "" &&
		strings.TrimSpace(ctx.GetParamString("db_cluster_name", "")) == "" {
		return fmt.Errorf("db_cluster_name is required unless primary_env_file is set")
	}

	standbyLogPhase(ctx, "check-start", "log_mode query")
	primaryUser := GetPrimaryOSUser(ctx)

	ctx.Logger.Info("Checking primary database archive mode")
	ctx.Logger.Info("  Primary user: %s", primaryUser)

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

	ctx.Logger.Info("Querying log_mode from v$database...")
	sql := "SELECT log_mode FROM v$database;"

	result, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, primaryUser, envFile, clusterName, sql, true)
	if err != nil {
		return fmt.Errorf("failed to query archive mode: %w", err)
	}

	ctx.Logger.Info("Query result:")
	ctx.Logger.Info("%s", result.Stdout)

	if !IsArchiveLogModeOutput(result.Stdout) {
		ctx.Logger.Error("================================================================")
		ctx.Logger.Error("ERROR: Primary database is NOT running in archive mode.")
		ctx.Logger.Error("Archive mode is REQUIRED for standby database creation.")
		ctx.Logger.Error("To enable archive mode on the primary:")
		ctx.Logger.Error("  1. Connect as SYS: yasql sys/<password>@%s", clusterName)
		ctx.Logger.Error("  2. SHUTDOWN IMMEDIATE;")
		ctx.Logger.Error("  3. STARTUP MOUNT;")
		ctx.Logger.Error("  4. ALTER DATABASE ARCHIVELOG;")
		ctx.Logger.Error("  5. ALTER DATABASE OPEN;")
		ctx.Logger.Error("  6. Verify: SELECT log_mode FROM v$database;")
		ctx.Logger.Error("================================================================")
		return fmt.Errorf("primary database is not in archive mode")
	}

	standbyLogPhase(ctx, "check-done", "log_mode=archivelog")
	ctx.Logger.Info("OK: Primary database is running in archive mode")
	return nil
}

// IsArchiveLogModeOutput 判断 yasql 输出是否为 ARCHIVELOG（排除 NOARCHIVELOG 子串误判）。
func IsArchiveLogModeOutput(stdout string) bool {
	mode := extractLogModeToken(stdout)
	if mode == "" {
		return false
	}
	return mode == "ARCHIVELOG"
}

// extractLogModeToken 从 SELECT log_mode 结果中提取 LOG_MODE 取值（大写）。
func extractLogModeToken(stdout string) string {
	var firstData string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "LOG_MODE") {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.Contains(upper, "ROW FETCHED") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		tok := strings.ToUpper(fields[0])
		if tok == "ARCHIVELOG" || tok == "NOARCHIVELOG" {
			return tok
		}
		if firstData == "" {
			firstData = tok
		}
	}
	upperAll := strings.ToUpper(stdout)
	if strings.Contains(upperAll, "NOARCHIVELOG") {
		return "NOARCHIVELOG"
	}
	if strings.Contains(upperAll, "ARCHIVELOG") {
		return "ARCHIVELOG"
	}
	return firstData
}
