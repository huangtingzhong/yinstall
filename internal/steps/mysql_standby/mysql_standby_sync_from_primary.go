package mysql_standby

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
	mysqlsteps "github.com/yinstall/internal/steps/mysql"
)

// stepSyncFromPrimary syncs data from primary to replica (clone or mysqldump+restore).
func stepSyncFromPrimary() *runner.Step {
	return &runner.Step{
		Name:        "Sync Data From Primary",
		Description: "Clone on replica, or remote mysqldump on replica + restore per --sync-method",
		Tags:        []string{"mysql-standby", "replica", "primary"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if err := skipUnlessStandbyReplicationStage(ctx); err != nil {
				return err
			}
			role := dataSyncRole(ctx)
			switch syncMethod(ctx) {
			case "clone":
				if role != "replica" {
					return fmt.Errorf("skipped: clone sync runs on replica")
				}
			case "dump":
				if role != "replica" {
					return fmt.Errorf("skipped: dump sync runs on replica")
				}
			default:
				return fmt.Errorf("skipped: unknown sync_method %q", syncMethod(ctx))
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			switch syncMethod(ctx) {
			case "dump":
				if err := dumpRemotePrimaryOnReplica(ctx); err != nil {
					return err
				}
				return restoreDumpOnReplica(ctx)
			default:
				return cloneFromPrimary(ctx)
			}
		},
	}
}

func dataSyncRole(ctx *runner.StepContext) string {
	return strings.ToLower(strings.TrimSpace(ctx.GetParamString("data_sync_role", "")))
}

func cloneFromPrimary(ctx *runner.StepContext) error {
	standbyLogPhase(ctx, "clone-start", "MR-011 clone from primary")
	layout, err := replicaLayout(ctx)
	if err != nil {
		return err
	}
	host := primaryHost(ctx)
	port := primaryPort(ctx)
	password := ctx.GetParamString("mysql_root_password", "")
	donor := fmt.Sprintf("'%s:%d'", host, port)
	setDonor := fmt.Sprintf("SET GLOBAL clone_valid_donor_list = %s", donor)
	if err := commonsql.ExecuteMysqlSQL(ctx, layout, password, setDonor); err != nil {
		return err
	}
	cloneSQL := fmt.Sprintf("CLONE INSTANCE FROM '%s'@'%s':%d IDENTIFIED BY '%s'",
		repUser(ctx), host, port, commonsql.EscapeSQLString(repPassword(ctx)))
	ctx.LogScriptPreview("sql", "clone", cloneSQL)
	return runCloneWithMonitor(ctx, layout, password, cloneSQL)
}

func dumpRemotePrimaryOnReplica(ctx *runner.StepContext) error {
	host := primaryHost(ctx)
	port := primaryPort(ctx)
	standbyLogPhase(ctx, "dump-start", fmt.Sprintf("MR-011 remote mysqldump %s:%d on replica", host, port))
	layout, err := replicaLayout(ctx)
	if err != nil {
		return err
	}
	dumpPath := resolveDumpFilePath(ctx, port)
	if err := ensureRemoteDir(ctx, filepath.Dir(strings.ReplaceAll(dumpPath, `\`, `/`))); err != nil {
		return fmt.Errorf("ensure dump directory: %w", err)
	}
	if err := runMysqldump(ctx, layout, host, port, dumpUser(ctx), dumpPassword(ctx), dumpPath); err != nil {
		return err
	}
	size, err := remoteFileSize(ctx, dumpPath)
	if err != nil {
		ctx.Logger.Warn("could not stat dump file %s: %v", dumpPath, err)
	} else {
		ctx.Logger.Info("Remote dump complete on replica: %s (%s)", dumpPath, formatByteSize(size))
	}
	ctx.SetResult("dump_file", dumpPath)
	ctx.Params["dump_file"] = dumpPath
	return nil
}

func restoreDumpOnReplica(ctx *runner.StepContext) error {
	dumpPath := dumpFileFromContext(ctx)
	if dumpPath == "" {
		return fmt.Errorf("dump_file not set: pass --dump-file or run dump phase first")
	}
	standbyLogPhase(ctx, "restore-start", fmt.Sprintf("MR-011 restore dump %s", dumpPath))
	layout, err := replicaLayout(ctx)
	if err != nil {
		return err
	}
	password := ctx.GetParamString("mysql_root_password", "")
	if size, err := remoteFileSize(ctx, dumpPath); err != nil {
		ctx.Logger.Warn("dump file stat before restore: %v", err)
	} else {
		ctx.Logger.Info("Restoring dump file %s (%s)", dumpPath, formatByteSize(size))
	}
	if err := commonsql.ExecuteMysqlSQL(ctx, layout, password, "RESET MASTER"); err != nil {
		if err2 := commonsql.ExecuteMysqlSQL(ctx, layout, password, "RESET BINARY LOGS AND GTIDS"); err2 != nil {
			ctx.Logger.Warn("reset replica logs before restore: %v", err)
		}
	}
	if err := commonsql.ExecuteMysqlScript(ctx, layout, password, dumpPath); err != nil {
		return fmt.Errorf("restore dump: %w", err)
	}
	readySec := ctx.GetParamInt("dump_ready_timeout", 600)
	if readySec <= 0 {
		readySec = 600
	}
	standbyLogPhase(ctx, "restore-done", "waiting for mysqld ready after restore")
	return mysqlsteps.WaitForMysqlReady(ctx, layout, time.Duration(readySec)*time.Second, password)
}
