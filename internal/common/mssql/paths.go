package mssql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

const PlatformWindows = "windows"

// Layout holds resolved MSSQL paths on target.
type Layout struct {
	Instance          string
	Port              int
	UseSQLDefaults    bool   // true when no data path flags: setup.exe uses Program Files data defaults
	UseProgramCustom  bool   // true when --mssql-program-dir or --mssql-instance-dir specified
	AdminBase         string // yinstall artifacts under -R/yinstall/{instance}
	DatabaseRoot      string
	Base              string // SQL instance data root ({DatabaseRoot}/{Instance})
	DataDir           string
	LogDir            string
	BackupDir         string
	ProgramDir        string // SQL program root (Microsoft SQL Server layer)
	SharedDir         string // INSTALLSHAREDDIR ({ProgramDir}/{major*10})
	InstanceDir       string // INSTANCEDIR (instance program root)
	SetupProductMajor int
	SetupRoot         string
	SqlcmdPath        string
}

// DefaultInstance is the default SQL instance name.
const DefaultInstance = "MSSQLSERVER"

// DefaultPort is the default SQL TCP port.
const DefaultPort = 1433

// ResolveAdminBase is the yinstall operator directory under remote software dir (-R).
func ResolveAdminBase(softwareDir, instance string) string {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		instance = DefaultInstance
	}
	root := strings.TrimSpace(softwareDir)
	if root == "" {
		root = DefaultRemoteSoftwareDir()
	}
	return joinWinPath(joinWinPath(normalizeWinPath(root), "yinstall"), instance)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func pathCustomizationExplicit(dataRoot, dataDir, logDir, backupDir string) bool {
	for _, s := range []string{dataRoot, dataDir, logDir, backupDir} {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

func pathProgramCustomizationExplicit(programDir, instanceDir string) bool {
	return strings.TrimSpace(programDir) != "" || strings.TrimSpace(instanceDir) != ""
}

// InstanceDataRoot returns {databaseRoot}/{instance} for custom database layout.
func InstanceDataRoot(databaseRoot, instance string) string {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		instance = DefaultInstance
	}
	return joinWinPath(normalizeWinPath(databaseRoot), instance)
}

// DefaultSharedDirUnderProgram returns {programDir}/{major*10} for INSTALLSHAREDDIR.
func DefaultSharedDirUnderProgram(programDir string, major int) string {
	programDir = normalizeWinPath(programDir)
	if programDir == "" || major <= 0 {
		return ""
	}
	return joinWinPath(programDir, ToolsRegKeyFromMajor(major))
}

// DefaultInstanceDirUnderProgram returns {programDir}/MSSQL{major}.{instance} for INSTANCEDIR.
func DefaultInstanceDirUnderProgram(programDir string, major int, instance string) string {
	programDir = normalizeWinPath(programDir)
	instance = strings.TrimSpace(instance)
	if instance == "" {
		instance = DefaultInstance
	}
	if programDir == "" || major <= 0 {
		return ""
	}
	return joinWinPath(programDir, fmt.Sprintf("MSSQL%d.%s", major, instance))
}

func resolvePathOverride(custom, defaultPath string) string {
	if strings.TrimSpace(custom) != "" {
		return normalizeWinPath(custom)
	}
	return defaultPath
}

// InstanceDataRootFromCtx returns yinstall admin base (AdminBase), not SQL instance root.
func InstanceDataRootFromCtx(ctx *runner.StepContext) string {
	return ResolveLayoutFromContext(ctx).AdminBase
}

func winPathSameOrNested(a, b string) bool {
	a = strings.TrimRight(strings.ToLower(normalizeWinPath(a)), `\`)
	b = strings.TrimRight(strings.ToLower(normalizeWinPath(b)), `\`)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return strings.HasPrefix(a, b+`\`) || strings.HasPrefix(b, a+`\`)
}

// ValidateProgramDataPaths rejects overlapping program and data directories.
func ValidateProgramDataPaths(l Layout) error {
	if !l.UseProgramCustom {
		return nil
	}
	dataPaths := []string{l.Base, l.DataDir, l.LogDir, l.BackupDir, l.DatabaseRoot}
	programPaths := []string{l.InstanceDir, l.SharedDir, l.ProgramDir}
	for _, p := range programPaths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		for _, d := range dataPaths {
			if strings.TrimSpace(d) == "" {
				continue
			}
			if winPathSameOrNested(p, d) {
				return fmt.Errorf(
					"program path %q overlaps data path %q; use separate directories for --mssql-program-dir and --mssql-data-root",
					p, d,
				)
			}
		}
	}
	return nil
}

// ValidateProgramLayoutMajor ensures program customization can resolve INSTANCEDIR/INSTALLSHAREDDIR.
func ValidateProgramLayoutMajor(l Layout) error {
	if !l.UseProgramCustom {
		return nil
	}
	if strings.TrimSpace(l.InstanceDir) == "" {
		return fmt.Errorf(
			"cannot derive instance program directory: setup product major unknown; ensure MS-004 resolves SQL Server media or use --mssql-instance-dir",
		)
	}
	if strings.TrimSpace(l.ProgramDir) != "" && strings.TrimSpace(l.SharedDir) == "" {
		return fmt.Errorf(
			"cannot derive shared component directory: setup product major unknown; ensure MS-004 resolves SQL Server media",
		)
	}
	return nil
}

// InstanceDirs returns directories to create before install (admin base + data + program paths).
func (l Layout) InstanceDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimRight(strings.TrimSpace(p), `\`)
		if p == "" || seen[strings.ToLower(p)] {
			return
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
	}
	add(l.AdminBase)
	if !l.UseSQLDefaults {
		add(l.Base)
		add(l.DataDir)
		add(l.LogDir)
		add(l.BackupDir)
	}
	if l.UseProgramCustom {
		add(l.SharedDir)
		add(l.InstanceDir)
	}
	return out
}

func setupProductMajorFromParams(params map[string]interface{}) int {
	if params == nil {
		return 0
	}
	switch v := params["mssql_setup_product_major"].(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// ResolveLayout builds paths from CLI/step params.
func ResolveLayout(params map[string]interface{}) Layout {
	getStr := func(k, def string) string {
		if v, ok := params[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	getInt := func(k string, def int) int {
		if params == nil {
			return def
		}
		v := params[k]
		if IsPortAuto(v) {
			return def
		}
		switch x := v.(type) {
		case int:
			if x > 0 {
				return x
			}
		case int64:
			if x > 0 {
				return int(x)
			}
		case string:
			p, err := strconv.Atoi(strings.TrimSpace(x))
			if err == nil && p > 0 {
				return p
			}
		}
		return def
	}
	inst := getStr("mssql_instance", DefaultInstance)
	softwareDir := getStr("mssql_software_dir", "")
	dataRoot := firstNonEmpty(
		getStr("mssql_data_root", ""),
		getStr("mssql_database", ""),
	)
	dataDir := firstNonEmpty(
		getStr("mssql_data_dir", ""),
		getStr("mssql_data", ""),
	)
	logDir := firstNonEmpty(
		getStr("mssql_log_dir", ""),
		getStr("mssql_log", ""),
	)
	backupDir := firstNonEmpty(
		getStr("mssql_backup_dir", ""),
		getStr("mssql_backup", ""),
	)
	programDir := getStr("mssql_program_dir", "")
	instanceDir := getStr("mssql_instance_dir", "")
	major := setupProductMajorFromParams(params)
	customData := pathCustomizationExplicit(dataRoot, dataDir, logDir, backupDir)
	customProgram := pathProgramCustomizationExplicit(programDir, instanceDir)
	adminBase := ResolveAdminBase(softwareDir, inst)

	l := Layout{
		Instance:          inst,
		Port:              getInt("mssql_port", DefaultPort),
		UseSQLDefaults:    !customData,
		UseProgramCustom:  customProgram,
		AdminBase:         adminBase,
		ProgramDir:        normalizeWinPath(programDir),
		SetupProductMajor: major,
	}
	if customData {
		dbRoot := normalizeWinPath(dataRoot)
		if dbRoot == "" && strings.TrimSpace(dataDir) != "" {
			dbRoot = normalizeWinPath(dataDir)
		}
		if dbRoot == "" {
			dbRoot = adminBase
		}
		base := InstanceDataRoot(dbRoot, inst)
		l.DatabaseRoot = dbRoot
		l.Base = base
		l.DataDir = resolvePathOverride(dataDir, joinWinPath(base, "Data"))
		l.LogDir = resolvePathOverride(logDir, joinWinPath(base, "Log"))
		l.BackupDir = resolvePathOverride(backupDir, joinWinPath(base, "Backup"))
	}
	if customProgram {
		if l.ProgramDir == "" && instanceDir != "" {
			l.ProgramDir = normalizeWinPath(instanceDir)
		}
		if strings.TrimSpace(instanceDir) != "" {
			l.InstanceDir = normalizeWinPath(instanceDir)
		} else if l.ProgramDir != "" && major > 0 {
			l.InstanceDir = DefaultInstanceDirUnderProgram(l.ProgramDir, major, inst)
		}
		if l.ProgramDir != "" && major > 0 {
			l.SharedDir = DefaultSharedDirUnderProgram(l.ProgramDir, major)
		}
	}
	return l
}

// ResolveLayoutFromContext builds layout from runner step context.
func ResolveLayoutFromContext(ctx *runner.StepContext) Layout {
	if ctx == nil {
		return ResolveLayout(nil)
	}
	params := map[string]interface{}{
		"mssql_data_root":    ctx.GetParamString("mssql_data_root", ""),
		"mssql_database":     ctx.GetParamString("mssql_database", ""),
		"mssql_data_dir":     ctx.GetParamString("mssql_data_dir", ""),
		"mssql_data":         ctx.GetParamString("mssql_data", ""),
		"mssql_log_dir":      ctx.GetParamString("mssql_log_dir", ""),
		"mssql_log":          ctx.GetParamString("mssql_log", ""),
		"mssql_backup_dir":   ctx.GetParamString("mssql_backup_dir", ""),
		"mssql_backup":       ctx.GetParamString("mssql_backup", ""),
		"mssql_program_dir":  ctx.GetParamString("mssql_program_dir", ""),
		"mssql_instance_dir": ctx.GetParamString("mssql_instance_dir", ""),
		"mssql_software_dir": strings.TrimSpace(ctx.RemoteSoftwareDir),
		"mssql_instance":     LayoutInstanceName(ctx),
		"mssql_port":         ctx.GetParam("mssql_port"),
	}
	if v, ok := ctx.Results["mssql_setup_product_major"]; ok {
		params["mssql_setup_product_major"] = v
	}
	return ResolveLayout(params)
}
