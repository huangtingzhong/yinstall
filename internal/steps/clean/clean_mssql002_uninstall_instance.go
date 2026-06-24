package clean

import (
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepCleanMssql002UninstallInstance() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-MSSQL-002",
		Name:        "Uninstall SQL Instance",
		Description: "Run setup.exe /Action=Uninstall for the instance",
		Tags:        []string{"clean", "mssql"},
		Dangerous:   true,
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			layout := mssqlCleanLayout(ctx)
			svc := mssqlServiceName(layout.Instance)
			res, _ := ctx.Execute(`powershell -NoProfile -Command "(Get-Service -Name '`+strings.ReplaceAll(svc, `'`, `''`)+`' -ErrorAction SilentlyContinue).Name"`, false)
			if res == nil || strings.TrimSpace(res.GetStdout()) == "" {
				return runner.NewStepSkippedError("SQL instance service not installed")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			layout := mssqlCleanLayout(ctx)
			if ctx.DryRun || ctx.Precheck {
				ctx.Logger.Info("CLEAN-MSSQL-002 dry-run/precheck: would uninstall instance %s", layout.Instance)
				return nil
			}
			setupExe, err := commonmssql.ResolveUninstallSetupExe(ctx)
			if err != nil {
				ctx.Logger.Warn("CLEAN-MSSQL-002: %v; skip uninstall (remove data dirs only)", err)
				return nil
			}
			if err := commonmssql.RunUninstallInstance(ctx, setupExe, layout.Instance, true); err != nil {
				ctx.Logger.Warn("CLEAN-MSSQL-002: setup uninstall failed: %v (continuing with directory cleanup)", err)
				return nil
			}
			ctx.Logger.Info("CLEAN-MSSQL-002: uninstall completed for %s", layout.Instance)
			return nil
		},
	}
}
