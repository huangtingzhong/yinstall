package mysql_standby

import (
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

func stepReplicationFirewallPrepare() *runner.Step {
	return &runner.Step{
		Name:        "Replication Firewall Prepare",
		Description: "Open firewall and verify inter-server MySQL TCP ports before data sync/replication",
		Tags:        []string{"mysql-standby", "firewall", "primary", "replica"},
		PreCheck:    skipUnlessStandbyReplicationStage,
		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "MR-019 replication firewall and inter-server port check")
			pp, rp := commonmysql.StandbyPeerPorts(ctx)
			ctx.Logger.Info("MR-019: host=%s primary_port=%d replica_port=%d", ctx.Executor.Host(), pp, rp)
			return commonmysql.VerifyStandbyInterServerPorts(ctx, "MR-019")
		},
	}
}
