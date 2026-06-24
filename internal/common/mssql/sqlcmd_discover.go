package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// DiscoverSqlcmdPath locates sqlcmd.exe from resolved registry entry only.
func DiscoverSqlcmdPath(ctx *runner.StepContext) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("nil context")
	}
	if v, ok := ctx.Results["mssql_sqlcmd_path"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v), nil
	}
	if entry, ok := RegistryEntryFromContext(ctx); ok && strings.TrimSpace(entry.SqlcmdPath) != "" {
		ctx.SetResult("mssql_sqlcmd_path", entry.SqlcmdPath)
		return entry.SqlcmdPath, nil
	}
	if ctx.DryRun {
		ctx.SetResult("mssql_sqlcmd_path", "sqlcmd")
		return "sqlcmd", nil
	}
	entry, err := EnsureInstanceResolved(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(entry.SqlcmdPath) == "" {
		return "", fmt.Errorf(
			"sqlcmd not found in registry ClientSetup for SQL major %d (tools key %s); install SQL Server Client Tools",
			entry.ProductMajor, entry.ToolsRegKey,
		)
	}
	ctx.SetResult("mssql_sqlcmd_path", entry.SqlcmdPath)
	return entry.SqlcmdPath, nil
}
