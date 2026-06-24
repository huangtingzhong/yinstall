package mssql

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

const (
	toolsEnvFileName   = "yinstall_mssql_tools.ps1"
	setupEnvFileName   = "yinstall_mssql_setup.ps1"
	machineEnvSetup    = "YINSTALL_MSSQL_SETUP_ROOT"
	machineEnvSQLCmd   = "YINSTALL_SQLCMD"
	machineEnvToolsBin = "YINSTALL_MSSQL_TOOLS_BIN"
)

// InstanceProfile holds instance connection metadata for operator env files.
type InstanceProfile struct {
	Port        int
	Instance    string
	Server      string
	Base        string
	DataDir     string
	LogDir      string
	BackupDir   string
	ServiceName string
	SQLCmdPath  string
	ToolsEnv    string
	SetupEnv    string
}

// BuildInstanceProfile assembles profile fields from step context.
func BuildInstanceProfile(ctx *runner.StepContext) InstanceProfile {
	layout := ResolveLayoutFromContext(ctx)
	if layout.UseSQLDefaults && !ctx.DryRun && !ctx.Precheck {
		if enriched, err := EnrichLayoutWithInstalledPaths(ctx, layout); err == nil {
			layout = enriched
		}
	}
	inst := layout.Instance
	port := layout.Port
	svc := "MSSQLSERVER"
	if !strings.EqualFold(inst, DefaultInstance) {
		svc = "MSSQL$" + inst
	}
	sqlcmd := SqlcmdBinary(ctx)
	adminBase := layout.AdminBase
	return InstanceProfile{
		Port:        port,
		Instance:    inst,
		Server:      SqlcmdServerTarget("localhost", port),
		Base:        layout.Base,
		DataDir:     layout.DataDir,
		LogDir:      layout.LogDir,
		BackupDir:   layout.BackupDir,
		ServiceName: svc,
		SQLCmdPath:  sqlcmd,
		ToolsEnv:    joinWinPath(adminBase, toolsEnvFileName),
		SetupEnv:    joinWinPath(adminBase, setupEnvFileName),
	}
}

// ResolveInstanceProfilePath returns user-home instance env file (default ~\{port}.ps1 on Windows).
func ResolveInstanceProfilePath(ctx *runner.StepContext, port int) (string, error) {
	if raw := strings.TrimSpace(ctx.GetParamString("mssql_env_file", "")); raw != "" {
		if strings.HasPrefix(raw, "~") {
			home, err := WindowsUserProfile(ctx)
			if err != nil {
				return "", err
			}
			suffix := strings.TrimPrefix(raw, "~")
			suffix = strings.TrimPrefix(suffix, `/`)
			suffix = strings.TrimPrefix(suffix, `\`)
			return joinWinPath(home, suffix), nil
		}
		return raw, nil
	}
	home, err := WindowsUserProfile(ctx)
	if err != nil {
		return "", err
	}
	return joinWinPath(home, strconv.Itoa(port)+".ps1"), nil
}

// WindowsUserProfile returns the target user's profile directory.
func WindowsUserProfile(ctx *runner.StepContext) (string, error) {
	if ctx.DryRun || ctx.Precheck {
		return `C:\Users\Administrator`, nil
	}
	res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "[Environment]::GetFolderPath('UserProfile')"`, false)
	if err != nil {
		return "", fmt.Errorf("resolve user profile: %w", err)
	}
	home := strings.TrimSpace(res.GetStdout())
	if home == "" {
		return "", fmt.Errorf("empty user profile path")
	}
	return normalizeWinPath(home), nil
}

// RenderSetupSoftwareEnvPS writes setup media root for dot-sourcing.
func RenderSetupSoftwareEnvPS(setupRoot string) string {
	setupRoot = strings.ReplaceAll(normalizeWinPath(setupRoot), `'`, `''`)
	return fmt.Sprintf(`$env:%s = '%s'
`, machineEnvSetup, setupRoot)
}

// RenderToolsEnvPS writes sqlcmd/tools paths for dot-sourcing.
func RenderToolsEnvPS(sqlcmdPath string) string {
	sqlcmdPath = strings.ReplaceAll(normalizeWinPath(sqlcmdPath), `'`, `''`)
	toolsBin := strings.ReplaceAll(normalizeWinPath(filepath.Dir(sqlcmdPath)), `'`, `''`)
	return fmt.Sprintf(`$env:%s = '%s'
$env:%s = '%s'
`, machineEnvSQLCmd, sqlcmdPath, machineEnvToolsBin, toolsBin)
}

// RenderInstanceProfilePS writes per-instance operator env file named by port.
func RenderInstanceProfilePS(p InstanceProfile) string {
	q := func(s string) string { return strings.ReplaceAll(s, `'`, `''`) }
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# yinstall MSSQL instance profile (port %d)\n", p.Port))
	b.WriteString(fmt.Sprintf("$env:YINSTALL_MSSQL_PORT = '%d'\n", p.Port))
	b.WriteString(fmt.Sprintf("$env:YINSTALL_MSSQL_INSTANCE = '%s'\n", q(p.Instance)))
	b.WriteString(fmt.Sprintf("$env:YINSTALL_MSSQL_SERVER = '%s'\n", q(p.Server)))
	b.WriteString(fmt.Sprintf("$env:YINSTALL_MSSQL_BASE = '%s'\n", q(p.Base)))
	b.WriteString(fmt.Sprintf("$env:YINSTALL_MSSQL_DATA = '%s'\n", q(p.DataDir)))
	b.WriteString(fmt.Sprintf("$env:YINSTALL_MSSQL_LOG = '%s'\n", q(p.LogDir)))
	b.WriteString(fmt.Sprintf("$env:YINSTALL_MSSQL_BACKUP = '%s'\n", q(p.BackupDir)))
	b.WriteString(fmt.Sprintf("$env:YINSTALL_MSSQL_SERVICE = '%s'\n", q(p.ServiceName)))
	if p.SQLCmdPath != "" {
		b.WriteString(fmt.Sprintf("$env:YINSTALL_SQLCMD = '%s'\n", q(p.SQLCmdPath)))
	}
	if p.ToolsEnv != "" {
		b.WriteString(fmt.Sprintf("if (Test-Path -LiteralPath '%s') { . '%s' }\n", q(p.ToolsEnv), q(p.ToolsEnv)))
	}
	if p.SetupEnv != "" {
		b.WriteString(fmt.Sprintf("if (Test-Path -LiteralPath '%s') { . '%s' }\n", q(p.SetupEnv), q(p.SetupEnv)))
	}
	return b.String()
}

// SetMachineEnvironmentVariable sets a machine-scoped environment variable on Windows.
func SetMachineEnvironmentVariable(ctx *runner.StepContext, name, value string) error {
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	qn := strings.ReplaceAll(name, `'`, `''`)
	qv := strings.ReplaceAll(value, `'`, `''`)
	script := fmt.Sprintf(`[Environment]::SetEnvironmentVariable('%s','%s','Machine')`, qn, qv)
	_, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false)
	return err
}

// AppendMachinePath adds a directory to the machine PATH if missing.
func AppendMachinePath(ctx *runner.StepContext, dir string) error {
	dir = normalizeWinPath(dir)
	if dir == "" {
		return nil
	}
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	q := strings.ReplaceAll(dir, `'`, `''`)
	script := fmt.Sprintf(
		`$dir='%s'; $cur=[Environment]::GetEnvironmentVariable('Path','Machine'); `+
			`if ($cur -and ($cur -split ';' | Where-Object { $_.TrimEnd('\') -ieq $dir.TrimEnd('\') })) { exit 0 }; `+
			`if ($cur) { $new=$cur.TrimEnd(';')+';'+$dir } else { $new=$dir }; `+
			`[Environment]::SetEnvironmentVariable('Path',$new,'Machine')`,
		q,
	)
	_, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false)
	return err
}

// WriteSetupSoftwareEnv records setup media root in machine env and base ps1 file.
func WriteSetupSoftwareEnv(ctx *runner.StepContext, setupRoot string) error {
	setupRoot = normalizeWinPath(setupRoot)
	if setupRoot == "" {
		return fmt.Errorf("setup root empty")
	}
	base := InstanceDataRootFromCtx(ctx)
	content := RenderSetupSoftwareEnvPS(setupRoot)
	envPath := joinWinPath(base, setupEnvFileName)
	ctx.LogScriptPreview("powershell", "MSSQL setup env", content)
	if ctx.DryRun || ctx.Precheck {
		ctx.SetResult("mssql_setup_env_path", envPath)
		return nil
	}
	if err := SetMachineEnvironmentVariable(ctx, machineEnvSetup, setupRoot); err != nil {
		return fmt.Errorf("set %s: %w", machineEnvSetup, err)
	}
	if err := writeRemoteText(ctx, envPath, content); err != nil {
		return err
	}
	ctx.SetResult("mssql_setup_env_path", envPath)
	ctx.Logger.Info("MSSQL setup env: %s=%s (file %s)", machineEnvSetup, setupRoot, envPath)
	return nil
}

// WriteSQLToolsEnv discovers sqlcmd, updates machine PATH and tools ps1 file.
func WriteSQLToolsEnv(ctx *runner.StepContext) error {
	sqlcmd, err := DiscoverSqlcmdPath(ctx)
	if err != nil {
		return err
	}
	toolsBin := normalizeWinPath(filepath.Dir(sqlcmd))
	base := InstanceDataRootFromCtx(ctx)
	content := RenderToolsEnvPS(sqlcmd)
	envPath := joinWinPath(base, toolsEnvFileName)
	ctx.LogScriptPreview("powershell", "MSSQL tools env", content)
	if ctx.DryRun || ctx.Precheck {
		ctx.SetResult("mssql_tools_env_path", envPath)
		return nil
	}
	if err := AppendMachinePath(ctx, toolsBin); err != nil {
		return fmt.Errorf("append tools bin to PATH: %w", err)
	}
	if err := SetMachineEnvironmentVariable(ctx, machineEnvSQLCmd, sqlcmd); err != nil {
		return fmt.Errorf("set %s: %w", machineEnvSQLCmd, err)
	}
	if err := SetMachineEnvironmentVariable(ctx, machineEnvToolsBin, toolsBin); err != nil {
		return fmt.Errorf("set %s: %w", machineEnvToolsBin, err)
	}
	if err := writeRemoteText(ctx, envPath, content); err != nil {
		return err
	}
	ctx.SetResult("mssql_tools_env_path", envPath)
	ctx.Logger.Info("MSSQL tools env: PATH+= %s (file %s)", toolsBin, envPath)
	return nil
}

// WriteInstanceProfileEnv writes ~/{port}.ps1 for the operator user.
func WriteInstanceProfileEnv(ctx *runner.StepContext) error {
	profile := BuildInstanceProfile(ctx)
	path, err := ResolveInstanceProfilePath(ctx, profile.Port)
	if err != nil {
		return err
	}
	content := RenderInstanceProfilePS(profile)
	ctx.LogScriptPreview("powershell", "MSSQL instance profile", content)
	if ctx.DryRun || ctx.Precheck {
		ctx.SetResult("mssql_instance_env_path", path)
		return nil
	}
	if err := writeRemoteText(ctx, path, content); err != nil {
		return err
	}
	ctx.SetResult("mssql_instance_env_path", path)
	ctx.Logger.Info("MSSQL instance profile: %s (port %d)", path, profile.Port)
	return nil
}

func writeRemoteText(ctx *runner.StepContext, path, content string) error {
	return commonfile.RemoteWriteTextFile(ctx, path, content, false)
}
