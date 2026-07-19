package clean

import (
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepCleanMssql003RemoveDirectories() *runner.Step {
	return &runner.Step{
		Name:        "Remove MSSQL Directories",
		Description: "Remove SQL instance data or software paths per --stage",
		Tags:        []string{"clean", "mssql"},
		Dangerous:   true,
		Action: func(ctx *runner.StepContext) error {
			layout := mssqlCleanLayout(ctx)
			stage := mssqlCleanStageFromCtx(ctx)
			paths := commonmssql.CleanRemovePaths(stage, layout)
			if len(paths) == 0 {
				return nil
			}
			if ctx.DryRun || ctx.Precheck {
				ctx.Logger.Info("CLEAN-MSSQL-003 dry-run/precheck: would remove %v (stage=%s)", paths, stage)
				return nil
			}
			for _, p := range paths {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				ctx.Logger.Info("Removing %s (stage=%s)", p, stage)
				if err := commonfile.RemoteRemovePath(ctx, p, false); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
