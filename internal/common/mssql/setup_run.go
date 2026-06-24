package mssql

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	"github.com/yinstall/internal/winrm"
)

// WinRMServiceWaitTimeout covers MS-008 post-install service polling over WinRM.
const WinRMServiceWaitTimeout = 90 * time.Minute

// RunSetupInstance runs setup.exe and blocks until the process exits (-Wait).
func RunSetupInstance(ctx *runner.StepContext, setupExe, iniPath string, quiet bool) error {
	if ctx.DryRun || ctx.Precheck {
		ctx.Logger.Info("MS-008 dry-run/precheck: skip setup.exe")
		return nil
	}
	setupExe = normalizeWinPath(setupExe)
	iniPath = normalizeWinPath(iniPath)

	if usesWinRMExecutor(ctx) {
		if !quiet {
			ctx.Logger.Warn("MS-008: WinRM remote install requires silent setup (/Q); ignoring mssql-setup-quiet=false")
			quiet = true
		}
		ctx.SetExecuteTimeout(winrm.SetupExecuteTimeout)
		defer ctx.SetExecuteTimeout(0)
	}

	saPassword := strings.TrimSpace(ctx.GetParamString("mssql_sa_password", ""))
	waitPS := buildWaitSetupPS(setupExe, iniPath, quiet, saPassword)
	ctx.LogScriptPreview("powershell", "MS-008 setup.exe -Wait", waitPS)
	res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+waitPS+`"`, false)
	if err != nil {
		return fmt.Errorf("setup.exe: %w", err)
	}
	exitCode := 0
	if res != nil {
		exitCode = normalizeSetupExitCode(res.GetExitCode())
	}
	ctx.Logger.Info("MS-008: setup.exe finished (exit=%d)", exitCode)
	if exitCode == 3010 {
		if rebootRequired, err := setupExit3010NeedsReboot(ctx); err != nil {
			return fmt.Errorf("setup.exe exit 3010: %w", err)
		} else if rebootRequired {
			return fmt.Errorf("setup.exe exit 3010: 目标机需要重启后才能安装 SQL Server，请重启后重跑 MS-008")
		}
	}
	if exitCode != 0 && exitCode != 3010 {
		return fmt.Errorf("%s", SetupExitCodeError(exitCode))
	}
	return nil
}

// normalizeSetupExitCode maps WinRM unsigned/HRESULT-style codes to setup.exe exit code.
func normalizeSetupExitCode(code int) int {
	u := uint32(code)
	if u > 0x7fffffff {
		code = int(int32(u))
	}
	if low := int(uint16(uint32(code) & 0xffff)); low == 3010 {
		return 3010
	}
	return code
}

func setupExit3010NeedsReboot(ctx *runner.StepContext) (bool, error) {
	inst := LayoutInstanceName(ctx)
	svc := "MSSQLSERVER"
	if !strings.EqualFold(inst, DefaultInstance) {
		svc = "MSSQL$" + inst
	}
	q := strings.ReplaceAll(svc, `'`, `''`)
	script := fmt.Sprintf(`$s=Get-Service -Name '%s' -ErrorAction SilentlyContinue; if ($s) { 'installed' } else { 'absent' }`, q)
	res, err := ctx.Execute(`powershell -NoProfile -Command "`+script+`"`, false)
	if err != nil {
		return false, err
	}
	if res != nil && strings.Contains(res.GetStdout(), "installed") {
		return false, nil
	}
	return true, nil
}

func UsesWinRMExecutor(ctx *runner.StepContext) bool {
	return usesWinRMExecutor(ctx)
}

func usesWinRMExecutor(ctx *runner.StepContext) bool {
	if ctx == nil || ctx.Executor == nil {
		return false
	}
	return isWinRMUnderlying(ctx.Executor)
}

func isWinRMUnderlying(ex runner.Executor) bool {
	if ex == nil {
		return false
	}
	u, ok := ex.(interface{ SSHExecutor() ssh.Executor })
	if !ok {
		return false
	}
	_, ok = u.SSHExecutor().(*winrm.Executor)
	return ok
}

func buildWaitSetupPS(setupExe, iniPath string, quiet bool, saPassword string) string {
	qExe := strings.ReplaceAll(setupExe, `'`, `''`)
	qIni := strings.ReplaceAll(iniPath, `'`, `''`)
	quietFlag := ""
	if quiet {
		quietFlag = "'/Q',"
	}
	saArg := ""
	if pwd := strings.TrimSpace(saPassword); pwd != "" {
		qPwd := strings.ReplaceAll(pwd, `'`, `''`)
		saArg = fmt.Sprintf(`$pwd='%s'; $setupArgs+=('/SAPWD="'+$pwd+'"'); `, qPwd)
	}
	return fmt.Sprintf(
		`$exe='%s'; $ini='%s'; $setupArgs=@('/ConfigurationFile='+$ini,%s'/IACCEPTSQLSERVERLICENSETERMS'); %s`+
			`$p=Start-Process -FilePath $exe -ArgumentList $setupArgs -Wait -PassThru -WindowStyle Hidden; `+
			`if (-not $p) { throw 'Start-Process failed' }; exit $p.ExitCode`,
		qExe, qIni, quietFlag, saArg,
	)
}
