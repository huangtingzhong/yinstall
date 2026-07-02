package db

import (
	"fmt"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// ctxCDBEnabled reports whether this install targets a CDB (multitenant).
func ctxCDBEnabled(ctx *runner.StepContext) bool {
	return ctx != nil && ctx.GetParamBool("db_enable_pluggable", false)
}

// pdbNamesFromCtx returns PDB names from --db-pdb specs.
func pdbNamesFromCtx(ctx *runner.StepContext) ([]string, error) {
	specs, err := ParsePDBSpecs(ctx.GetParamStringSlice("db_pdb_specs"))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return names, nil
}

// forEachPDBTarget runs fn for each --db-pdb name when CDB mode is enabled.
func forEachPDBTarget(ctx *runner.StepContext, fn func(pdbName string) error) error {
	names, err := pdbNamesFromCtx(ctx)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("multitenant mode requires at least one --db-pdb")
	}
	for _, name := range names {
		if err := fn(name); err != nil {
			return err
		}
	}
	return nil
}

// openPDBTargetsIfNeeded opens --db-pdb PDBs that are not OPEN (e.g. after cluster restart).
func openPDBTargetsIfNeeded(hctx *runner.StepContext, user, envFile, clusterName string) error {
	if !ctxCDBEnabled(hctx) {
		return nil
	}
	targets, err := pdbNamesFromCtx(hctx)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}

	statusRes, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "pdb-status-after-restart", "SELECT NAME, STATUS FROM V$PDBS", false)
	if err != nil {
		return fmt.Errorf("PDB status query after restart failed: %w", err)
	}
	status := commonsql.ParseYasqlOutput(statusRes.Stdout)
	needOpen := PDBNamesNeedingOpen(targets, status)
	for _, name := range targets {
		st := pdbStatusForName(status, name)
		if st == "" {
			hctx.Logger.Info("  PDB %s: status unknown after restart, will ALTER OPEN", name)
		} else if strings.EqualFold(st, "OPEN") {
			hctx.Logger.Info("  PDB %s: already OPEN after restart", name)
		} else {
			hctx.Logger.Info("  PDB %s: status=%s after restart, will ALTER OPEN", name, st)
		}
	}
	openSQL := BuildPDBOpenSQL(needOpen)
	if openSQL == "" {
		hctx.Logger.Info("All target PDB(s) already OPEN after restart")
		return nil
	}
	hctx.Logger.Info("Opening %d PDB(s) after restart...", len(needOpen))
	if _, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "open-pdbs-after-restart", openSQL, true, commonsql.YasqlErrPDBAlreadyOpen); err != nil {
		return fmt.Errorf("PDB open after restart failed: %w", err)
	}
	return nil
}
