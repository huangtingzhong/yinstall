package mssql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

// RunSqlcmdQueries prepares the sqlcmd session and runs each query with ExecuteWithCheck.
// DryRun skips execution but still logs the command preview.
func RunSqlcmdQueries(ctx *runner.StepContext, label string, queries []string) error {
	if ctx == nil {
		return nil
	}
	if err := PrepareSqlcmdSession(ctx); err != nil {
		return err
	}
	for _, q := range queries {
		cmd := SqlcmdQueryCommand(ctx, q)
		ctx.LogScriptPreview("sqlcmd", label, cmd)
		if ctx.DryRun {
			continue
		}
		if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
			return err
		}
	}
	return nil
}

// QuerySqlcmdScalar runs a query and returns the trimmed stdout.
// Returns empty string in DryRun; errors when Precheck is set (no real query).
func QuerySqlcmdScalar(ctx *runner.StepContext, label, query string) (string, error) {
	if ctx == nil {
		return "", nil
	}
	if ctx.DryRun {
		return "", nil
	}
	if err := PrepareSqlcmdSession(ctx); err != nil {
		return "", err
	}
	cmd := SqlcmdQueryCommand(ctx, query)
	ctx.LogScriptPreview("sqlcmd", label, cmd)
	res, err := ctx.ExecuteWithCheck(cmd, false)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", fmt.Errorf("%s: empty sqlcmd result", label)
	}
	return res.GetStdout(), nil
}

// QuerySqlcmdScalarOptional is like QuerySqlcmdScalar but returns empty string
// without error in DryRun/Precheck modes (used by PreCheck phases that should
// not fail when the SQL session is not yet established).
func QuerySqlcmdScalarOptional(ctx *runner.StepContext, label, query string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("%s: nil context", label)
	}
	if ctx.DryRun || ctx.Precheck {
		return "", nil
	}
	if err := PrepareSqlcmdSession(ctx); err != nil {
		return "", err
	}
	cmd := SqlcmdQueryCommand(ctx, query)
	ctx.LogScriptPreview("sqlcmd", label, cmd)
	res, err := ctx.Execute(cmd, false)
	if err != nil {
		return "", err
	}
	if res == nil || res.GetExitCode() != 0 {
		return "", fmt.Errorf("%s: sqlcmd failed", label)
	}
	return res.GetStdout(), nil
}

// ResolveSQLMajor returns the SQL Server major version (e.g. 14 for SQL 2019).
// Uses the cached value in ctx when available; otherwise queries SERVERPROPERTY.
func ResolveSQLMajor(ctx *runner.StepContext) (int, error) {
	if ctx == nil {
		return 0, nil
	}
	if m := SQLMajorFromContext(ctx); m > 0 && m != 14 {
		return m, nil
	}
	stdout, err := QuerySqlcmdScalar(ctx, "sql major version", "SELECT CAST(SERVERPROPERTY('ProductMajorVersion') AS INT);")
	if err != nil {
		return SQLMajorFromContext(ctx), err
	}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || IsSqlcmdMetaLine(line) {
			continue
		}
		var major int
		if major, err = strconv.Atoi(line); err == nil && major > 0 {
			return major, nil
		}
	}
	return SQLMajorFromContext(ctx), nil
}

// SqlcmdScalarIsOne reports whether the sqlcmd stdout contains a "1" scalar
// (after filtering metadata separator lines).
func SqlcmdScalarIsOne(stdout string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if IsSqlcmdMetaLine(line) {
			continue
		}
		if line == "1" {
			return true
		}
	}
	return false
}
