package mssql_ag

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepA014AddAGDatabases() *runner.Step {
	return &runner.Step{
		ID:          "A-014",
		Name:        "Add Databases to AG",
		Description: "Manual or automatic seeding of user databases into AG",
		Tags:        []string{"mssql-ha", "ag", "seed"},
		PreCheck: func(ctx *runner.StepContext) error {
			dbs := commonmssql.AGDBNamesParam(ctx)
			if len(dbs) == 0 {
				return runner.NewStepSkippedError("A-014: no --mssql-ag-db specified")
			}
			if commonmssql.AGSeedingMode(ctx) == "automatic" {
				return commonmssql.ValidateAutomaticSeedingUNC(ctx)
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			phase := strings.TrimSpace(ctx.GetParamString("ag_014_phase", ""))
			if phase == "" {
				return fmt.Errorf("A-014: missing ag_014_phase (internal runner configuration)")
			}
			dbs := commonmssql.AGDBNamesParam(ctx)
			ag := commonmssql.AGName(ctx)
			switch phase {
			case "backup-primary":
				return a014ManualBackupPrimary(ctx, dbs)
			case "restore-secondary":
				return a014ManualRestoreSecondary(ctx, dbs)
			case "log-backup":
				return a014ManualLogChain(ctx, dbs, "log-backup")
			case "log-restore":
				return a014ManualLogChain(ctx, dbs, "log-restore-secondary")
			case "add-manual":
				if !commonmssql.IsPrimaryHost(ctx) {
					return runner.NewStepSkippedError("A-014 add-manual runs on primary only")
				}
				for _, db := range dbs {
					if dbAlreadyInAG(ctx, db) {
						ctx.Logger.Info("A-014: skip add database %s (already in AG %s)", db, ag)
						continue
					}
					if err := commonmssql.RunSqlcmdQueries(ctx, "A-014 add db "+db, []string{commonmssql.AddDatabaseToAGSQL(ag, db)}); err != nil {
						return err
					}
				}
			case "join-secondary":
				if commonmssql.IsPrimaryHost(ctx) {
					return runner.NewStepSkippedError("A-014 join-secondary runs on secondary only")
				}
				if !commonmssql.IsListedReplicaHost(ctx) {
					ctx.Logger.Info("A-014: skip join on %s (not in -t; existing AG member)", commonmssql.TargetHost(ctx))
					return nil
				}
				for _, db := range dbs {
					if dbAlreadyInAG(ctx, db) {
						ctx.Logger.Info("A-014: skip join %s (already in AG %s on this secondary)", db, ag)
						continue
					}
					if err := commonmssql.RunSqlcmdQueries(ctx, "A-014 join db "+db, []string{commonmssql.JoinDatabaseToAGSQL(ag, db)}); err != nil {
						return err
					}
				}
			case "add-automatic":
				if !commonmssql.IsPrimaryHost(ctx) {
					return runner.NewStepSkippedError("A-014 add-automatic runs on primary only")
				}
				for _, db := range dbs {
					if dbAlreadyInAG(ctx, db) {
						ctx.Logger.Info("A-014: skip automatic add %s (already in AG %s)", db, ag)
						continue
					}
					sqlMajor := commonmssql.SQLMajorFromContext(ctx)
					if err := commonmssql.RunSqlcmdQueries(ctx, "A-014 automatic add "+db, []string{commonmssql.AddDatabaseToAGAutomaticSQL(ag, db, sqlMajor)}); err != nil {
						return err
					}
				}
			default:
				return fmt.Errorf("A-014: unknown phase %q", phase)
			}
			return nil
		},
	}
}
