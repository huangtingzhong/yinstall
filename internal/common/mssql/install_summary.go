package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
)

// UserDatabaseInfo is one user database row for install summary.
type UserDatabaseInfo struct {
	Name          string
	State         string
	RecoveryModel string
	DataFile      string
}

// RemoteSQLServerAddress formats host,port for client connection (always prefers TCP port when known).
func RemoteSQLServerAddress(host string, port int, instance string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "localhost"
	}
	if port > 0 {
		return fmt.Sprintf("%s,%d", host, port)
	}
	instance = strings.TrimSpace(instance)
	if instance != "" && !strings.EqualFold(instance, DefaultInstance) {
		return host + `\` + instance
	}
	return host
}

// DisplaySAPassword returns SA password for terminal summary (respects --log-redact).
func DisplaySAPassword(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	pwd := strings.TrimSpace(ctx.GetParamString("mssql_sa_password", ""))
	if pwd == "" {
		return "(not configured)"
	}
	if logging.RedactSensitive() {
		return "***REDACTED***"
	}
	return pwd
}

// UserDatabaseListSQL lists user databases (name, state, recovery model).
func UserDatabaseListSQL() string {
	return `
SELECT CONCAT(
  CAST(d.name AS NVARCHAR(128)) COLLATE Latin1_General_CI_AS, N'|',
  CAST(d.state_desc AS NVARCHAR(64)) COLLATE Latin1_General_CI_AS, N'|',
  CAST(d.recovery_model_desc AS NVARCHAR(64)) COLLATE Latin1_General_CI_AS
)
FROM sys.databases d
WHERE d.database_id > 4
ORDER BY d.name;`
}

// SQLProductVersionSQL returns ProductVersion|Edition|ProductLevel.
func SQLProductVersionSQL() string {
	return `SELECT CONCAT(
  RTRIM(CAST(SERVERPROPERTY('ProductVersion') AS NVARCHAR(128))), N'|',
  RTRIM(CAST(SERVERPROPERTY('Edition') AS NVARCHAR(128))), N'|',
  RTRIM(CAST(SERVERPROPERTY('ProductLevel') AS NVARCHAR(128)))
);`
}

// ParseUserDatabaseList parses UserDatabaseListSQL output.
func ParseUserDatabaseList(stdout string) []UserDatabaseInfo {
	var out []UserDatabaseInfo
	parsePipeRows(stdout, 3, func(p []string) {
		out = append(out, UserDatabaseInfo{
			Name:          strings.TrimSpace(p[0]),
			State:         strings.TrimSpace(p[1]),
			RecoveryModel: strings.TrimSpace(p[2]),
		})
	})
	return out
}

// ParseSQLProductVersion parses SQLProductVersionSQL output.
func ParseSQLProductVersion(stdout string) (version, edition, level string) {
	parsePipeRows(stdout, 3, func(p []string) {
		version = strings.TrimSpace(p[0])
		edition = strings.TrimSpace(p[1])
		level = strings.TrimSpace(p[2])
	})
	return version, edition, level
}

// TargetHost returns executor host or localhost.
func TargetHost(ctx *runner.StepContext) string {
	if ctx != nil && ctx.Executor != nil {
		if h := strings.TrimSpace(ctx.Executor.Host()); h != "" {
			return h
		}
	}
	return "localhost"
}

// InstanceProfilePathFromResults reads MS-019 profile path.
func InstanceProfilePathFromResults(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Results["mssql_instance_env_path"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
