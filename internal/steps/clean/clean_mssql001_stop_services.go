package clean

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepCleanMssql001StopServices() *runner.Step {
	return &runner.Step{
		Name:        "Stop MSSQL Services",
		Description: "Stop SQL Server and SQL Agent Windows services",
		Tags:        []string{"clean", "mssql"},
		Dangerous:   true,
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetTargetPlatform() != "" && ctx.GetTargetPlatform() != commonmssql.PlatformWindows {
				return fmt.Errorf("mssql cleanup requires Windows target")
			}
			if _, err := commonmssql.EnsureInstanceResolved(ctx); err != nil {
				return err
			}
			layout := mssqlCleanLayout(ctx)
			if commonmssql.LayoutPathParamsExplicitFromContext(ctx) {
				ctx.Logger.Info("MSSQL cleanup: stage=%s port=%d admin=%s instance=%s (paths from CLI)",
					mssqlCleanStageFromCtx(ctx), layout.Port, layout.AdminBase, layout.Instance)
			} else {
				ctx.Logger.Info("MSSQL cleanup: stage=%s port=%d admin=%s instance=%s base=%s data=%s log=%s backup=%s (paths from registry)",
					mssqlCleanStageFromCtx(ctx), layout.Port, layout.AdminBase, layout.Instance,
					layout.Base, layout.DataDir, layout.LogDir, layout.BackupDir)
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			layout := mssqlCleanLayout(ctx)
			svc := mssqlServiceName(layout.Instance)
			agent := mssqlAgentServiceName(layout.Instance)
			script := fmt.Sprintf(
				`foreach ($n in @('%s','%s')) { Stop-Service -Name $n -Force -ErrorAction SilentlyContinue }; `+
					`$s=Get-Service -Name '%s' -ErrorAction SilentlyContinue; if ($s) { $s.Status.ToString() } else { 'absent' }`,
				strings.ReplaceAll(agent, `'`, `''`), strings.ReplaceAll(svc, `'`, `''`), strings.ReplaceAll(svc, `'`, `''`))
			ctx.LogScriptPreview("powershell", "stop mssql services", script)
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			res, err := ctx.Execute(`powershell -NoProfile -Command "`+script+`"`, false)
			if err != nil {
				return err
			}
			if res != nil && strings.Contains(strings.ToLower(res.GetStdout()), "running") {
				return fmt.Errorf("service %s still running", svc)
			}
			ctx.Logger.Info("MSSQL services stopped or absent for instance %s", layout.Instance)
			return nil
		},
	}
}
