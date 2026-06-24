package mssql_mirror

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func compareMirrorPartnerVersions(ctx *runner.StepContext) error {
	if ctx.DryRun {
		return nil
	}
	primary := commonmssql.ResolvePrimaryHost(ctx)
	hosts := append([]string{primary}, commonmssql.ReplicaHosts(ctx)...)
	infos := make([]commonmssql.MirrorInstanceInfo, 0, len(hosts))
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		info, ok := commonmssql.MirrorInstanceInfoFromResults(ctx.Results, h)
		if !ok {
			return fmt.Errorf("M-006: missing instance version for %s (run M-005 first)", h)
		}
		infos = append(infos, info)
	}
	mshLogPhase(ctx, "version-compare-start", primary)
	if err := commonmssql.CompareMirrorPartners(primary, infos); err != nil {
		return err
	}
	for _, info := range infos {
		ctx.Logger.Info("M-006: %s version=%s level=%s edition=%s engine=%s major=%s",
			info.Host, info.ProductVersion, info.ProductLevel, info.Edition, info.EngineEdition, info.ProductMajorVersion)
	}
	mshLogPhase(ctx, "version-compare-done", primary)
	return nil
}

func collectMirrorInstanceInfo(ctx *runner.StepContext) error {
	if ctx.DryRun {
		return nil
	}
	host := commonmssql.MirrorHostKey(ctx.Executor.Host())
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "M-005 instance version", commonmssql.MirrorInstanceInfoSQL())
	if err != nil {
		return err
	}
	info, err := commonmssql.ParseMirrorInstanceInfo(host, stdout)
	if err != nil {
		return fmt.Errorf("M-005: %w", err)
	}
	ctx.SetResult(commonmssql.MirrorInstanceInfoResultKey(host), info)
	ctx.Logger.Info("M-005: %s SQL Server version=%s level=%s edition=%s",
		host, info.ProductVersion, info.ProductLevel, info.Edition)
	return nil
}

func collectMirrorDBStatuses(ctx *runner.StepContext) error {
	if ctx.DryRun {
		return nil
	}
	dbs, err := ensureMirrorTargetDBs(ctx)
	if err != nil {
		return err
	}
	host := commonmssql.MirrorHostKey(ctx.Executor.Host())
	for _, db := range dbs {
		stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "M-005 database status "+db, commonmssql.MirrorDBStatusSQL(db))
		if err != nil {
			return err
		}
		st, err := commonmssql.ParseMirrorDBStatus(host, db, stdout)
		if err != nil {
			return fmt.Errorf("M-005: %w", err)
		}
		ctx.SetResult(commonmssql.MirrorDBStatusResultKey(host, db), st)
		if st.Exists {
			ctx.Logger.Info("M-005: %s database %s state=%s recovery=%s mirroring=%s role=%s",
				host, db, st.StateDesc, st.RecoveryModel, st.MirroringState, st.MirroringRole)
		} else {
			ctx.Logger.Info("M-005: %s database %s does not exist", host, db)
		}
	}
	return nil
}

func discoverMirrorTargetDBs(ctx *runner.StepContext) error {
	if dbs := commonmssql.MirrorDBNamesParam(ctx); len(dbs) > 0 {
		commonmssql.SetMirrorDBList(ctx, dbs)
		if !ctx.DryRun {
			ctx.Logger.Info("M-005: mirror target databases (%d): %s", len(dbs), strings.Join(dbs, ", "))
		}
		return nil
	}
	if v, err := commonmssql.MirrorTargetDBs(ctx); err == nil && len(v) > 0 {
		return nil
	}
	if !commonmssql.IsPrimaryHost(ctx) {
		return nil
	}
	if ctx.DryRun {
		commonmssql.SetMirrorDBList(ctx, []string{"(user-databases)"})
		return nil
	}
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "M-005 list user databases", commonmssql.MirrorBusinessDBListSQL())
	if err != nil {
		return err
	}
	dbs, err := commonmssql.ParseMirrorDBNameList(stdout)
	if err != nil {
		return err
	}
	if len(dbs) == 0 {
		return fmt.Errorf("M-005: no user databases found on primary")
	}
	commonmssql.SetMirrorDBList(ctx, dbs)
	ctx.Logger.Info("M-005: mirror all user databases (%d): %s", len(dbs), strings.Join(dbs, ", "))
	return nil
}

func discoverMirrorRemoveDBs(ctx *runner.StepContext) error {
	if dbs := commonmssql.MirrorDBNamesParam(ctx); len(dbs) > 0 {
		commonmssql.SetMirrorDBList(ctx, dbs)
		return nil
	}
	if v, err := commonmssql.MirrorTargetDBs(ctx); err == nil && len(v) > 0 {
		return nil
	}
	if !commonmssql.IsPrimaryHost(ctx) {
		return nil
	}
	if ctx.DryRun {
		commonmssql.SetMirrorDBList(ctx, []string{"(mirrored-databases)"})
		return nil
	}
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "M-051 list mirrored databases", commonmssql.MirrorMirroredDBListSQL())
	if err != nil {
		return err
	}
	dbs, err := commonmssql.ParseMirrorDBNameList(stdout)
	if err != nil {
		return err
	}
	if len(dbs) == 0 {
		return fmt.Errorf("M-051: no mirrored databases found on primary")
	}
	commonmssql.SetMirrorDBList(ctx, dbs)
	ctx.Logger.Info("M-051: remove mirroring for databases (%d): %s", len(dbs), strings.Join(dbs, ", "))
	return nil
}

func discoverMirrorWorkDir(ctx *runner.StepContext) error {
	hostKey := commonmssql.MirrorHostKey(ctx.Executor.Host())
	if v, ok := ctx.Results[commonmssql.HAWorkDirResultKey(hostKey)].(string); ok && strings.TrimSpace(v) != "" {
		return nil
	}
	if wd := strings.TrimSpace(ctx.GetParamString("mirror_work_dir", "")); wd != "" {
		commonmssql.SetHAWorkDir(ctx, hostKey, wd)
		return nil
	}
	if ctx.DryRun || ctx.Precheck {
		commonmssql.SetHAWorkDir(ctx, hostKey, commonmssql.MirrorWorkDir(ctx))
		return nil
	}
	cmd := commonmssql.SqlcmdQueryCommand(ctx, commonmssql.DiscoverHAWorkDirSQL())
	res, err := ctx.Execute(cmd, false)
	if err != nil || res == nil || res.GetExitCode() != 0 {
		commonmssql.SetHAWorkDir(ctx, hostKey, commonmssql.MirrorWorkDir(ctx))
		return nil
	}
	base, err := commonmssql.ParseHAWorkDirFromSqlcmd(res.GetStdout())
	if err != nil {
		return fmt.Errorf("M-008: %w", err)
	}
	work := commonmssql.JoinWinPath(base, commonmssql.MirrorWorkSubdir)
	commonmssql.SetHAWorkDir(ctx, hostKey, work)
	return nil
}
