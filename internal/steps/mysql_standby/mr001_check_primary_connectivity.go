package mysql_standby

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepMR001CheckPrimaryConnectivity verifies primary host SSH connectivity.
func StepMR001CheckPrimaryConnectivity() *runner.Step {
	return &runner.Step{
		ID:          "MR-001",
		Name:        "Check Primary Connectivity",
		Description: "Verify primary host SSH connectivity",
		Tags:        []string{"mysql-standby", "primary"},
		PreCheck: func(ctx *runner.StepContext) error {
			if strings.TrimSpace(primaryHost(ctx)) == "" {
				return fmt.Errorf("primary_host is required")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "MR-001 primary connectivity")
			res, err := ctx.Execute("hostname", false)
			if err != nil {
				return err
			}
			hostname := strings.TrimSpace(res.GetStdout())
			ctx.SetResult("primary_hostname", hostname)
			ctx.Logger.Info("Primary hostname: %s", hostname)
			return nil
		},
	}
}
