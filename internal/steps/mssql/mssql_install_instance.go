package mssql

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepInstallInstance() *runner.Step {
	return &runner.Step{
		Name: "Install Instance",
		Tags: []string{"mssql", "mssql-instance"},
		PreCheck: func(ctx *runner.StepContext) error {
			if err := commonmssql.PhaseB1Complete(ctx); err != nil {
				return err
			}
			return commonmssql.EnsureSetupLocaleCompatible(ctx)
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				ctx.Logger.Info("MS-008 dry-run/precheck: skip setup.exe")
				return nil
			}
			setupExe, iniPath, err := commonmssql.ResolveSetupExeAndINI(ctx)
			if err != nil {
				return err
			}
			if err := commonmssql.RunSetupInstance(ctx, setupExe, iniPath, ctx.GetParamBool("mssql_setup_quiet", true)); err != nil {
				return err
			}
			inst := commonmssql.ResolvedInstanceName(ctx)
			svc := "MSSQLSERVER"
			if !strings.EqualFold(inst, commonmssql.DefaultInstance) {
				svc = "MSSQL$" + inst
			}
			waitScript := fmt.Sprintf(`$svc='%s'; $deadline=(Get-Date).AddMinutes(90); while ((Get-Date) -lt $deadline) { $s=Get-Service -Name $svc -ErrorAction SilentlyContinue; if ($s -and $s.Status -eq 'Running') { 'running'; exit 0 }; Start-Sleep -Seconds 15 }; throw \"SQL service $svc not running within 90 minutes\"`, strings.ReplaceAll(svc, "'", "''"))
			ctx.LogScriptPreview("powershell", "MS-008 wait service", waitScript)
			if _, ok := ctx.Executor.(runner.ExecuteTimeoutSetter); ok {
				ctx.SetExecuteTimeout(commonmssql.WinRMServiceWaitTimeout)
				defer ctx.SetExecuteTimeout(0)
			}
			if _, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+waitScript+`"`, false); err != nil {
				return fmt.Errorf("wait SQL service: %w", err)
			}
			if err := commonmssql.WaitForInstanceReadyAfterInstall(ctx, 0); err != nil {
				return fmt.Errorf("wait SQL instance ready: %w", err)
			}
			ctx.SetResult("mssql_service_name", commonmssql.DefaultSQLSvcAccount(inst))
			return nil
		},
	}
}
