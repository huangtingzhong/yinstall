package mssql_mirror

import (
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func ensureMirrorTargetDBs(ctx *runner.StepContext) ([]string, error) {
	dbs, err := commonmssql.MirrorTargetDBs(ctx)
	if err == nil {
		return dbs, nil
	}
	if commonmssql.IsPrimaryHost(ctx) {
		if err := discoverMirrorTargetDBs(ctx); err != nil {
			return nil, err
		}
		return commonmssql.MirrorTargetDBs(ctx)
	}
	return nil, err
}

func ensureMirrorRemoveDBs(ctx *runner.StepContext) ([]string, error) {
	dbs, err := commonmssql.MirrorTargetDBs(ctx)
	if err == nil {
		return dbs, nil
	}
	if commonmssql.IsPrimaryHost(ctx) {
		if err := discoverMirrorRemoveDBs(ctx); err != nil {
			return nil, err
		}
		return commonmssql.MirrorTargetDBs(ctx)
	}
	return nil, err
}

func allMirrorDBsMatch(ctx *runner.StepContext, dbs []string, fn func(db string) bool) bool {
	if len(dbs) == 0 {
		return false
	}
	for _, db := range dbs {
		if !fn(db) {
			return false
		}
	}
	return true
}

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

func mirrorAnyDatabaseMirroring(ctx *runner.StepContext) (bool, error) {
	if ctx.DryRun {
		return false, nil
	}
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "mirror infra any db mirroring", commonmssql.MirrorAnyDatabaseMirroringSQL())
	if err != nil {
		return false, err
	}
	return commonmssql.ParseSqlcmdBoolScalar(stdout), nil
}

func mirrorDBHasPartner(ctx *runner.StepContext, db string) bool {
	if ctx.DryRun || ctx.Precheck {
		return false
	}
	out, err := commonmssql.QuerySqlcmdScalar(ctx, "mirror has partner "+db, commonmssql.MirrorHasPartnerScalarSQL(db))
	if err != nil {
		return false
	}
	return commonmssql.ParseSqlcmdBoolScalar(out)
}

func mirrorDBRestoring(ctx *runner.StepContext, db string) bool {
	if ctx.DryRun {
		return false
	}
	st, err := queryMirrorDBStatus(ctx, db)
	if err != nil {
		return false
	}
	return st.IsRestoring()
}

func mirrorDBConfigured(ctx *runner.StepContext, db string) bool {
	if ctx.DryRun {
		return false
	}
	st, err := queryMirrorDBStatus(ctx, db)
	if err != nil {
		return false
	}
	return st.IsMirroring() || st.IsRestoring()
}

func mirrorDBSynchronized(ctx *runner.StepContext, db string) bool {
	if ctx.DryRun {
		return false
	}
	st, err := queryMirrorDBStatus(ctx, db)
	if err != nil {
		return false
	}
	return st.IsSynchronized()
}
