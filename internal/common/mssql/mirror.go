package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	// MirrorWorkSubdir under SQL default backup directory.
	MirrorWorkSubdir = "yinstall_mirror"
	MirrorCertSub    = "certs"
)

// DefaultMirrorCertValidDays is default mirror certificate validity (1 year).
const DefaultMirrorCertValidDays = 365

// HAMode constants for --ha-mode.
const (
	HAModeMirror = "mirror"
	HAModeAG     = "ag"
)

// NormalizeHAMode returns mirror or ag.
func NormalizeHAMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case HAModeAG, "alwayson", "always_on":
		return HAModeAG
	default:
		return HAModeMirror
	}
}

// MirrorDBNamesParam returns explicit --mirror-db values (comma-separated; empty = all user DBs on primary).
func MirrorDBNamesParam(ctx *runner.StepContext) []string {
	if ctx == nil {
		return nil
	}
	return ParseMirrorDBNames(ctx.GetParamString("mssql_mirror_db", ""))
}

// MirrorDBNameParam returns first explicit --mirror-db value, or empty.
func MirrorDBNameParam(ctx *runner.StepContext) string {
	dbs := MirrorDBNamesParam(ctx)
	if len(dbs) == 0 {
		return ""
	}
	return dbs[0]
}

// MirrorDBName returns first mirror database from params (legacy).
func MirrorDBName(ctx *runner.StepContext) string {
	return MirrorDBNameParam(ctx)
}

// MirrorDBListResultKey is Results key for resolved mirror target database names.
func MirrorDBListResultKey() string {
	return "mirror_db_list"
}

// MirrorEndpointPort returns mirroring endpoint port for the current host.
func MirrorEndpointPort(ctx *runner.StepContext) int {
	return LocalHAEndpointPort(ctx)
}

// DiscoverMirrorWorkDirSQL returns backup directory via xp_instance_regread.
func DiscoverMirrorWorkDirSQL() string {
	return `DECLARE @b NVARCHAR(260); EXEC master.dbo.xp_instance_regread N'HKEY_LOCAL_MACHINE', N'Software\Microsoft\MSSQLServer\MSSQLServer', N'BackupDirectory', @b OUTPUT; SELECT @b AS backup_dir;`
}

// JoinWinPath joins Windows path segments.
func JoinWinPath(base, name string) string {
	return joinWinPath(base, name)
}

// MirrorWorkDirForHost returns mirror work root for a specific host (from Results or registry).
func MirrorWorkDirForHost(ctx *runner.StepContext, hostKey string) string {
	hostKey = strings.TrimSpace(hostKey)
	if hostKey != "" {
		if v, ok := ctx.Results[mirrorWorkDirResultKey(hostKey)].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimRight(strings.TrimSpace(v), `\`)
		}
	}
	if ctx != nil && ctx.Results != nil && hostKey != "" {
		if entry, ok := ctx.Results[RegistryEntryResultKey(hostKey)].(InstanceRegistryEntry); ok {
			layout := LayoutFromRegistryEntry(entry)
			if layout.BackupDir != "" {
				return joinWinPath(layout.BackupDir, MirrorWorkSubdir)
			}
		}
	}
	self := ""
	if ctx != nil && ctx.Executor != nil {
		self = strings.TrimSpace(ctx.Executor.Host())
	}
	if hostKey == "" || strings.EqualFold(hostKey, self) {
		return mirrorWorkDirLocal(ctx)
	}
	return defaultMirrorWorkDirFallback()
}

func mirrorWorkDirResultKey(hostKey string) string {
	return "mirror_work_dir_" + MirrorHostKey(hostKey)
}

// MirrorCertDirForHost returns cert dir under layoutHost's backup work dir.
func MirrorCertDirForHost(ctx *runner.StepContext, layoutHost string) string {
	return joinWinPath(MirrorWorkDirForHost(ctx, layoutHost), MirrorCertSub)
}

// MirrorCertFileForHost returns cert file path on layoutHost (certHostKey in filename).
func MirrorCertFileForHost(ctx *runner.StepContext, layoutHost, certHostKey string) string {
	return mirrorCertFilePath(ctx, layoutHost, certHostKey)
}

// MirrorWorkDir returns mirror work root on the current executor host.
func MirrorWorkDir(ctx *runner.StepContext) string {
	if ctx != nil && ctx.Executor != nil {
		return MirrorWorkDirForHost(ctx, ctx.Executor.Host())
	}
	return mirrorWorkDirLocal(ctx)
}

func mirrorWorkDirLocal(ctx *runner.StepContext) string {
	if ctx != nil {
		if v := strings.TrimSpace(ctx.GetParamString("mirror_work_dir", "")); v != "" {
			return strings.TrimRight(v, `\`)
		}
		if ctx.Executor != nil {
			hostKey := MirrorHostKey(ctx.Executor.Host())
			if v, ok := ctx.Results[mirrorWorkDirResultKey(hostKey)].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimRight(strings.TrimSpace(v), `\`)
			}
			if entry, ok := RegistryEntryFromContext(ctx); ok {
				layout := LayoutFromRegistryEntry(entry)
				if layout.BackupDir != "" {
					return joinWinPath(layout.BackupDir, MirrorWorkSubdir)
				}
			}
		}
	}
	return defaultMirrorWorkDirFallback()
}

func defaultMirrorWorkDirFallback() string {
	return joinWinPath(`C:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER\MSSQL\Backup`, MirrorWorkSubdir)
}

// MirrorCertDir returns cert export directory on the current host.
func MirrorCertDir(ctx *runner.StepContext) string {
	return MirrorCertDirForHost(ctx, "")
}

// MirrorBackupPathForHost returns backup file path using hostKey's work directory layout.
func MirrorBackupPathForHost(ctx *runner.StepContext, hostKey, dbName string) string {
	return MirrorNewBackupPathForHost(ctx, hostKey, dbName, MirrorBackupTimestamp())
}

// MirrorBackupPath returns full backup file path for a database on the current host.
func MirrorBackupPath(ctx *runner.StepContext, dbName string) string {
	return MirrorNewBackupPath(ctx, dbName, MirrorBackupTimestamp())
}

// MirrorCertFile returns local cert file path (current host layout, certHostKey in filename).
func MirrorCertFile(ctx *runner.StepContext, certHostKey string) string {
	layoutHost := ""
	if ctx != nil && ctx.Executor != nil {
		layoutHost = ctx.Executor.Host()
	}
	return mirrorCertFilePath(ctx, layoutHost, certHostKey)
}

func mirrorCertFilePath(ctx *runner.StepContext, layoutHost, certHostKey string) string {
	key := strings.ReplaceAll(strings.TrimSpace(certHostKey), ":", "_")
	key = strings.ReplaceAll(key, `\`, "_")
	key = strings.ReplaceAll(key, ".", "_")
	return joinWinPath(MirrorCertDirForHost(ctx, layoutHost), key+".cer")
}

// MirrorPartnerHost returns the partner SQL host for the current executor.
func MirrorPartnerHost(ctx *runner.StepContext) string {
	self := ""
	if ctx.Executor != nil {
		self = strings.TrimSpace(ctx.Executor.Host())
	}
	primary := ResolvePrimaryHost(ctx)
	if strings.EqualFold(self, primary) || self == "" {
		replicas := ReplicaHosts(ctx)
		if len(replicas) > 0 {
			return replicas[0]
		}
	}
	return primary
}

// ReplicaHosts returns replica host list from params.
func ReplicaHosts(ctx *runner.StepContext) []string {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Params["mssql_replica_hosts"].([]string); ok && len(v) > 0 {
		return v
	}
	if s, ok := ctx.Params["mssql_replica_hosts"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.Split(s, ",")
	}
	primary := ResolvePrimaryHost(ctx)
	var out []string
	for _, th := range ctx.HostsToRun() {
		h := strings.TrimSpace(th.Host)
		if h == "" || strings.EqualFold(h, primary) {
			continue
		}
		out = append(out, h)
	}
	return out
}

// MirrorPartnerAddress returns TCP partner address for SET PARTNER.
func MirrorPartnerAddress(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	return fmt.Sprintf("TCP://%s:%d", host, port)
}

// MirrorHostKey returns stable file key for cert naming (prefer IP).
func MirrorHostKey(host string) string {
	return strings.TrimSpace(host)
}

// CreateMirrorMasterKeySQL creates database master key if missing.
func CreateMirrorMasterKeySQL() string {
	return `
IF NOT EXISTS (SELECT 1 FROM sys.symmetric_keys WHERE name = '##MS_DatabaseMasterKey##')
BEGIN
  CREATE MASTER KEY ENCRYPTION BY PASSWORD = N'YinstallMirrorKey1!';
END`
}

// MirrorCertValidDays returns mirror cert validity in days from params (default 365).
func MirrorCertValidDays(ctx *runner.StepContext) int {
	if ctx != nil {
		if d := ctx.GetParamInt("mirror_cert_valid_days", 0); d > 0 {
			return d
		}
	}
	return DefaultMirrorCertValidDays
}

// CreateMirrorCertSQL creates server certificate for mirroring.
func CreateMirrorCertSQL(hostKey string, validDays int) string {
	return CreateHACertSQL(HAEndpointMirror, hostKey, validDays)
}

func mirrorCertName(hostKey string) string {
	key := strings.ReplaceAll(hostKey, ".", "_")
	key = strings.ReplaceAll(key, ":", "_")
	return "YinstallMirror_" + key
}

func mirrorLoginName(partnerKey string) string {
	return "MirrorLogin_" + strings.ReplaceAll(strings.ReplaceAll(partnerKey, ".", "_"), ":", "_")
}

// CreateMirrorEndpointSQL creates DATABASE_MIRRORING endpoint with certificate auth.
func CreateMirrorEndpointSQL(hostKey string, port int) string {
	return CreateCertEndpointSQL(HAEndpointMirror, hostKey, port)
}

// EnsureMirrorEndpointStartedSQL starts Mirror_endpoint when it exists but is not started.
func EnsureMirrorEndpointStartedSQL() string {
	return EnsureCertEndpointStartedSQL(HAEndpointMirror)
}

// ExportMirrorCertSQL returns BACKUP CERTIFICATE statement.
func ExportMirrorCertSQL(hostKey, filePath string) string {
	return ExportHACertSQL(HAEndpointMirror, hostKey, filePath)
}

// ImportMirrorPartnerCertSQL creates cert + login + grant for partner.
func ImportMirrorPartnerCertSQL(partnerKey, cerPath string) []string {
	return ImportHAPartnerCertSQL(HAEndpointMirror, partnerKey, cerPath)
}

// CreateMirrorSeedDBSQL creates empty database for mirroring test.
func CreateMirrorSeedDBSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	return fmt.Sprintf(`
IF NOT EXISTS (SELECT 1 FROM sys.databases WHERE name = N'%s')
BEGIN
  CREATE DATABASE [%s];
END`, dbName, dbName)
}

// BackupMirrorDBSQL backs up database to path.
func BackupMirrorDBSQL(dbName, backupPath string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	backupPath = strings.ReplaceAll(backupPath, "'", "''")
	return fmt.Sprintf("BACKUP DATABASE [%s] TO DISK = N'%s' WITH FORMAT, INIT;", dbName, backupPath)
}

// RestoreMirrorDBSQL restores with NORECOVERY for mirroring.
func RestoreMirrorDBSQL(dbName, backupPath string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	backupPath = strings.ReplaceAll(backupPath, "'", "''")
	return fmt.Sprintf(`
RESTORE DATABASE [%s] FROM DISK = N'%s' WITH NORECOVERY, REPLACE;`, dbName, backupPath)
}

// BackupMirrorLogSQL backs up transaction log for mirroring catch-up.
func BackupMirrorLogSQL(dbName, logPath string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	logPath = strings.ReplaceAll(logPath, "'", "''")
	return fmt.Sprintf("BACKUP LOG [%s] TO DISK = N'%s' WITH INIT;", dbName, logPath)
}

// RestoreMirrorLogSQL restores transaction log with NORECOVERY before SET PARTNER.
func RestoreMirrorLogSQL(dbName, logPath string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	logPath = strings.ReplaceAll(logPath, "'", "''")
	return fmt.Sprintf("RESTORE LOG [%s] FROM DISK = N'%s' WITH NORECOVERY;", dbName, logPath)
}

// SetMirrorPartnerSQL sets partner on database.
func SetMirrorPartnerSQL(dbName, partnerAddr string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	partnerAddr = strings.ReplaceAll(partnerAddr, "'", "''")
	return fmt.Sprintf("ALTER DATABASE [%s] SET PARTNER = N'%s';", dbName, partnerAddr)
}

// RemoveMirrorPartnerSQL removes database mirroring on the principal.
func RemoveMirrorPartnerSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	esc := strings.ReplaceAll(dbName, "'", "''")
	return fmt.Sprintf(`
IF EXISTS (
  SELECT 1 FROM sys.database_mirroring m
  INNER JOIN sys.databases d ON d.database_id = m.database_id
  WHERE d.name = N'%s' AND m.mirroring_state IS NOT NULL
)
BEGIN
  ALTER DATABASE [%s] SET PARTNER OFF WITH ROLLBACK IMMEDIATE;
END`, esc, dbName)
}

// RecoverMirroredDBSQL brings a former mirror database online with RECOVERY.
func RecoverMirroredDBSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	return fmt.Sprintf("RESTORE DATABASE [%s] WITH RECOVERY;", dbName)
}

// MirrorHasPartnerSQL returns whether the database has an active mirroring partner.
func MirrorHasPartnerSQL(dbName string) string {
	return MirrorHasPartnerScalarSQL(dbName)
}

// MirrorHasPartnerScalarSQL returns 1 when the database has mirroring_state set.
func MirrorHasPartnerScalarSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "'", "''")
	return fmt.Sprintf(`SELECT CASE WHEN EXISTS (
  SELECT 1 FROM sys.database_mirroring m
  INNER JOIN sys.databases d ON d.database_id = m.database_id
  WHERE d.name = N'%s' AND m.mirroring_state IS NOT NULL
) THEN N'1' ELSE N'0' END;`, dbName)
}

// MirrorDBRestoringSQL returns whether the database is in RESTORING state (post partner off on mirror).
func MirrorDBRestoringSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "'", "''")
	return fmt.Sprintf(`
SELECT d.state_desc
FROM sys.databases d
WHERE d.name = N'%s';`, dbName)
}

// MirrorDBStateSQL returns database mirroring state for precheck/idempotency.
func MirrorDBStateSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "'", "''")
	return fmt.Sprintf(`
SELECT d.state_desc, m.mirroring_state, m.mirroring_role_desc
FROM sys.databases d
LEFT JOIN sys.database_mirroring m ON d.database_id = m.database_id
WHERE d.name = N'%s';`, dbName)
}

// VerifyMirrorSQL returns mirroring state query.
func VerifyMirrorSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "'", "''")
	return fmt.Sprintf(`
SELECT DB_NAME(database_id) AS db_name, mirroring_state, mirroring_role_desc, mirroring_partner_name
FROM sys.database_mirroring
WHERE DB_NAME(database_id) = N'%s';`, dbName)
}

// AdminShareUNC returns \\host\C$ UNC for admin share.
func AdminShareUNC(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, `\\`)
	if i := strings.Index(host, `\`); i >= 0 {
		host = host[:i]
	}
	return `\\` + host + `\C$`
}

// AdminShareMirrorCertPath returns UNC path to certHostKey's cert on shareHost admin share.
func AdminShareMirrorCertPath(ctx *runner.StepContext, shareHost, certHostKey string) string {
	rel := strings.TrimPrefix(mirrorCertFilePath(ctx, shareHost, certHostKey), `C:\`)
	return AdminShareUNC(shareHost) + `\` + rel
}
