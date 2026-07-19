package mssql

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepDistributeSetupMedia() *runner.Step {
	return &runner.Step{
		Name:     "Distribute Setup Media",
		Tags:     []string{"mssql", "mssql-software", "mssql-instance"},
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetParamString("mssql_setup_unc", "") != "" {
				return runner.NewStepSkippedError("UNC media configured")
			}
			if root, ok := commonmssql.ReadySetupRoot(ctx); ok {
				if commonmssql.RemoteSetupExeExists(ctx, root) {
					ctx.SetResult("mssql_setup_root", root)
					return runner.NewStepSkippedError("setup media already available at " + root)
				}
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mssqlLogPhase(ctx, "upload-setup-start", "")
			root, err := commonmssql.EnsureSetupMediaOnTarget(ctx)
			if err != nil {
				return err
			}
			ctx.SetResult("mssql_setup_root", root)
			mssqlLogPhase(ctx, "upload-setup-done", root)
			return nil
		},
	}
}
