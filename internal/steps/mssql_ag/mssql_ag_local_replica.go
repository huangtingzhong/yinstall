package mssql_ag

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

// localReplicaJoinedAG reports whether the local SQL instance is already a
// member of the named AG (is_local=1). Used by A-012 to skip redundant JOIN
// when adding a new node to an existing AG.
func localReplicaJoinedAG(ctx *runner.StepContext, ag string) (bool, error) {
	if ctx == nil {
		return false, nil
	}
	if ctx.DryRun {
		return false, nil
	}
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "A-012 local replica joined", commonmssql.LocalReplicaJoinedAGSQL(ag))
	if err != nil {
		return false, err
	}
	return commonmssql.SqlcmdScalarIsOne(stdout), nil
}
