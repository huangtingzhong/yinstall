package mssql

import (
	"fmt"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepWriteSetupSoftwareEnv() *runner.Step {
	return &runner.Step{
		Name: "Write Setup Software Env",
		Tags: []string{"mssql", "mssql-software", "env"},
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			if _, ok := commonmssql.ReadySetupRoot(ctx); !ok {
				return fmt.Errorf("setup media root not ready; run MS-004/MS-006 first")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			root, _ := ctx.Results["mssql_setup_root"].(string)
			if root == "" {
				if r, ok := commonmssql.ReadySetupRoot(ctx); ok {
					root = r
				}
			}
			if root == "" {
				root = ctx.GetParamString("mssql_setup_unc", "")
			}
			if root == "" {
				return fmt.Errorf("mssql_setup_root not set")
			}
			return commonmssql.WriteSetupSoftwareEnv(ctx, root)
		},
	}
}
