package mysql_standby

import (
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepConfigurePrimaryForReplication enables GTID on primary when missing.
func stepConfigurePrimaryForReplication() *runner.Step {
	return &runner.Step{
		Name:        "Configure Primary for Replication",
		Description: "Enable GTID-related settings on primary when missing",
		Tags:        []string{"mysql-standby", "primary"},
		PreCheck:    skipUnlessStandbyReplicationStage,
		Action: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			standbyLogPhase(ctx, "plan", "MR-003 configure primary")
			gtid, err := queryPrimarySQL(ctx, "SELECT @@gtid_mode")
			if err != nil {
				return err
			}
			if strings.EqualFold(parseSQLScalar(gtid), "ON") {
				ctx.Logger.Info("Primary gtid_mode already ON")
				return nil
			}
			layout := primaryLayout(ctx)
			pw := primaryRootPassword(ctx)
			for _, sql := range []string{
				"SET GLOBAL enforce_gtid_consistency = OFF",
				"SET GLOBAL gtid_mode = OFF_PERMISSIVE",
				"SET GLOBAL gtid_mode = ON_PERMISSIVE",
				"SET GLOBAL gtid_mode = ON",
				"SET GLOBAL enforce_gtid_consistency = ON",
			} {
				if err := commonsql.ExecuteMysqlSQL(ctx, layout, pw, sql); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
