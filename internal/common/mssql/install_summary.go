package mssql

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

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

// DisplaySAPassword returns SA password for terminal summary.
func DisplaySAPassword(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	pwd := strings.TrimSpace(ctx.GetParamString("mssql_sa_password", ""))
	if pwd == "" {
		return "(not configured)"
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

// SummaryOKLabel returns OK/FAIL for install summary health lines.
func SummaryOKLabel(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAIL"
}

// InstallSummaryTopology returns deployment topology label for summary.
func InstallSummaryTopology(ctx *runner.StepContext) string {
	if ctx == nil {
		return "standalone"
	}
	switch strings.TrimSpace(ctx.GetParamString("mssql_topology", "")) {
	case string(TopologyMirror):
		return "mirror"
	case string(TopologyAGWSFC):
		return "ag_wsfc"
	default:
		return "standalone"
	}
}

// SetupMediaLabel returns basename of resolved setup media for summary.
func SetupMediaLabel(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	for _, key := range []string{"mssql_setup_local_path", "mssql_setup_remote_path", "mssql_setup_root"} {
		if v, ok := ctx.Results[key].(string); ok {
			if label := mediaBasename(v); label != "" {
				return label
			}
		}
	}
	if pkg := strings.TrimSpace(ctx.GetParamString("mssql_setup_package", "")); pkg != "" {
		return mediaBasename(pkg)
	}
	return ""
}

func mediaBasename(ref string) string {
	ref = strings.TrimSpace(strings.ReplaceAll(ref, `\`, `/`))
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(ref), "\\\\") {
		parts := strings.Split(ref, "/")
		return parts[len(parts)-1]
	}
	return filepath.Base(ref)
}

// InstallSummaryProductLine formats product major and marketing year.
func InstallSummaryProductLine(ctx *runner.StepContext, layout Layout) string {
	major := layout.SetupProductMajor
	if ctx != nil && ctx.Results != nil {
		switch v := ctx.Results["mssql_setup_product_major"].(type) {
		case int:
			major = v
		case int64:
			major = int(v)
		}
	}
	if entry, ok := RegistryEntryFromContext(ctx); ok && entry.ProductMajor > 0 {
		major = entry.ProductMajor
	}
	if major == 0 {
		return ""
	}
	if year, ok := SQLReleaseYearFromMajor(major); ok {
		return fmt.Sprintf("major=%d (SQL Server %d)", major, year)
	}
	return fmt.Sprintf("major=%d", major)
}

// EnrichLayoutProgramPathsFromRegistry fills program/shared/instance dirs from registry SQLPath.
func EnrichLayoutProgramPathsFromRegistry(layout Layout, entry InstanceRegistryEntry) Layout {
	if strings.TrimSpace(entry.SQLPath) == "" {
		return layout
	}
	major := entry.ProductMajor
	if major == 0 {
		major = ProductMajorFromInternalID(entry.InternalID)
	}
	if layout.InstanceDir == "" {
		layout.InstanceDir = normalizeWinPath(filepath.Dir(strings.ReplaceAll(entry.SQLPath, `\`, `/`)))
	}
	if layout.ProgramDir == "" && layout.InstanceDir != "" {
		layout.ProgramDir = normalizeWinPath(filepath.Dir(strings.ReplaceAll(layout.InstanceDir, `\`, `/`)))
	}
	if layout.SharedDir == "" && layout.ProgramDir != "" && major > 0 {
		layout.SharedDir = DefaultSharedDirUnderProgram(layout.ProgramDir, major)
	}
	if layout.Base == "" {
		layout.Base = normalizeWinPath(entry.SQLPath)
	}
	return layout
}

// ProbeTCPPortListening reports whether a TCP port is listening on the target host.
func ProbeTCPPortListening(ctx *runner.StepContext, port int) bool {
	if ctx == nil || port <= 0 || ctx.DryRun || ctx.Precheck {
		return false
	}
	cmd := fmt.Sprintf(`powershell -NoProfile -Command "(Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction SilentlyContinue | Measure-Object).Count"`, port)
	res, err := ctx.Execute(cmd, false)
	if err != nil || res == nil {
		return false
	}
	n, _ := strconv.Atoi(strings.TrimSpace(res.GetStdout()))
	return n > 0
}

// QueryWindowsServiceStatus returns Windows service state by service name.
func QueryWindowsServiceStatus(ctx *runner.StepContext, serviceName string) (InstanceServiceStatus, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return InstanceServiceMissing, fmt.Errorf("empty service name")
	}
	return QueryInstanceServiceStatus(ctx, InstanceRegistryEntry{ServiceName: serviceName})
}

// SQLMaxServerMemoryInUseSQL returns configured max server memory (MB).
func SQLMaxServerMemoryInUseSQL() string {
	return `SELECT CAST(value_in_use AS NVARCHAR(20)) FROM sys.configurations WHERE name = 'max server memory (MB)';`
}

// MaxServerMemorySummaryMB returns configured max server memory for summary (result cache or sqlcmd).
func MaxServerMemorySummaryMB(ctx *runner.StepContext) (int, bool) {
	if ctx == nil {
		return 0, false
	}
	switch v := ctx.Results["mssql_max_server_memory_mb"].(type) {
	case int:
		if v > 0 {
			return v, true
		}
	case int64:
		if v > 0 {
			return int(v), true
		}
	}
	if ctx.DryRun || ctx.Precheck {
		return 0, false
	}
	stdout, err := QuerySqlcmdScalarOptional(ctx, "install summary max memory", SQLMaxServerMemoryInUseSQL())
	if err != nil {
		return 0, false
	}
	mb, err := strconv.Atoi(strings.TrimSpace(firstSqlcmdDataLine(stdout)))
	if err != nil || mb <= 0 {
		return 0, false
	}
	return mb, true
}

func firstSqlcmdDataLine(stdout string) string {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || IsSqlcmdMetaLine(line) {
			continue
		}
		return line
	}
	return ""
}

// SqlcmdVerifySucceeded reports whether MS-020 verify stored success in results.
func SqlcmdVerifySucceeded(ctx *runner.StepContext) bool {
	if ctx == nil || ctx.Results == nil {
		return false
	}
	v, ok := ctx.Results["mssql_version_running"].(string)
	return ok && strings.EqualFold(strings.TrimSpace(v), "verified")
}

// SqlcmdSAConnectionExample builds sqlcmd example using SA authentication.
func SqlcmdSAConnectionExample(ctx *runner.StepContext, remoteServer string) string {
	pwd := strings.TrimSpace(ctx.GetParamString("mssql_sa_password", ""))
	if pwd == "" {
		return fmt.Sprintf("sqlcmd -S %s -U sa -P <password>", remoteServer)
	}
	return fmt.Sprintf("sqlcmd -S %s -U sa -P %s", remoteServer, pwd)
}
