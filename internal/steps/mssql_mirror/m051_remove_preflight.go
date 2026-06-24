package mssql_mirror

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepM051MirrorRemovePreflight() *runner.Step {
	return &runner.Step{
		ID:          "M-051",
		Name:        "Mirror Remove Preflight",
		Description: "Validate connectivity before removing database mirroring",
		Tags:        []string{"mssql-ha", "mirror", "remove", "preflight"},
		PreCheck: func(ctx *runner.StepContext) error {
			replicas := commonmssql.ReplicaHosts(ctx)
			if len(replicas) == 0 && len(ctx.HostsToRun()) < 2 {
				return runner.NewStepSkippedError("mirror remove preflight requires primary + replica")
			}
			if err := commonmssql.RunSqlcmdQueries(ctx, "M-051 sqlcmd ping", []string{"SELECT 1 AS ok;"}); err != nil {
				return err
			}
			if commonmssql.IsPrimaryHost(ctx) {
				return discoverMirrorRemoveDBs(ctx)
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mshLogPhase(ctx, "plan", "M-051 mirror remove preflight")
			if err := commonmssql.RunSqlcmdQueries(ctx, "M-051 sqlcmd ping", []string{"SELECT 1 AS ok;"}); err != nil {
				return err
			}
			return discoverMirrorRemoveDBs(ctx)
		},
	}
}
