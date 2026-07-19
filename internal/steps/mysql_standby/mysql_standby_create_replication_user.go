package mysql_standby

import (
	"fmt"
	"strings"

	commonmysql "github.com/yinstall/internal/common/mysql"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepCreateReplicationUser creates replication user on primary.
func stepCreateReplicationUser() *runner.Step {
	return &runner.Step{
		Name:        "Create Replication User",
		Description: "Create replication user on primary; existing user gets GRANT only (no ALTER USER) to avoid breaking replicas",
		Tags:        []string{"mysql-standby", "primary"},
		PreCheck: func(ctx *runner.StepContext) error {
			if err := skipUnlessStandbyReplicationStage(ctx); err != nil {
				return err
			}
			if repPassword(ctx) == "" && !ctx.DryRun && !ctx.Precheck {
				return fmt.Errorf("rep_password is required")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			standbyLogPhase(ctx, "plan", "MR-004 create replication user")
			user := repUser(ctx)
			checkSQL := fmt.Sprintf("SELECT 1 FROM mysql.user WHERE user='%s' LIMIT 1",
				commonsql.EscapeSQLString(user))
			out, err := queryPrimarySQL(ctx, checkSQL)
			if err != nil {
				return fmt.Errorf("check replication user: %w", err)
			}
			if strings.Contains(strings.TrimSpace(out), "1") {
				ctx.Logger.Info("Replication user %s already exists, granting privileges (skip ALTER USER)", user)
				standbyLogPhase(ctx, "plan", fmt.Sprintf("MR-004 grant replication privileges to %s", user))
				if err := executePrimarySQL(ctx, commonmysql.BuildGrantReplicationPrivileges(user, nil)); err != nil {
					return err
				}
				return ensureDumpPrivilegesOnPrimary(ctx)
			}
			sql := commonmysql.BuildCreateReplicationUser(user, repPassword(ctx), ctx.GetParamBool("rep_ssl", false))
			if err := executePrimarySQL(ctx, sql); err != nil {
				return err
			}
			return ensureDumpPrivilegesOnPrimary(ctx)
		},
	}
}

func ensureDumpPrivilegesOnPrimary(ctx *runner.StepContext) error {
	if syncMethod(ctx) != "dump" {
		return nil
	}
	user := dumpUser(ctx)
	if dumpUserIsRep(ctx) {
		standbyLogPhase(ctx, "plan", fmt.Sprintf("MR-004 grant mysqldump privileges to replication user %s", user))
		return executePrimarySQL(ctx, commonmysql.BuildGrantDumpPrivileges(user, nil))
	}
	password := dumpPassword(ctx)
	if password == "" {
		return fmt.Errorf("dump_password or rep_password is required when --dump-user is set")
	}
	hosts := replicaHosts(ctx)
	standbyLogPhase(ctx, "plan", fmt.Sprintf("MR-004 ensure dedicated dump user %s for hosts %v", user, hosts))
	return executePrimarySQL(ctx, commonmysql.BuildEnsureDumpUser(user, password, hosts))
}
