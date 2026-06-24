package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// InstalledPaths holds SQL Server paths read from registry after install.
type InstalledPaths struct {
	InstanceRoot string
	DataDir      string
	BackupDir    string
}

// DiscoverInstalledPaths reads Setup registry keys for an installed instance.
func DiscoverInstalledPaths(ctx *runner.StepContext, instance string) (InstalledPaths, error) {
	if ctx == nil {
		return InstalledPaths{}, fmt.Errorf("nil context")
	}
	instance = strings.TrimSpace(instance)
	if instance == "" {
		instance = DefaultInstance
	}
	if ctx.DryRun || ctx.Precheck {
		return InstalledPaths{
			InstanceRoot: `(registry-dry-run)`,
			DataDir:      `(registry-dry-run)`,
			BackupDir:    `(registry-dry-run)`,
		}, nil
	}
	if entry, ok := RegistryEntryFromContext(ctx); ok && strings.TrimSpace(entry.SQLPath) != "" {
		dataDir := userDatabaseDirFromRegistry(entry.DataRoot, entry.SQLPath)
		backup := entry.BackupDir
		if backup == "" {
			backup = joinWinPath(entry.SQLPath, "Backup")
		}
		return InstalledPaths{
			InstanceRoot: entry.SQLPath,
			DataDir:      dataDir,
			BackupDir:    backup,
		}, nil
	}
	qInst := strings.ReplaceAll(instance, `'`, `''`)
	script := fmt.Sprintf(
		`$inst='%s'; $names=Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\Instance Names\SQL' -ErrorAction Stop; `+
			`$id=$names.$inst; if (-not $id) { throw \"instance not found: $inst\" }; `+
			`$s=Get-ItemProperty \"HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\$id\Setup\" -ErrorAction Stop; `+
			`Write-Output ($s.SQLPath+'|'+$s.SQLDataRoot+'|'+$s.BackupDirectory)`,
		qInst,
	)
	res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false)
	if err != nil {
		return InstalledPaths{}, fmt.Errorf("discover instance paths: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(res.GetStdout()), "|")
	if len(parts) < 3 {
		return InstalledPaths{}, fmt.Errorf("unexpected registry output: %q", res.GetStdout())
	}
	return InstalledPaths{
		InstanceRoot: normalizeWinPath(parts[0]),
		DataDir:      normalizeWinPath(parts[1]),
		BackupDir:    normalizeWinPath(parts[2]),
	}, nil
}

// EnrichLayoutWithInstalledPaths fills empty SQL paths from registry (after setup when UseSQLDefaults).
func EnrichLayoutWithInstalledPaths(ctx *runner.StepContext, layout Layout) (Layout, error) {
	if !layout.UseSQLDefaults {
		return layout, nil
	}
	installed, err := DiscoverInstalledPaths(ctx, layout.Instance)
	if err != nil {
		return layout, err
	}
	layout.Base = installed.InstanceRoot
	layout.DataDir = installed.DataDir
	layout.LogDir = installed.DataDir
	layout.BackupDir = installed.BackupDir
	return layout, nil
}

// LayoutPathParamsExplicitFromContext reports whether CLI passed any data path override.
func LayoutPathParamsExplicitFromContext(ctx *runner.StepContext) bool {
	if ctx == nil {
		return false
	}
	return pathCustomizationExplicit(
		firstNonEmpty(ctx.GetParamString("mssql_data_root", ""), ctx.GetParamString("mssql_database", "")),
		firstNonEmpty(ctx.GetParamString("mssql_data_dir", ""), ctx.GetParamString("mssql_data", "")),
		firstNonEmpty(ctx.GetParamString("mssql_log_dir", ""), ctx.GetParamString("mssql_log", "")),
		firstNonEmpty(ctx.GetParamString("mssql_backup_dir", ""), ctx.GetParamString("mssql_backup", "")),
	)
}

func registryUsesYinstallDataLayout(entry InstanceRegistryEntry) bool {
	return yinstallDataRootFromRegistry(entry) != ""
}

func yinstallDataRootFromRegistry(entry InstanceRegistryEntry) string {
	dataRoot := normalizeWinPath(entry.DataRoot)
	inst := strings.TrimSpace(entry.Name)
	if inst == "" {
		return ""
	}
	if dataRoot != "" && strings.HasSuffix(strings.ToLower(dataRoot), `\`+strings.ToLower(inst)) {
		return dataRoot
	}
	if dataRoot != "" {
		needle := `\` + inst + `\`
		if idx := strings.Index(strings.ToLower(dataRoot), strings.ToLower(needle)); idx > 0 {
			return normalizeWinPath(dataRoot[:idx+len(needle)-1])
		}
	}
	backup := normalizeWinPath(entry.BackupDir)
	if dataRoot != "" && backup != "" && strings.EqualFold(backup, joinWinPath(dataRoot, "Backup")) {
		return dataRoot
	}
	return ""
}

func instanceRootFromSQLPath(sqlPath string) string {
	sqlPath = normalizeWinPath(sqlPath)
	if strings.HasSuffix(strings.ToLower(sqlPath), `\mssql`) {
		return strings.TrimRight(sqlPath[:len(sqlPath)-len(`\MSSQL`)], `\`)
	}
	return sqlPath
}

// RestoreTargetDirsFromContext returns data/log directories for RESTORE ... WITH MOVE.
// Priority: --replica-mssql-restore-* / mssql_restore_* params, then CLI install path
// flags (--mssql-data-root / --mssql-data-dir), then target instance registry layout.
func RestoreTargetDirsFromContext(ctx *runner.StepContext) (dataDir, logDir string, err error) {
	if ctx == nil {
		return "", "", fmt.Errorf("nil context")
	}
	dataDir = normalizeWinPath(firstNonEmpty(
		ctx.GetParamString("mssql_restore_data_dir", ""),
		ctx.GetParamString("replica_mssql_restore_data_dir", ""),
	))
	logDir = normalizeWinPath(firstNonEmpty(
		ctx.GetParamString("mssql_restore_log_dir", ""),
		ctx.GetParamString("replica_mssql_restore_log_dir", ""),
	))
	if dataDir != "" {
		if logDir == "" {
			logDir = dataDir
		}
		return dataDir, logDir, nil
	}
	if LayoutPathParamsExplicitFromContext(ctx) {
		layout := ResolveLayoutFromContext(ctx)
		if strings.TrimSpace(layout.DataDir) == "" {
			return "", "", fmt.Errorf("restore data directory not resolved from CLI path flags")
		}
		logDir = layout.LogDir
		if strings.TrimSpace(logDir) == "" {
			logDir = layout.DataDir
		}
		return layout.DataDir, logDir, nil
	}
	if entry, ok := RegistryEntryFromContext(ctx); ok {
		if dataDir, logDir, ok := restoreDirsFromRegistryEntry(entry); ok {
			return dataDir, logDir, nil
		}
	}
	entry, resolveErr := EnsureInstanceResolved(ctx)
	if resolveErr != nil {
		return "", "", fmt.Errorf("resolve instance for restore paths: %w", resolveErr)
	}
	if dataDir, logDir, ok := restoreDirsFromRegistryEntry(entry); ok {
		return dataDir, logDir, nil
	}
	inst := strings.TrimSpace(entry.Name)
	if inst == "" {
		inst = DefaultInstance
	}
	return "", "", fmt.Errorf("cannot resolve restore data directory for instance %q", inst)
}

func restoreDirsFromRegistryEntry(entry InstanceRegistryEntry) (dataDir, logDir string, ok bool) {
	layout := LayoutFromRegistryEntry(entry)
	if strings.TrimSpace(layout.DataDir) == "" {
		return "", "", false
	}
	logDir = layout.LogDir
	if strings.TrimSpace(logDir) == "" {
		logDir = layout.DataDir
	}
	return layout.DataDir, logDir, true
}

// userDatabaseDirFromRegistry returns the directory for user database files (.mdf).
func userDatabaseDirFromRegistry(dataRoot, sqlPath string) string {
	dataRoot = normalizeWinPath(dataRoot)
	sqlPath = normalizeWinPath(sqlPath)
	if dataRoot == "" {
		if sqlPath != "" {
			return joinWinPath(sqlPath, "DATA")
		}
		return ""
	}
	lower := strings.ToLower(dataRoot)
	if strings.HasSuffix(lower, `\data`) {
		return dataRoot
	}
	if sqlPath != "" && strings.EqualFold(dataRoot, sqlPath) {
		return joinWinPath(dataRoot, "DATA")
	}
	if strings.HasSuffix(lower, `\mssql`) {
		return joinWinPath(dataRoot, "DATA")
	}
	return dataRoot
}

// LayoutFromRegistryEntry builds cleanup data paths from a resolved registry entry.
// Does not include program/shared dirs (InstanceDir/SharedDir/130).
func LayoutFromRegistryEntry(entry InstanceRegistryEntry) Layout {
	layout := Layout{
		Instance:       strings.TrimSpace(entry.Name),
		Port:           entry.ListenPort,
		UseSQLDefaults: false,
	}
	dataRoot := normalizeWinPath(entry.DataRoot)
	sqlPath := normalizeWinPath(entry.SQLPath)
	backup := normalizeWinPath(entry.BackupDir)

	if base := yinstallDataRootFromRegistry(entry); base != "" {
		layout.Base = base
		layout.DatabaseRoot = base
		layout.DataDir = joinWinPath(base, "Data")
		layout.LogDir = joinWinPath(base, "Log")
		if backup != "" {
			layout.BackupDir = backup
		} else {
			layout.BackupDir = joinWinPath(base, "Backup")
		}
		return layout
	}

	if dataRoot != "" {
		layout.DataDir = userDatabaseDirFromRegistry(dataRoot, sqlPath)
		layout.LogDir = layout.DataDir
		if sqlPath != "" {
			layout.Base = instanceRootFromSQLPath(sqlPath)
		} else {
			layout.Base = dataRoot
		}
		layout.DatabaseRoot = layout.Base
	} else if sqlPath != "" {
		layout.DataDir = joinWinPath(sqlPath, "DATA")
		layout.LogDir = layout.DataDir
		layout.Base = instanceRootFromSQLPath(sqlPath)
	}
	if backup != "" {
		layout.BackupDir = backup
	} else if sqlPath != "" {
		layout.BackupDir = joinWinPath(sqlPath, "Backup")
	} else if layout.Base != "" {
		layout.BackupDir = joinWinPath(layout.Base, "Backup")
	}
	return layout
}

// EnrichCleanLayoutFromRegistry fills data/backup paths from registry when clean omits path flags.
func EnrichCleanLayoutFromRegistry(ctx *runner.StepContext, layout Layout) (Layout, error) {
	if ctx == nil || LayoutPathParamsExplicitFromContext(ctx) {
		return layout, nil
	}
	entry, ok := RegistryEntryFromContext(ctx)
	if !ok {
		var err error
		entry, err = EnsureInstanceResolved(ctx)
		if err != nil {
			return layout, err
		}
	}
	reg := LayoutFromRegistryEntry(entry)
	reg.AdminBase = layout.AdminBase
	reg.Instance = layout.Instance
	if layout.Port > 0 {
		reg.Port = layout.Port
	}
	reg.ProgramDir = layout.ProgramDir
	reg.SharedDir = layout.SharedDir
	reg.InstanceDir = layout.InstanceDir
	reg.SetupProductMajor = layout.SetupProductMajor
	reg.UseProgramCustom = layout.UseProgramCustom
	return reg, nil
}

// ShouldOmitInstallSharedDir reports whether INSTALLSHAREDDIR should be omitted because
// another instance with the same product major is already installed on the host.
func ShouldOmitInstallSharedDir(ctx *runner.StepContext, major int, newInstance string) (bool, error) {
	if ctx == nil || major <= 0 {
		return false, nil
	}
	if ctx.DryRun || ctx.Precheck {
		return false, nil
	}
	entries, err := ListInstanceRegistry(ctx)
	if err != nil {
		return false, err
	}
	newInstance = strings.TrimSpace(newInstance)
	for _, e := range entries {
		if e.ProductMajor == major && !strings.EqualFold(strings.TrimSpace(e.Name), newInstance) {
			return true, nil
		}
	}
	return false, nil
}
