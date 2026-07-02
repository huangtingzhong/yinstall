package db

import (
	"fmt"
	"path"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// StepC024CreatePluggableDatabases creates PDBs in CDB$ROOT per --db-pdb after install.
func StepC024CreatePluggableDatabases() *runner.Step {
	return &runner.Step{
		ID:          "C-024",
		Name:        "Create Pluggable Databases",
		Description: "Create and open PDBs via --db-pdb when CDB (multitenant) mode is enabled",
		Tags:        []string{"db", "pdb", "multitenant"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			entries := ctx.GetParamStringSlice("db_pdb_specs")
			if len(entries) == 0 {
				return fmt.Errorf("no --db-pdb specified, skipping")
			}
			if !ctx.GetParamBool("db_enable_pluggable", false) {
				return fmt.Errorf("--db-enable-pluggable is required when --db-pdb is set")
			}

			if _, err := ParsePDBSpecs(entries); err != nil {
				return fmt.Errorf("invalid --db-pdb: %w", err)
			}
			if err := ensureMultitenantPackageVersionCtx(ctx, "C-024"); err != nil {
				return err
			}

			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			yasbootPath := path.Join(stageDir, "bin/yasboot")
			result, _ := ctx.Execute(fmt.Sprintf("test -x %s", yasbootPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("yasboot not found at %s", yasbootPath))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			entries := ctx.GetParamStringSlice("db_pdb_specs")
			specs, err := ParsePDBSpecs(entries)
			if err != nil {
				return fmt.Errorf("invalid --db-pdb: %w", err)
			}
			if len(specs) == 0 {
				ctx.Logger.Info("No PDB specs to create, skipping")
				return nil
			}

			createSQL, err := BuildPDBCreateSQL(specs)
			if err != nil {
				return err
			}
			openTargets := PDBOpenTargetNames(specs)

			dbLogPhase(ctx, "plan", fmt.Sprintf("C-024: Create %d PDB(s) in CDB$ROOT", len(specs)))
			for _, s := range specs {
				ctx.Logger.Info("  PDB: name=%s user=%s open=%v compat=%s datafile=%s size=%s file_convert=%s",
					s.Name, s.AdminUser, s.Open, pdbCompatLabel(s.CompatMode),
					s.UsersDatafile, s.UsersSize, pdbFileConvertLabel(s))
			}

			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)
			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile := resolveDBEnvFile(ctx, hctx)

			hctx.Logger.Info("Executing CREATE PLUGGABLE DATABASE via yasql (/ as sysdba, root container)...")
			if _, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "create-pdbs", createSQL, true); err != nil {
				return fmt.Errorf("PDB creation failed: %w", err)
			}

			if len(openTargets) > 0 {
				statusRes, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "pdb-status", "SELECT NAME, STATUS FROM V$PDBS", false)
				if err != nil {
					return fmt.Errorf("PDB status query failed: %w", err)
				}
				status := commonsql.ParseYasqlOutput(statusRes.Stdout)
				needOpen := PDBNamesNeedingOpen(openTargets, status)
				for _, name := range openTargets {
					st := pdbStatusForName(status, name)
					if st == "" {
						hctx.Logger.Info("  PDB %s: status unknown, will ALTER OPEN", name)
					} else if strings.EqualFold(st, "OPEN") {
						hctx.Logger.Info("  PDB %s: already OPEN, skip ALTER OPEN", name)
					} else {
						hctx.Logger.Info("  PDB %s: status=%s, will ALTER OPEN", name, st)
					}
				}
				if openSQL := BuildPDBOpenSQL(needOpen); openSQL != "" {
					hctx.Logger.Info("Opening %d PDB(s)...", len(needOpen))
					if _, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "open-pdbs", openSQL, true, commonsql.YasqlErrPDBAlreadyOpen); err != nil {
						return fmt.Errorf("PDB open failed: %w", err)
					}
				} else {
					hctx.Logger.Info("All target PDB(s) already OPEN, skipping ALTER OPEN")
				}
			}

			hctx.Logger.Info("PDB creation completed successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			entries := ctx.GetParamStringSlice("db_pdb_specs")
			if len(entries) == 0 {
				return nil
			}
			specs, err := ParsePDBSpecs(entries)
			if err != nil || len(specs) == 0 {
				return nil
			}

			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)
			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile := resolveDBEnvFile(ctx, hctx)

			res, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "verify-pdbs", "SELECT NAME, STATUS FROM V$PDBS", false)
			if err != nil {
				hctx.Logger.Warn("PDB verification query failed: %v", err)
				return nil
			}
			if res != nil && strings.TrimSpace(res.Stdout) != "" {
				hctx.Logger.Info("V$PDBS:\n%s", strings.TrimSpace(res.Stdout))
			}
			return nil
		},
	}
}

func pdbCompatLabel(mode string) string {
	if mode == "mysql" {
		return "mysql"
	}
	return "yashan"
}

func pdbFileConvertLabel(s PDBSpec) string {
	if s.FileConvertNone {
		return "none"
	}
	if s.FileConvertFrom != "" && s.FileConvertTo != "" {
		return s.FileConvertFrom + ":" + s.FileConvertTo
	}
	return "(default)"
}
