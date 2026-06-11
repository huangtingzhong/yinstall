package mysql_standby

import (
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepMR006CheckReplicaConnectivity verifies replica host SSH connectivity.
func StepMR006CheckReplicaConnectivity() *runner.Step {
	return &runner.Step{
		ID:          "MR-006",
		Name:        "Check Replica Connectivity",
		Description: "Verify replica host SSH connectivity",
		Tags:        []string{"mysql-standby", "replica"},
		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "MR-006 replica connectivity")
			res, err := ctx.Execute("hostname", false)
			if err != nil {
				return err
			}
			ctx.SetResult("replica_hostname", strings.TrimSpace(res.GetStdout()))
			return nil
		},
	}
}
