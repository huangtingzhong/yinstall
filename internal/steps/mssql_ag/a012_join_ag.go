package mssql_ag

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepA012AddReplica() *runner.Step {
	return &runner.Step{
		ID:          "A-012",
		Name:        "Add Replica",
		Description: "Join secondary replica to AG",
		Tags:        []string{"mssql-ha", "ag"},
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsSecondaryHost(ctx) {
				return runner.NewStepSkippedError("A-012 runs on secondary nodes only")
			}
			if !commonmssql.IsListedReplicaHost(ctx) {
				return runner.NewStepSkippedError("A-012: host not in -t (existing AG member; cert/hosts only)")
			}
			if err := requireHadrEnabledWmi(ctx, "A-012"); err != nil {
				return err
			}
			// Idempotent: skip JOIN when the local instance is already in the AG.
			// Needed when adding a new node to an existing AG (existing secondaries
			// would otherwise fail ALTER AVAILABILITY GROUP ... JOIN).
			ag := commonmssql.AGName(ctx)
			if joined, err := localReplicaJoinedAG(ctx, ag); err != nil {
				return err
			} else if joined {
				return runner.NewStepSkippedError("A-012: local instance already joined AG " + ag)
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			ag := commonmssql.AGName(ctx)
			sqlMajor, err := commonmssql.ResolveSQLMajor(ctx)
			if err != nil {
				return err
			}
			return commonmssql.RunSqlcmdQueries(ctx, "A-012 join AG", commonmssql.JoinAvailabilityGroupSQL(ag, sqlMajor))
		},
	}
}
