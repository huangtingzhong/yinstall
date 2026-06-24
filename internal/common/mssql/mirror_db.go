package mssql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/yinstall/internal/runner"
)

// MirrorDBStatus describes database state relevant to mirroring setup.
type MirrorDBStatus struct {
	Host           string
	Name           string
	Exists         bool
	StateDesc      string
	RecoveryModel  string
	MirroringState string
	MirroringRole  string
}

func (s MirrorDBStatus) IsMirroring() bool {
	return strings.TrimSpace(s.MirroringState) != "" && strings.TrimSpace(s.MirroringState) != "NULL"
}

// IsSynchronized reports mirroring_state = 4 (SYNCHRONIZED).
func (s MirrorDBStatus) IsSynchronized() bool {
	return strings.TrimSpace(s.MirroringState) == "4"
}

func (s MirrorDBStatus) IsRestoring() bool {
	return strings.EqualFold(strings.TrimSpace(s.StateDesc), "RESTORING")
}

func (s MirrorDBStatus) IsRecovering() bool {
	return strings.EqualFold(strings.TrimSpace(s.StateDesc), "RECOVERING")
}

func (s MirrorDBStatus) IsOnline() bool {
	return s.Exists && strings.EqualFold(strings.TrimSpace(s.StateDesc), "ONLINE")
}

func (s MirrorDBStatus) IsFullRecovery() bool {
	return strings.EqualFold(strings.TrimSpace(s.RecoveryModel), "FULL")
}

// MirrorSkipSeed reports whether MSH-105/106 seed steps should be skipped.
func MirrorSkipSeed(ctx *runner.StepContext) bool {
	return ctx != nil && ctx.GetParamBool("mirror_skip_seed", false)
}

// MirrorDropSecondaryDB reports whether ha remove / remove-ag should DROP DATABASE on secondary.
func MirrorDropSecondaryDB(ctx *runner.StepContext) bool {
	return ctx != nil && ctx.GetParamBool("mirror_drop_secondary_db", false)
}

// MirrorDropExistingSecondary reports whether secondary DB may be dropped before restore.
func MirrorDropExistingSecondary(ctx *runner.StepContext) bool {
	return ctx != nil && ctx.GetParamBool("mirror_drop_existing", false)
}

// MirrorBusinessDBListSQL lists online user databases eligible for mirroring on primary.
func MirrorBusinessDBListSQL() string {
	return `SELECT name FROM sys.databases
WHERE database_id > 4
  AND state_desc = N'ONLINE'
  AND is_read_only = 0
ORDER BY name;`
}

// MirrorMirroredDBListSQL lists databases with active mirroring (for remove when --mirror-db unset).
func MirrorMirroredDBListSQL() string {
	return `SELECT d.name
FROM sys.databases d
INNER JOIN sys.database_mirroring m ON m.database_id = d.database_id
WHERE m.mirroring_state IS NOT NULL
ORDER BY d.name;`
}

// ParseMirrorDBNames splits comma-separated --mirror-db values.
func ParseMirrorDBNames(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	seen := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

// IsSqlcmdMetaLine reports sqlcmd headers, separators, and row-count footers (any locale).
func IsSqlcmdMetaLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "---") {
		return true
	}
	lower := strings.ToLower(line)
	if strings.EqualFold(line, "name") || strings.Contains(lower, "rows affected") {
		return true
	}
	// e.g. "(4 rows affected)" or localized "(4 行受影响)" (may arrive garbled over SSH).
	if strings.HasPrefix(line, "(") && strings.HasSuffix(line, ")") {
		inner := strings.TrimSpace(strings.Trim(line, "()"))
		if i := strings.IndexFunc(inner, func(r rune) bool { return !unicode.IsDigit(r) }); i > 0 {
			if _, err := strconv.Atoi(inner[:i]); err == nil {
				return true
			}
		} else if inner != "" {
			if _, err := strconv.Atoi(inner); err == nil {
				return true
			}
		}
	}
	return false
}

// ParseMirrorDBNameList parses sqlcmd name column output.
func ParseMirrorDBNameList(stdout string) ([]string, error) {
	var out []string
	seen := make(map[string]struct{})
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if IsSqlcmdMetaLine(line) {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	return out, nil
}

// SetMirrorDBList stores resolved mirror targets in Results.
func SetMirrorDBList(ctx *runner.StepContext, dbs []string) {
	if ctx == nil {
		return
	}
	ctx.SetResult(MirrorDBListResultKey(), append([]string(nil), dbs...))
}

// MirrorTargetDBs returns databases to mirror: explicit --mirror-db list or discovered list from primary.
func MirrorTargetDBs(ctx *runner.StepContext) ([]string, error) {
	if dbs := MirrorDBNamesParam(ctx); len(dbs) > 0 {
		return append([]string(nil), dbs...), nil
	}
	if ctx != nil {
		if v, ok := ctx.Results[MirrorDBListResultKey()].([]string); ok && len(v) > 0 {
			return append([]string(nil), v...), nil
		}
	}
	return nil, fmt.Errorf("mirror database list not resolved; run MSH-101 on primary first or set --mirror-db")
}

// MirrorDBStatusSQL returns pipe-delimited database inventory for sqlcmd.
func MirrorDBStatusSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "'", "''")
	return fmt.Sprintf(`
SELECT CONCAT(
  CASE WHEN d.database_id IS NULL THEN N'0' ELSE N'1' END, N'|',
  ISNULL(d.state_desc, N''), N'|',
  ISNULL(d.recovery_model_desc, N''), N'|',
  ISNULL(CAST(m.mirroring_state AS NVARCHAR(10)), N''), N'|',
  ISNULL(m.mirroring_role_desc, N'')
) AS mirror_db_status
FROM (SELECT N'%s' AS name) x
LEFT JOIN sys.databases d ON d.name = x.name
LEFT JOIN sys.database_mirroring m ON m.database_id = d.database_id;`, dbName)
}

// MirrorDBStatusResultKey stores per-host DB status in Results.
func MirrorDBStatusResultKey(hostKey, dbName string) string {
	return "mirror_db_status_" + MirrorHostKey(hostKey) + "_" + mirrorDBResultSuffix(dbName)
}

// ParseMirrorDBStatus parses sqlcmd stdout from MirrorDBStatusSQL.
func ParseMirrorDBStatus(host, dbName, stdout string) (MirrorDBStatus, error) {
	host = strings.TrimSpace(host)
	dbName = strings.TrimSpace(dbName)
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "---") ||
			strings.EqualFold(line, "mirror_db_status") {
			continue
		}
		if !strings.Contains(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		st := MirrorDBStatus{
			Host:           host,
			Name:           dbName,
			Exists:         strings.TrimSpace(parts[0]) == "1",
			StateDesc:      strings.TrimSpace(parts[1]),
			RecoveryModel:  strings.TrimSpace(parts[2]),
			MirroringState: strings.TrimSpace(parts[3]),
			MirroringRole:  strings.TrimSpace(parts[4]),
		}
		return st, nil
	}
	return MirrorDBStatus{Host: host, Name: dbName}, fmt.Errorf("cannot parse mirror database status from sqlcmd output")
}

// ValidatePrimaryMirrorSeed checks primary database state before backup.
func ValidatePrimaryMirrorSeed(st MirrorDBStatus) error {
	if st.IsMirroring() {
		return fmt.Errorf("primary database %s already in mirroring (state=%s role=%s)", st.Name, st.MirroringState, st.MirroringRole)
	}
	if !st.Exists {
		return fmt.Errorf("primary database %s does not exist", st.Name)
	}
	if !st.IsFullRecovery() {
		return fmt.Errorf("primary database %s recovery model is %s; FULL required for mirroring", st.Name, st.RecoveryModel)
	}
	return nil
}

// ValidateSecondaryMirrorRestore checks secondary database state before restore.
func ValidateSecondaryMirrorRestore(st MirrorDBStatus, dropExisting, force bool) error {
	if st.IsMirroring() || st.IsRestoring() {
		return nil
	}
	if !st.Exists {
		return nil
	}
	if dropExisting {
		if !force {
			return fmt.Errorf("secondary database %s exists; use --mirror-drop-existing with -F or -f MSH-106 (primary is never dropped)", st.Name)
		}
		return nil
	}
	return fmt.Errorf(
		"secondary database %s exists (state=%s); use --mirror-drop-existing with -F or -f MSH-106 to drop on secondary only, or set --mirror-db",
		st.Name, st.StateDesc,
	)
}

// MirrorPartnerOffSQL removes mirroring on secondary (single statement batch).
func MirrorPartnerOffSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	return fmt.Sprintf("ALTER DATABASE [%s] SET PARTNER OFF WITH ROLLBACK IMMEDIATE;", dbName)
}

// MirrorRecoverSecondarySQL must run alone in a sqlcmd batch (RESTORE requirement).
func MirrorRecoverSecondarySQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	return fmt.Sprintf("RESTORE DATABASE [%s] WITH RECOVERY;", dbName)
}

// MirrorDropDatabaseSQL drops an online/restored database on secondary.
func MirrorDropDatabaseSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	esc := strings.ReplaceAll(dbName, "'", "''")
	return fmt.Sprintf(`
IF EXISTS (SELECT 1 FROM sys.databases WHERE name = N'%s')
BEGIN
  ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE;
  DROP DATABASE [%s];
END`, esc, dbName, dbName)
}

// DropMirrorSecondaryDBSQL drops database on mirror partner only (caller must enforce secondary host).
func DropMirrorSecondaryDBSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	esc := strings.ReplaceAll(dbName, "'", "''")
	return fmt.Sprintf(`
IF EXISTS (SELECT 1 FROM sys.databases WHERE name = N'%s')
BEGIN
  ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE;
  DROP DATABASE [%s];
END`, esc, dbName, dbName)
}

// DropMirrorSecondaryDBAfterRemoveSQL drops a former mirror database on secondary after partner off on primary.
func DropMirrorSecondaryDBAfterRemoveSQL(dbName string) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	esc := strings.ReplaceAll(dbName, "'", "''")
	return fmt.Sprintf(`
IF EXISTS (
  SELECT 1 FROM sys.databases d
  INNER JOIN sys.database_mirroring m ON m.database_id = d.database_id
  WHERE d.name = N'%s' AND m.mirroring_state IS NOT NULL
)
  ALTER DATABASE [%s] SET PARTNER OFF WITH ROLLBACK IMMEDIATE;
IF EXISTS (SELECT 1 FROM sys.databases WHERE name = N'%s' AND state_desc = N'RESTORING')
  RESTORE DATABASE [%s] WITH RECOVERY;
IF EXISTS (SELECT 1 FROM sys.databases WHERE name = N'%s')
BEGIN
  ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE;
  DROP DATABASE [%s];
END`, esc, dbName, esc, dbName, esc, dbName, dbName)
}

func mirrorDBResultSuffix(dbName string) string {
	return strings.NewReplacer(`\`, "_", `/`, "_", `:`, "_", `*`, "_", `?`, "_", `"`, "_", `<`, "_", `>`, "_", `|`, "_", `[`, "_", `]`, "_", ` `, "_").Replace(strings.TrimSpace(dbName))
}

// MirrorBackupPathResultKey stores per-database backup path in Results.
func MirrorBackupPathResultKey(dbName string) string {
	return "mirror_backup_path_" + mirrorDBResultSuffix(dbName)
}

// MirrorBackupTimestamp returns UTC timestamp for backup file naming (YYYYmmdd_HHMMSS).
func MirrorBackupTimestamp() string {
	return time.Now().UTC().Format("20060102_150405")
}

// MirrorBackupFileName returns {db}_{timestamp}.bak for mirror seed backups.
func MirrorBackupFileName(dbName, timestamp string) string {
	dbName = strings.TrimSpace(dbName)
	timestamp = strings.TrimSpace(timestamp)
	safe := mirrorDBResultSuffix(dbName)
	if safe == "" {
		safe = "mirror"
	}
	return safe + "_" + timestamp + ".bak"
}

// MirrorBackupBaseDir returns directory for mirror seed .bak files.
func MirrorBackupBaseDir(ctx *runner.StepContext) string {
	return mirrorBackupBaseDir(ctx)
}

func mirrorBackupBaseDir(ctx *runner.StepContext) string {
	if ctx != nil {
		if d := strings.TrimSpace(ctx.GetParamString("mirror_backup_dir", "")); d != "" {
			return strings.TrimRight(d, `\`)
		}
	}
	return MirrorWorkDir(ctx)
}

func mirrorBackupBaseDirForHost(ctx *runner.StepContext, hostKey string) string {
	if ctx != nil {
		if d := strings.TrimSpace(ctx.GetParamString("mirror_backup_dir", "")); d != "" {
			return strings.TrimRight(d, `\`)
		}
	}
	return MirrorWorkDirForHost(ctx, hostKey)
}

// MirrorNewBackupPath builds a new backup path using db name + timestamp file naming.
func MirrorNewBackupPath(ctx *runner.StepContext, dbName, timestamp string) string {
	return joinWinPath(mirrorBackupBaseDir(ctx), MirrorBackupFileName(dbName, timestamp))
}

// MirrorNewBackupPathForHost builds backup path for a specific host layout.
func MirrorNewBackupPathForHost(ctx *runner.StepContext, hostKey, dbName, timestamp string) string {
	return joinWinPath(mirrorBackupBaseDirForHost(ctx, hostKey), MirrorBackupFileName(dbName, timestamp))
}

// MirrorPrimaryBackupPath returns backup path recorded by MSH-105 for a database.
func MirrorPrimaryBackupPath(ctx *runner.StepContext, dbName string) string {
	if ctx == nil || strings.TrimSpace(dbName) == "" {
		return ""
	}
	key := MirrorBackupPathResultKey(dbName)
	if v, ok := ctx.Results[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

// MirrorSecondaryLocalBackupPath returns local copy path on secondary (same file name, local work dir).
func MirrorSecondaryLocalBackupPath(ctx *runner.StepContext, primaryBackupPath string) string {
	base := strings.TrimSpace(primaryBackupPath)
	if i := strings.LastIndexAny(base, `\/`); i >= 0 {
		base = base[i+1:]
	}
	return joinWinPath(MirrorWorkDir(ctx), base)
}

// MirrorLogBackupPathResultKey stores per-database log backup path in Results.
func MirrorLogBackupPathResultKey(dbName string) string {
	return "mirror_log_backup_path_" + mirrorDBResultSuffix(dbName)
}

// MirrorLogBackupPathFromFull derives log backup path from full backup path.
func MirrorLogBackupPathFromFull(fullBackupPath string) string {
	base := strings.TrimSpace(fullBackupPath)
	if len(base) >= 4 && strings.EqualFold(base[len(base)-4:], ".bak") {
		return base[:len(base)-4] + "_log.trn"
	}
	return base + "_log.trn"
}

// MirrorPrimaryLogBackupPath returns log backup path recorded by MSH-107 log-backup phase.
func MirrorPrimaryLogBackupPath(ctx *runner.StepContext, dbName string) string {
	if ctx == nil || strings.TrimSpace(dbName) == "" {
		return ""
	}
	key := MirrorLogBackupPathResultKey(dbName)
	if v, ok := ctx.Results[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if full := MirrorPrimaryBackupPath(ctx, dbName); full != "" {
		return MirrorLogBackupPathFromFull(full)
	}
	return ""
}

// MirrorLogRestoreSource resolves local log restore path and primary admin-share fetch source.
func MirrorLogRestoreSource(ctx *runner.StepContext, dbName string) (localPath, fetchRemote string) {
	primary := ResolvePrimaryHost(ctx)
	primaryLog := MirrorPrimaryLogBackupPath(ctx, dbName)
	localPath = MirrorSecondaryLocalBackupPath(ctx, primaryLog)
	fetchRemote = AdminShareUNC(primary) + strings.TrimPrefix(primaryLog, `C:`)
	return localPath, fetchRemote
}

// MirrorRestoreSource resolves local restore path and optional primary admin-share fetch source.
func MirrorRestoreSource(ctx *runner.StepContext, dbName string) (localPath, fetchRemote string, skipFetch bool) {
	if ctx != nil {
		if from := strings.TrimSpace(ctx.GetParamString("mirror_restore_from", "")); from != "" {
			return from, "", true
		}
	}
	primary := ResolvePrimaryHost(ctx)
	primaryBackup := MirrorPrimaryBackupPath(ctx, dbName)
	if primaryBackup == "" {
		primaryBackup = MirrorNewBackupPathForHost(ctx, primary, dbName, MirrorBackupTimestamp())
	}
	localPath = MirrorSecondaryLocalBackupPath(ctx, primaryBackup)
	fetchRemote = AdminShareUNC(primary) + strings.TrimPrefix(primaryBackup, `C:`)
	return localPath, fetchRemote, false
}

// MirrorRestoreFile describes one file from RESTORE FILELISTONLY.
type MirrorRestoreFile struct {
	LogicalName  string
	Type         string
	PhysicalName string
}

// RestoreFileListOnlySQL lists logical/physical/type rows from a backup set.
func RestoreFileListOnlySQL(backupPath string) string {
	backupPath = strings.ReplaceAll(backupPath, "'", "''")
	return fmt.Sprintf("RESTORE FILELISTONLY FROM DISK = N'%s';", backupPath)
}

// ParseRestoreFileList parses sqlcmd stdout from RestoreFileListOnlySQL.
func ParseRestoreFileList(stdout string) ([]MirrorRestoreFile, error) {
	var out []MirrorRestoreFile
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || IsSqlcmdMetaLine(line) ||
			strings.EqualFold(line, "LogicalName") ||
			strings.HasPrefix(line, "LogicalName ") {
			continue
		}
		if strings.Contains(line, "|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 3 {
				out = append(out, MirrorRestoreFile{
					LogicalName:  strings.TrimSpace(parts[0]),
					Type:         strings.TrimSpace(parts[1]),
					PhysicalName: strings.TrimSpace(parts[2]),
				})
			}
			continue
		}
		fields := splitSqlcmdColumns(line)
		if len(fields) < 3 {
			continue
		}
		out = append(out, MirrorRestoreFile{
			LogicalName:  fields[0],
			Type:         fields[2],
			PhysicalName: fields[1],
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no restore files parsed from backup metadata")
	}
	return out, nil
}

func splitSqlcmdColumns(line string) []string {
	var fields []string
	for _, part := range strings.FieldsFunc(line, func(r rune) bool { return r == '\t' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields = append(fields, part)
	}
	if len(fields) >= 3 {
		return fields
	}
	// sqlcmd fixed-width columns: LogicalName PhysicalName Type ...
	re := regexp.MustCompile(`\s{2,}`)
	raw := re.Split(strings.TrimSpace(line), -1)
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func mirrorRestoreTargetPath(dataDir, logDir, dbName string, file MirrorRestoreFile, dataIndex int) string {
	baseName := winBaseName(file.PhysicalName)
	if baseName == "" {
		if file.Type == "L" {
			baseName = dbName + "_log.ldf"
		} else if dataIndex > 0 {
			baseName = fmt.Sprintf("%s_%d.mdf", dbName, dataIndex)
		} else {
			baseName = dbName + ".mdf"
		}
	}
	targetDir := dataDir
	if file.Type == "L" {
		targetDir = logDir
	}
	return joinWinPath(targetDir, baseName)
}

func winBaseName(path string) string {
	path = strings.ReplaceAll(path, `/`, `\`)
	if i := strings.LastIndex(path, `\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// RestoreMirrorDBSQLWithMove restores on secondary with MOVE to instance data/log dirs.
func RestoreMirrorDBSQLWithMove(dbName, backupPath, dataDir, logDir string, files []MirrorRestoreFile) string {
	dbName = strings.ReplaceAll(dbName, "]", "]]")
	backupPath = strings.ReplaceAll(backupPath, "'", "''")
	dataDir = normalizeWinPath(dataDir)
	logDir = normalizeWinPath(logDir)
	if logDir == "" {
		logDir = dataDir
	}
	var moves []string
	dataFiles := 0
	for _, f := range files {
		if strings.TrimSpace(f.LogicalName) == "" {
			continue
		}
		idx := dataFiles
		if f.Type != "L" {
			dataFiles++
		}
		target := mirrorRestoreTargetPath(dataDir, logDir, dbName, f, idx)
		logical := strings.ReplaceAll(f.LogicalName, "]", "]]")
		target = strings.ReplaceAll(target, "'", "''")
		moves = append(moves, fmt.Sprintf("MOVE N'%s' TO N'%s'", logical, target))
	}
	moveClause := ""
	if len(moves) > 0 {
		moveClause = ", " + strings.Join(moves, ", ")
	}
	return fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = N'%s' WITH NORECOVERY, REPLACE%s;", dbName, backupPath, moveClause)
}

// RestoreDBNorecoveryWithMove restores a full backup on the current instance with MOVE to local data/log dirs.
func RestoreDBNorecoveryWithMove(ctx *runner.StepContext, label, dbName, backupPath string) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	fileListOut, err := QuerySqlcmdScalar(ctx, label+" filelistonly", RestoreFileListOnlySQL(backupPath))
	if err != nil {
		return err
	}
	files, err := ParseRestoreFileList(fileListOut)
	if err != nil {
		return fmt.Errorf("backup file list: %w", err)
	}
	dataDir, logDir, err := RestoreTargetDirsFromContext(ctx)
	if err != nil {
		return err
	}
	if ctx.Logger != nil {
		ctx.Logger.Info("%s: restore MOVE targets data=%s log=%s", label, dataDir, logDir)
	}
	restore := RestoreMirrorDBSQLWithMove(dbName, backupPath, dataDir, logDir, files)
	return RunSqlcmdQueries(ctx, label, []string{restore})
}
