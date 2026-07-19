package mssql

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepApplyCuSp() *runner.Step {
	return &runner.Step{
		Name:     "Apply CU/SP",
		Tags:     []string{"mssql", "mssql-software"},
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetParamString("mssql_cu_package", "") == "" {
				return runner.NewStepSkippedError("no CU package")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			pkg := ctx.GetParamString("mssql_cu_package", "")
			base := commonmssql.InstanceDataRootFromCtx(ctx)
			staging := base + `\cu`
			mssqlLogPhase(ctx, "cu-upload-start", staging)
			if ctx.DryRun || ctx.Precheck {
				ctx.LogScriptPreview("shell", "MS-011 CU patch", commonmssql.PatchCommand(staging+`\setup.exe`, ctx.GetParamBool("mssql_setup_quiet", true)))
				return nil
			}
			setupExe, err := commonmssql.DistributeCUPackage(ctx, pkg, staging)
			if err != nil {
				return err
			}
			cmd := commonmssql.PatchCommand(setupExe, ctx.GetParamBool("mssql_setup_quiet", true))
			ctx.LogScriptPreview("shell", "MS-011 CU patch", cmd)
			if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
				return err
			}
			mssqlLogPhase(ctx, "cu-upload-done", setupExe)
			return nil
		},
	}
}
