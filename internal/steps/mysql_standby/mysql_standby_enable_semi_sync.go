package mysql_standby

import (
	"fmt"
	"strings"

	commonmysql "github.com/yinstall/internal/common/mysql"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepEnableSemiSync enables semi-sync replication when requested.
func stepEnableSemiSync() *runner.Step {
	return &runner.Step{
		Name:        "Enable Semi-Sync",
		Description: "Dynamic semi-sync plugin install and enable",
		Tags:        []string{"mysql-standby", "primary", "replica"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("enable_semi_sync", false) {
				return fmt.Errorf("skipped: enable_semi_sync false")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			standbyLogPhase(ctx, "plan", "MR-016 enable semi-sync")
			platform := ctx.GetTargetPlatform()
			role := ctx.GetParamString("semi_sync_role", "replica")
			sql := commonmysql.SemiSyncPluginSQL(platform, role)
			if sql == "" {
				return nil
			}
			if role == "primary" || role == "source" {
				layout := primaryLayout(ctx)
				pw := primaryRootPassword(ctx)
				if err := ensureSemiSyncPlugin(ctx, layout, pw, sql); err != nil {
					return err
				}
				return executePrimarySQL(ctx, "SET GLOBAL rpl_semi_sync_source_enabled = ON")
			}
			layout, err := replicaLayout(ctx)
			if err != nil {
				return err
			}
			pw := ctx.GetParamString("mysql_root_password", "")
			if err := ensureSemiSyncPlugin(ctx, layout, pw, sql); err != nil {
				return err
			}
			return commonsql.ExecuteMysqlSQL(ctx, layout, pw, "SET GLOBAL rpl_semi_sync_replica_enabled = ON")
		},
	}
}

func ensureSemiSyncPlugin(ctx *runner.StepContext, layout commonmysql.Layout, password, installSQL string) error {
	name := ""
	switch {
	case strings.Contains(installSQL, "rpl_semi_sync_source"):
		name = "rpl_semi_sync_source"
	case strings.Contains(installSQL, "rpl_semi_sync_replica"):
		name = "rpl_semi_sync_replica"
	}
	if name == "" {
		return commonsql.ExecuteMysqlSQL(ctx, layout, password, installSQL)
	}
	active, err := mysqlPluginActive(ctx, layout, password, name)
	if err != nil {
		return fmt.Errorf("check semi-sync plugin %s: %w", name, err)
	}
	if active {
		ctx.Logger.Info("semi-sync plugin %s already active", name)
		return nil
	}
	return commonsql.ExecuteMysqlSQL(ctx, layout, password, installSQL)
}
