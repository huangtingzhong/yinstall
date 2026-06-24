package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/winrm"
)

// DiscoverBootstrapSetupExe locates setup.exe for the given instance under Setup Bootstrap\Release.
func DiscoverBootstrapSetupExe(ctx *runner.StepContext, instance string) (string, error) {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		instance = DefaultInstance
	}
	if ctx.DryRun || ctx.Precheck {
		return `C:\Program Files\Microsoft SQL Server\130\Setup Bootstrap\Release\setup.exe`, nil
	}
	qInst := strings.ReplaceAll(instance, `'`, `''`)
	script := fmt.Sprintf(
		`$inst='%s'; $names=Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\Instance Names\SQL' -ErrorAction Stop; `+
			`$instId=$names.$inst; if (-not $instId) { throw \"instance $inst not in registry\" }; `+
			`if ($instId -match '^MSSQL(\d+)') { $maj=$Matches[1] } else { throw \"cannot parse instance id $instId\" }; `+
			`$map=@{ '16'='160'; '15'='150'; '14'='140'; '13'='130'; '12'='110'; '11'='100'; '10'='100' }; `+
			`$folder=$map[$maj]; if (-not $folder) { throw \"unsupported SQL major $maj\" }; `+
			`$bootstrap=Join-Path 'C:\Program Files\Microsoft SQL Server' (Join-Path $folder 'Setup Bootstrap'); `+
			`$setup=Get-ChildItem -LiteralPath $bootstrap -Recurse -Filter setup.exe -ErrorAction SilentlyContinue | Select-Object -First 1; `+
			`if (-not $setup) { throw \"setup.exe missing under $bootstrap\" }; Write-Output $setup.FullName`,
		qInst,
	)
	res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false)
	if err != nil {
		return "", fmt.Errorf("discover bootstrap setup.exe: %w", err)
	}
	path := strings.TrimSpace(res.GetStdout())
	if path == "" {
		return "", fmt.Errorf("empty bootstrap setup.exe path")
	}
	return normalizeWinPath(path), nil
}

// ResolveUninstallSetupExe returns setup.exe for uninstall (bootstrap for installed instance, else media root).
func ResolveUninstallSetupExe(ctx *runner.StepContext) (string, error) {
	instance := ctx.GetParamString("mssql_instance", DefaultInstance)
	if path, err := DiscoverBootstrapSetupExe(ctx, instance); err == nil {
		return path, nil
	}
	if root, ok := ReadySetupRoot(ctx); ok {
		return joinWinPath(root, "setup.exe"), nil
	}
	return DiscoverBootstrapSetupExe(ctx, instance)
}

// RunUninstallInstance runs setup.exe /Action=Uninstall and blocks until exit (-Wait).
func RunUninstallInstance(ctx *runner.StepContext, setupExe, instance string, quiet bool) error {
	if ctx.DryRun || ctx.Precheck {
		ctx.Logger.Info("CLEAN-MSSQL-002 dry-run/precheck: skip setup.exe uninstall")
		return nil
	}
	setupExe = normalizeWinPath(setupExe)
	instance = strings.TrimSpace(instance)
	if instance == "" {
		instance = DefaultInstance
	}
	waitPS := buildWaitUninstallPS(setupExe, instance, quiet)
	ctx.LogScriptPreview("powershell", "CLEAN-MSSQL-002 setup.exe uninstall -Wait", waitPS)
	if _, ok := ctx.Executor.(runner.ExecuteTimeoutSetter); ok {
		ctx.SetExecuteTimeout(winrm.SetupExecuteTimeout)
		defer ctx.SetExecuteTimeout(0)
	}
	res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+waitPS+`"`, false)
	if err != nil {
		return fmt.Errorf("setup.exe uninstall: %w", err)
	}
	exitCode := 0
	if res != nil {
		exitCode = normalizeSetupExitCode(res.GetExitCode())
	}
	ctx.Logger.Info("CLEAN-MSSQL-002: setup.exe uninstall finished (exit=%d)", exitCode)
	if exitCode != 0 && exitCode != 3010 {
		return fmt.Errorf("setup.exe uninstall exit code %d", exitCode)
	}
	return nil
}

func buildWaitUninstallPS(setupExe, instance string, quiet bool) string {
	qExe := strings.ReplaceAll(setupExe, `'`, `''`)
	qInst := strings.ReplaceAll(instance, `'`, `''`)
	quietFlag := ""
	if quiet {
		quietFlag = "'/Q',"
	}
	return fmt.Sprintf(
		`$exe='%s'; $setupArgs=@('/Action=Uninstall','/FEATURES=SQL','/INSTANCENAME=%s',%s'/IACCEPTSQLSERVERLICENSETERMS'); `+
			`$p=Start-Process -FilePath $exe -ArgumentList $setupArgs -Wait -PassThru -WindowStyle Hidden; `+
			`if (-not $p) { throw 'Start-Process failed' }; exit $p.ExitCode`,
		qExe, qInst, quietFlag,
	)
}

// BuildUninstallCommand builds setup.exe uninstall command line (direct invocation; prefer RunUninstallInstance).
func BuildUninstallCommand(setupExe, instance string, quiet bool) string {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		instance = DefaultInstance
	}
	flags := fmt.Sprintf(`/Action=Uninstall /FEATURES=SQL /INSTANCENAME=%s /IACCEPTSQLSERVERLICENSETERMS`, instance)
	if quiet {
		flags += " /Q"
	}
	return fmt.Sprintf(`"%s" %s`, setupExe, flags)
}

// CleanOperatorArtifacts removes yinstall env files and machine-level YINSTALL_* variables.
func CleanOperatorArtifacts(ctx *runner.StepContext, base string, port int) error {
	return cleanOperatorArtifacts(ctx, base, port, true, true)
}

// ClearSetupMachineEnv removes YINSTALL_MSSQL_SETUP_ROOT from machine scope.
func ClearSetupMachineEnv(ctx *runner.StepContext) error {
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	qn := strings.ReplaceAll(machineEnvSetup, `'`, `''`)
	_, err := ctx.Execute(`powershell -NoProfile -Command "[Environment]::SetEnvironmentVariable('`+qn+`',$null,'Machine')"`, false)
	return err
}

func cleanOperatorArtifacts(ctx *runner.StepContext, base string, port int, removeSetupEnv, clearMachineTools bool) error {
	base = normalizeWinPath(base)
	profile, err := ResolveInstanceProfilePath(ctx, port)
	if err != nil && !ctx.DryRun && !ctx.Precheck {
		return err
	}
	var files []string
	if removeSetupEnv {
		files = append(files, joinWinPath(base, setupEnvFileName))
	}
	files = append(files, joinWinPath(base, toolsEnvFileName))
	if profile != "" {
		files = append(files, profile)
	}
	ctx.LogScriptPreview("powershell", "MSSQL clean env artifacts", strings.Join(files, "; "))
	if ctx.DryRun || ctx.Precheck {
		return nil
	}
	for _, f := range files {
		q := strings.ReplaceAll(f, `'`, `''`)
		_, _ = ctx.Execute(`powershell -NoProfile -Command "if (Test-Path -LiteralPath '`+q+`') { Remove-Item -LiteralPath '`+q+`' -Force -ErrorAction SilentlyContinue }"`, false)
	}
	if removeSetupEnv {
		qn := strings.ReplaceAll(machineEnvSetup, `'`, `''`)
		_, _ = ctx.Execute(`powershell -NoProfile -Command "[Environment]::SetEnvironmentVariable('`+qn+`',$null,'Machine')"`, false)
	}
	if clearMachineTools {
		for _, name := range []string{machineEnvSQLCmd, machineEnvToolsBin} {
			qn := strings.ReplaceAll(name, `'`, `''`)
			_, _ = ctx.Execute(`powershell -NoProfile -Command "[Environment]::SetEnvironmentVariable('`+qn+`',$null,'Machine')"`, false)
		}
	}
	return nil
}
