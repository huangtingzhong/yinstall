package db

import (
	"fmt"
	"path"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepCreatePluggableDatabases creates PDBs in CDB$ROOT per --db-pdb after install.
func stepCreatePluggableDatabases() *runner.Step {
	return &runner.Step{
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

			specs, err := ParsePDBSpecs(entries)
			if err != nil {
				return fmt.Errorf("invalid --db-pdb: %w", err)
			}
			if err := ensureMultitenantPackageVersionCtx(ctx, ctx.CurrentStepID); err != nil {
				return err
			}

			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			yasbootPath := path.Join(stageDir, "bin/yasboot")
			result, _ := ctx.Execute(fmt.Sprintf("test -x %s", yasbootPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("yasboot not found at %s", yasbootPath))
			}

			// 只读：同名 PDB 是否已存在（库未起则跳过探测）
			return precheckExistingPDBs(ctx, specs)
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

			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)
			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile, err := resolveDBEnvFile(ctx, hctx)
			if err != nil {
				return err
			}

			status := map[string]string{}
			if statusRes, qerr := dbRunSQLPhase(hctx, user, envFile, clusterName, "pdb-status-before-create", "SELECT NAME, STATUS FROM V$PDBS", false); qerr == nil && statusRes != nil {
				status = commonsql.ParseYasqlOutput(statusRes.Stdout)
			}
			toCreate := filterMissingPDBSpecs(specs, status)
			openTargets := PDBOpenTargetNames(specs)

			dbLogPhase(ctx, "plan", fmt.Sprintf("C-024: Create %d PDB(s) (skip %d existing) in CDB$ROOT", len(toCreate), len(specs)-len(toCreate)))
			for _, s := range specs {
				if st := pdbStatusForName(status, s.Name); st != "" {
					ctx.Logger.Info("  PDB %s: already exists (status=%s), skip CREATE", s.Name, st)
					continue
				}
				tsLabel := "(omit)"
				if s.TablespaceSpecified {
					tsLabel = fmt.Sprintf("datafile=%s size=%s", s.UsersDatafile, s.UsersSize)
				}
				ctx.Logger.Info("  PDB: name=%s user=%s open=%v compat=%s tablespace=%s file_convert=%s",
					s.Name, s.AdminUser, s.Open, pdbCompatLabel(s.CompatMode),
					tsLabel, pdbFileConvertLabel(s))
			}

			if len(toCreate) > 0 {
				createSQL, err := BuildPDBCreateSQL(toCreate)
				if err != nil {
					return err
				}
				hctx.Logger.Info("Executing CREATE PLUGGABLE DATABASE via yasql (/ as sysdba, root container)...")
				if _, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "create-pdbs", createSQL, true); err != nil {
					return fmt.Errorf("PDB creation failed: %w", err)
				}
			} else {
				hctx.Logger.Info("All target PDB(s) already exist; skip CREATE")
				dbLogPhase(hctx, "create-skip", "all_exist")
			}

			if len(openTargets) > 0 {
				statusRes, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "pdb-status", "SELECT NAME, STATUS FROM V$PDBS", false)
				if err != nil {
					return fmt.Errorf("PDB status query failed: %w", err)
				}
				status = commonsql.ParseYasqlOutput(statusRes.Stdout)
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
			envFile, err := resolveDBEnvFile(ctx, hctx)
			if err != nil {
				return err
			}

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

// precheckExistingPDBs 只读查询 V$PDBS；已存在则 Info（apply 将 skip CREATE）。
func precheckExistingPDBs(ctx *runner.StepContext, specs []PDBSpec) error {
	if len(specs) == 0 {
		return nil
	}
	hosts := ctx.HostsToRun()
	if len(hosts) == 0 {
		return nil
	}
	hctx := ctx.ForHost(hosts[0])
	user := hctx.GetParamString("os_user", "yashan")
	clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
	envFile, err := resolveDBEnvFile(ctx, hctx)
	if err != nil {
		// 库未就绪时不阻塞 PreCheck（安装链前半段可能还没有 env）
		ctx.Logger.Info("Skip PDB existence precheck: %v", err)
		return nil
	}
	statusRes, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "pdb-status-precheck", "SELECT NAME, STATUS FROM V$PDBS", false)
	if err != nil || statusRes == nil {
		ctx.Logger.Info("Skip PDB existence precheck: V$PDBS query unavailable")
		return nil
	}
	status := commonsql.ParseYasqlOutput(statusRes.Stdout)
	existing := 0
	for _, s := range specs {
		if st := pdbStatusForName(status, s.Name); st != "" {
			existing++
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName: "Create Pluggable Databases",
				Host:     hctx.Executor.Host(),
				Severity: runner.PrecheckSeverityInfo,
				Code:     "PC.DB.PDB_ALREADY_EXISTS",
				Message:  fmt.Sprintf("PDB %s already exists (status=%s); apply will skip CREATE for this PDB", s.Name, st),
			})
		}
	}
	if existing == len(specs) {
		ctx.ReportPrecheckIssue(runner.PrecheckIssue{
			StepName: "Create Pluggable Databases",
			Host:     hctx.Executor.Host(),
			Severity: runner.PrecheckSeverityInfo,
			Code:     "PC.DB.PDB_ALL_EXIST",
			Message:  fmt.Sprintf("all %d target PDB(s) already exist; apply will skip CREATE (may still OPEN)", len(specs)),
		})
	}
	return nil
}

func filterMissingPDBSpecs(specs []PDBSpec, status map[string]string) []PDBSpec {
	var out []PDBSpec
	for _, s := range specs {
		if pdbStatusForName(status, s.Name) != "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
