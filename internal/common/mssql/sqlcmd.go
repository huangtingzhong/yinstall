package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// SqlcmdBinary returns sqlcmd executable path from resolved registry entry or Results.
func SqlcmdBinary(ctx *runner.StepContext) string {
	if ctx != nil {
		if entry, ok := RegistryEntryFromContext(ctx); ok && strings.TrimSpace(entry.SqlcmdPath) != "" {
			return entry.SqlcmdPath
		}
		if v, ok := ctx.Results["mssql_sqlcmd_path"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "sqlcmd"
}

// SqlcmdServerTarget returns -S target using TCP port only.
func SqlcmdServerTarget(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "localhost"
	}
	if port > 0 {
		return fmt.Sprintf("%s,%d", host, port)
	}
	return host
}

// SqlcmdServer returns -S server string for instance (legacy summaries).
func SqlcmdServer(instance string) string {
	instance = strings.TrimSpace(instance)
	if instance == "" || strings.EqualFold(instance, DefaultInstance) {
		return "localhost"
	}
	return `localhost\` + instance
}

// SqlcmdCommand builds a WinRM-safe sqlcmd command (PowerShell & for paths with spaces).
func SqlcmdCommand(ctx *runner.StepContext, args string) string {
	port := ResolvedListenPort(ctx)
	target := SqlcmdServerTarget("localhost", port)
	auth := SqlcmdAuthArgs(ctx)
	return FormatSqlcmdRemote(SqlcmdBinary(ctx), target, auth, strings.TrimSpace(args))
}

// FormatSqlcmdRemote invokes sqlcmd via PowerShell so D:\Program Files\... works over WinRM.
func FormatSqlcmdRemote(bin, target, auth, args string) string {
	binLit := strings.ReplaceAll(normalizeWinPath(strings.TrimSpace(bin)), `'`, `''`)
	inner := strings.TrimSpace(fmt.Sprintf("& '%s' -S %s %s %s", binLit, target, strings.TrimSpace(auth), strings.TrimSpace(args)))
	inner = strings.ReplaceAll(inner, `"`, `\"`)
	return `powershell -NoProfile -Command "` + inner + `"`
}

// SqlcmdAuthArgs returns -E or -U sa -P based on EnsureSqlcmdAuth probe result.
func SqlcmdAuthArgs(ctx *runner.StepContext) string {
	if mode, ok := SqlcmdAuthModeFromResults(ctx); ok {
		return authArgsForMode(ctx, mode)
	}
	return "-E"
}

// SqlcmdQueryCommand builds sqlcmd -Q 'query' -b (single-line query).
func SqlcmdQueryCommand(ctx *runner.StepContext, query string) string {
	q := strings.ReplaceAll(flattenSQL(query), `'`, `''`)
	return SqlcmdCommand(ctx, fmt.Sprintf(`-Q '%s' -b`, q))
}

// flattenSQL collapses multiline T-SQL into one line for sqlcmd -Q over SSH.
func flattenSQL(query string) string {
	lines := strings.Split(query, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}

// SqlcmdInputFileCommand builds sqlcmd -i path -b.
func SqlcmdInputFileCommand(ctx *runner.StepContext, inputPath string) string {
	p := strings.ReplaceAll(normalizeWinPath(inputPath), `'`, `''`)
	return SqlcmdCommand(ctx, fmt.Sprintf(`-i '%s' -b`, p))
}
