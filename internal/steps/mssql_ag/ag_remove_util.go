package mssql_ag

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func discoverAGRemoveDBs(ctx *runner.StepContext) error {
	if dbs := commonmssql.AGDBNamesParam(ctx); len(dbs) > 0 {
		commonmssql.SetMirrorDBList(ctx, dbs)
		if !ctx.DryRun {
			ctx.Logger.Info("A-051: AG remove targets (%d): %s", len(dbs), strings.Join(dbs, ", "))
		}
		return nil
	}
	if _, err := commonmssql.MirrorTargetDBs(ctx); err == nil {
		return nil
	}
	if !commonmssql.IsPrimaryHost(ctx) {
		return nil
	}
	if ctx.DryRun {
		commonmssql.SetMirrorDBList(ctx, []string{"(ag-databases)"})
		return nil
	}
	ag := commonmssql.AGName(ctx)
	exists, err := commonmssql.AGExistsOnPrimary(ctx, ag)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "A-051 list AG databases", commonmssql.AGDatabaseListSQL(ag))
	if err != nil {
		return err
	}
	dbs, err := commonmssql.ParseMirrorDBNameList(stdout)
	if err != nil {
		return err
	}
	if len(dbs) == 0 {
		if commonmssql.MirrorDropSecondaryDB(ctx) {
			return fmt.Errorf("A-051: no databases in availability group %s (set --mssql-ag-db)", ag)
		}
		return nil
	}
	commonmssql.SetMirrorDBList(ctx, dbs)
	ctx.Logger.Info("A-051: AG remove databases (%d): %s", len(dbs), strings.Join(dbs, ", "))
	return nil
}

func ensureAGRemoveDBs(ctx *runner.StepContext) ([]string, error) {
	dbs, err := commonmssql.MirrorTargetDBs(ctx)
	if err == nil {
		return dbs, nil
	}
	if commonmssql.IsPrimaryHost(ctx) {
		if err := discoverAGRemoveDBs(ctx); err != nil {
			return nil, err
		}
		return commonmssql.MirrorTargetDBs(ctx)
	}
	return nil, err
}
