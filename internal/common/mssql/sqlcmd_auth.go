package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	// SqlcmdAuthIntegrated uses sqlcmd -E (Windows integrated authentication).
	SqlcmdAuthIntegrated = "integrated"
	// SqlcmdAuthSQL uses sqlcmd -U sa -P.
	SqlcmdAuthSQL = "sql"
)

func sqlcmdAuthResultKey(ctx *runner.StepContext) string {
	return "mssql_sqlcmd_auth:" + TargetHost(ctx)
}

// SqlcmdAuthModeFromResults returns cached auth mode for the current target host.
func SqlcmdAuthModeFromResults(ctx *runner.StepContext) (string, bool) {
	if ctx == nil || ctx.Results == nil {
		return "", false
	}
	v, ok := ctx.Results[sqlcmdAuthResultKey(ctx)].(string)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

// UsesIntegratedSqlcmdAuth reports whether sqlcmd uses -E for the current host.
func UsesIntegratedSqlcmdAuth(ctx *runner.StepContext) bool {
	mode, ok := SqlcmdAuthModeFromResults(ctx)
	return ok && mode == SqlcmdAuthIntegrated
}

// DisplaySqlcmdAuth returns a human-readable auth mode for summaries.
func DisplaySqlcmdAuth(ctx *runner.StepContext) string {
	if UsesIntegratedSqlcmdAuth(ctx) {
		return "Windows Authentication (-E)"
	}
	return "SQL Server Authentication (sa)"
}

// SqlcmdConnectionExample returns a sample sqlcmd command for summaries.
func SqlcmdConnectionExample(ctx *runner.StepContext, server string) string {
	server = strings.TrimSpace(server)
	if server == "" {
		server = SqlcmdServerTarget("localhost", ResolvedListenPort(ctx))
	}
	if UsesIntegratedSqlcmdAuth(ctx) {
		return fmt.Sprintf(`sqlcmd -S %s -E`, server)
	}
	return fmt.Sprintf(`sqlcmd -S %s -U sa -P "<password>"`, server)
}

// PrepareSqlcmdSession discovers sqlcmd path and resolves auth mode (-E preferred).
func PrepareSqlcmdSession(ctx *runner.StepContext) error {
	if _, err := DiscoverSqlcmdPath(ctx); err != nil {
		return err
	}
	_, err := EnsureSqlcmdAuth(ctx)
	return err
}

// EnsureSqlcmdAuth probes sqlcmd auth: try -E first, then sa password if needed.
func EnsureSqlcmdAuth(ctx *runner.StepContext) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("nil context")
	}
	if mode, ok := SqlcmdAuthModeFromResults(ctx); ok {
		return mode, nil
	}
	if ctx.DryRun {
		storeSqlcmdAuth(ctx, SqlcmdAuthIntegrated)
		return SqlcmdAuthIntegrated, nil
	}
	if entry, ok := RegistryEntryFromContext(ctx); ok {
		if err := EnsureInstanceServiceRunning(ctx, entry); err != nil {
			return "", err
		}
	}
	if _, err := DiscoverSqlcmdPath(ctx); err != nil {
		return "", err
	}

	cmdE := sqlcmdProbeCommand(ctx, true)
	res, err := ctx.Execute(cmdE, false)
	if err == nil && res != nil && res.GetExitCode() == 0 {
		storeSqlcmdAuth(ctx, SqlcmdAuthIntegrated)
		logSqlcmdAuth(ctx, SqlcmdAuthIntegrated, "")
		return SqlcmdAuthIntegrated, nil
	}

	pwd := strings.TrimSpace(ctx.GetParamString("mssql_sa_password", ""))
	if pwd == "" {
		return "", fmt.Errorf(
			"sqlcmd Windows integrated auth failed on %s; provide --mssql-sa-password for SQL authentication",
			TargetHost(ctx),
		)
	}
	cmdSA := sqlcmdProbeCommand(ctx, false)
	res, err = ctx.Execute(cmdSA, false)
	if err != nil || res == nil || res.GetExitCode() != 0 {
		return "", fmt.Errorf(
			"sqlcmd auth failed on %s (tried -E and sa login)",
			TargetHost(ctx),
		)
	}
	storeSqlcmdAuth(ctx, SqlcmdAuthSQL)
	logSqlcmdAuth(ctx, SqlcmdAuthSQL, "sa")
	return SqlcmdAuthSQL, nil
}

func storeSqlcmdAuth(ctx *runner.StepContext, mode string) {
	if ctx != nil {
		ctx.SetResult(sqlcmdAuthResultKey(ctx), mode)
	}
}

func logSqlcmdAuth(ctx *runner.StepContext, mode, login string) {
	if ctx == nil || ctx.Logger == nil {
		return
	}
	host := TargetHost(ctx)
	if mode == SqlcmdAuthIntegrated {
		ctx.Logger.DebugWrite("INFO", fmt.Sprintf("phase=sqlcmd-auth host=%s mode=integrated (-E)", host))
		ctx.Logger.Info("sqlcmd auth on %s: Windows integrated (-E)", host)
		return
	}
	ctx.Logger.DebugWrite("INFO", fmt.Sprintf("phase=sqlcmd-auth host=%s mode=sql login=%s", host, login))
	ctx.Logger.Info("sqlcmd auth on %s: SQL login %s", host, login)
}

func sqlcmdProbeCommand(ctx *runner.StepContext, integrated bool) string {
	port := ResolvedListenPort(ctx)
	target := SqlcmdServerTarget("localhost", port)
	bin := SqlcmdBinary(ctx)
	auth := "-E"
	if !integrated {
		auth = authArgsForMode(ctx, SqlcmdAuthSQL)
	}
	return FormatSqlcmdRemote(bin, target, auth, `-Q 'SELECT 1' -b`)
}

func authArgsForMode(ctx *runner.StepContext, mode string) string {
	if mode == SqlcmdAuthSQL {
		pwd := strings.TrimSpace(ctx.GetParamString("mssql_sa_password", ""))
		pwd = strings.ReplaceAll(pwd, `'`, `''`)
		return fmt.Sprintf(`-U sa -P '%s'`, pwd)
	}
	return "-E"
}
