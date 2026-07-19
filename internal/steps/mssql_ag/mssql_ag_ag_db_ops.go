package mssql_ag

import (
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

// queryMirrorDBStatus queries the local state of a single database. Used by
// A-053 to decide whether DROP DATABASE is applicable on the secondary.
func queryMirrorDBStatus(ctx *runner.StepContext, db string) (commonmssql.MirrorDBStatus, error) {
	if ctx.DryRun {
		host := commonmssql.MirrorHostKey(ctx.Executor.Host())
		key := commonmssql.MirrorDBStatusResultKey(host, db)
		if v, ok := ctx.Results[key].(commonmssql.MirrorDBStatus); ok {
			return v, nil
		}
		return commonmssql.MirrorDBStatus{Host: host, Name: db}, nil
	}
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "mirror db status", commonmssql.MirrorDBStatusSQL(db))
	if err != nil {
		return commonmssql.MirrorDBStatus{}, err
	}
	host := commonmssql.MirrorHostKey(ctx.Executor.Host())
	return commonmssql.ParseMirrorDBStatus(host, db, stdout)
}

// dropMirrorSecondaryDB removes partner, recovers RESTORING/RECOVERING, then DROP on secondary.
// Used by A-053 to clean up former AG databases after the AG is dropped.
func dropMirrorSecondaryDB(ctx *runner.StepContext, stepID, db string) error {
	st, err := queryMirrorDBStatus(ctx, db)
	if err != nil {
		return err
	}
	if !st.Exists {
		ctx.Logger.Info("%s: skip %s (not on secondary)", stepID, db)
		return nil
	}
	if st.IsMirroring() {
		if err := commonmssql.RunSqlcmdQueries(ctx, stepID+" partner off "+db, []string{commonmssql.MirrorPartnerOffSQL(db)}); err != nil {
			return err
		}
		st, err = queryMirrorDBStatus(ctx, db)
		if err != nil {
			return err
		}
	}
	if st.IsRestoring() || st.IsRecovering() {
		if err := commonmssql.RunSqlcmdQueries(ctx, stepID+" recover "+db, []string{commonmssql.MirrorRecoverSecondarySQL(db)}); err != nil {
			return err
		}
		st, err = queryMirrorDBStatus(ctx, db)
		if err != nil {
			return err
		}
	}
	if !st.Exists {
		return nil
	}
	return commonmssql.RunSqlcmdQueries(ctx, stepID+" drop secondary db "+db, []string{commonmssql.MirrorDropDatabaseSQL(db)})
}

// mirrorAnyDatabaseMirroring is retained because cert_trust_util.go's
// haTrustProtected references it for the mirror branch. AG mode never takes
// that branch, so this always returns false here.
func mirrorAnyDatabaseMirroring(ctx *runner.StepContext) (bool, error) {
	_ = strings.TrimSpace
	_ = ctx
	return false, nil
}
