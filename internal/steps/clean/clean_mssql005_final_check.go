package clean

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepCleanMssql005FinalCheck() *runner.Step {
	return &runner.Step{
		Name:        "MSSQL Cleanup Final Check",
		Description: "Verify service stopped and paths removed per stage",
		Tags:        []string{"clean", "mssql"},
		PostCheck: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			layout := mssqlCleanLayout(ctx)
			stage := mssqlCleanStageFromCtx(ctx)
			svc := mssqlServiceName(layout.Instance)
			res, _ := ctx.Execute(`powershell -NoProfile -Command "(Get-Service -Name '`+strings.ReplaceAll(svc, `'`, `''`)+`' -ErrorAction SilentlyContinue).Status"`, false)
			if res != nil && strings.TrimSpace(res.GetStdout()) == "Running" {
				return fmt.Errorf("service %s still running", svc)
			}
			for _, p := range commonmssql.CleanRemovePaths(stage, layout) {
				p = strings.TrimRight(strings.TrimSpace(p), `\`)
				if p == "" {
					continue
				}
				res2, _ := ctx.Execute(`powershell -NoProfile -Command "if (Test-Path -LiteralPath '`+strings.ReplaceAll(p, `'`, `''`)+`') { 'exists' } else { 'gone' }"`, false)
				if res2 != nil && strings.Contains(res2.GetStdout(), "exists") {
					return fmt.Errorf("path still exists: %s", p)
				}
			}
			if stage == commonmssql.StageSoftware {
				for _, dir := range commonmssql.CleanPreservePackageDirs(commonmssql.RemoteSoftwareDir(ctx)) {
					res3, _ := ctx.Execute(`powershell -NoProfile -Command "if (Test-Path -LiteralPath '`+strings.ReplaceAll(dir, `'`, `''`)+`') { 'kept' } else { 'missing' }"`, false)
					if res3 != nil && strings.Contains(res3.GetStdout(), "missing") {
						ctx.Logger.Warn("CLEAN-MSSQL-005: package dir %s not found (ISO may need re-upload)", dir)
					} else {
						ctx.Logger.Info("CLEAN-MSSQL-005: preserved package dir %s", dir)
					}
				}
			}
			return nil
		},
	}
}
