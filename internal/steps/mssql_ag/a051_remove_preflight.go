package mssql_ag

import (
	"fmt"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepA051AGRemovePreflight() *runner.Step {
	return &runner.Step{
		ID:          "A-051",
		Name:        "AG Remove Preflight",
		Description: "Validate connectivity before removing Always On availability group",
		Tags:        []string{"mssql-ha", "ag", "remove", "preflight"},
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetTargetPlatform() != "" && ctx.GetTargetPlatform() != commonmssql.PlatformWindows {
				return fmt.Errorf("A-051: Always On remove requires Windows target")
			}
			if err := commonmssql.RunSqlcmdQueries(ctx, "A-051 sqlcmd ping", []string{"SELECT 1 AS ok;"}); err != nil {
				return err
			}
			if commonmssql.IsPrimaryHost(ctx) {
				return discoverAGRemoveDBs(ctx)
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			ag := commonmssql.AGName(ctx)
			mshLogPhase(ctx, "plan", "A-051 AG remove preflight ag="+ag)
			if err := commonmssql.RunSqlcmdQueries(ctx, "A-051 sqlcmd ping", []string{"SELECT 1 AS ok;"}); err != nil {
				return err
			}
			if err := discoverAGRemoveDBs(ctx); err != nil {
				return err
			}
			hasArtifacts, err := commonmssql.WSFCCleanArtifacts(ctx, ag)
			if err != nil {
				return err
			}
			if hasArtifacts {
				ctx.Logger.Info("A-051: AG artifacts detected on %s", ctx.Executor.Host())
				return nil
			}
			ctx.Logger.Info("A-051: no AG artifacts on %s", ctx.Executor.Host())
			return nil
		},
	}
}
