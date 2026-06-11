package sql

import (
	"fmt"
	"strings"

	commonmysql "github.com/yinstall/internal/common/mysql"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// EscapeSQLString escapes single quotes for SQL string literals.
func EscapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// ExecuteMysqlSQL runs mysql client with optional password.
func ExecuteMysqlSQL(ctx *runner.StepContext, layout commonmysql.Layout, password, sql string) error {
	_, err := QueryMysqlSQL(ctx, layout, password, sql)
	return err
}

// QueryMysqlSQL executes SQL and returns stdout.
func QueryMysqlSQL(ctx *runner.StepContext, layout commonmysql.Layout, password, sql string) (string, error) {
	port := layout.Port
	socket := layout.Other + "/mysql.sock"
	platform := ctx.GetTargetPlatform()

	mysqlBin, err := commonmysql.ResolveMysqlToolBin(ctx, layout, "mysql")
	if err != nil {
		return "", err
	}
	mysqlBinQ := commonos.ShellSingleQuote(mysqlBin)
	socketQ := commonos.ShellSingleQuote(socket)

	var cmd string
	if platform == "windows" {
		hostPort := fmt.Sprintf("--host=127.0.0.1 --port=%d", port)
		sqlQ := commonos.PowerShellSingleQuote(powershellMysqlSQL(sql))
		if password != "" {
			cmd = fmt.Sprintf(`powershell -NoProfile -Command "$env:MYSQL_PWD='%s'; & %s %s -uroot -e %s"`,
				strings.ReplaceAll(password, `'`, `''`), mysqlBinQ, hostPort, sqlQ)
		} else {
			cmd = fmt.Sprintf(`powershell -NoProfile -Command "& %s %s -uroot -e %s"`, mysqlBinQ, hostPort, sqlQ)
		}
	} else if password != "" {
		sqlQ := commonos.ShellSingleQuote(sql)
		cmd = fmt.Sprintf("MYSQL_PWD=%s %s --no-defaults -S %s -uroot -e %s",
			commonos.ShellSingleQuote(password), mysqlBinQ, socketQ, sqlQ)
	} else {
		sqlQ := commonos.ShellSingleQuote(sql)
		cmd = fmt.Sprintf("%s --no-defaults -S %s -uroot -e %s", mysqlBinQ, socketQ, sqlQ)
	}
	res, err := ctx.ExecuteWithCheck(cmd, false)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.GetStdout()), nil
}

// ExecuteMysqlScript runs a SQL script on the target.
func ExecuteMysqlScript(ctx *runner.StepContext, layout commonmysql.Layout, password, scriptPath string) error {
	remoteScript := scriptPath
	platform := ctx.GetTargetPlatform()
	isAbs := strings.HasPrefix(scriptPath, "/") || (platform == "windows" && len(scriptPath) > 2 && scriptPath[1] == ':')
	if !isAbs {
		dest := "/tmp/yinstall_mysql_custom.sql"
		if platform == "windows" {
			dest = `C:/Windows/Temp/yinstall_mysql_custom.sql`
		}
		if err := ctx.Executor.Upload(scriptPath, dest, ctx.UploadContext()); err != nil {
			return err
		}
		remoteScript = dest
	}
	if platform == "windows" {
		homeWin := strings.ReplaceAll(layout.Home, `\`, `/`)
		scriptWin := strings.ReplaceAll(remoteScript, `\`, `/`)
		var auth string
		if password != "" {
			auth = fmt.Sprintf(`$env:MYSQL_PWD='%s'; `, strings.ReplaceAll(password, `'`, `''`))
		}
		hostPort := fmt.Sprintf("--host=127.0.0.1 --port=%d", layout.Port)
		eArg := commonos.PowerShellSingleQuote("source " + scriptWin + ";")
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "%s& '%s/bin/mysql.exe' %s -uroot --batch -e %s"`,
			auth, homeWin, hostPort, eArg)
		_, err := ctx.ExecuteWithCheck(cmd, false)
		return err
	}
	return ExecuteMysqlSQL(ctx, layout, password, fmt.Sprintf("source %s;", remoteScript))
}

// powershellMysqlSQL collapses SQL to one line for PowerShell -Command -e (newlines break quoting).
func powershellMysqlSQL(sql string) string {
	sql = strings.ReplaceAll(sql, "\r\n", " ")
	sql = strings.ReplaceAll(sql, "\n", " ")
	sql = strings.ReplaceAll(sql, "\r", " ")
	return strings.Join(strings.Fields(sql), " ")
}
