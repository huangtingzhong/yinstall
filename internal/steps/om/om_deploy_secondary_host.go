// om_deploy_secondary_host.go - P2: 在单台目标机部署 secondary yasom
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepDeploySecondaryHost() *runner.Step {
	return &runner.Step{
		Name:        "OM Deploy Secondary Host",
		Description: "Deploy synced secondary yasom on one host (CLI loops targets)",
		Tags:        []string{"om", "deploy-secondary"},

		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("om_deploy_secondary", true) {
				return runner.NewStepSkippedError("om secondary disabled (--om-secondary=false)")
			}
			host := strings.TrimSpace(ctx.GetParamString("om_secondary_host", ""))
			if host == "" {
				return fmt.Errorf("om_secondary_host is required")
			}
			pri, _ := ctx.Results["om_primary_ip"].(string)
			if pri != "" && pri == host {
				return runner.NewStepSkippedError("skip primary OM host for secondary deploy")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Deploy Secondary Host")
			host := strings.TrimSpace(ctx.GetParamString("om_secondary_host", ""))
			return EnsureSecondaryYasom(ctx, host)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			host := strings.TrimSpace(ctx.GetParamString("om_secondary_host", ""))
			listen, _ := ctx.Results["om_secondary_listen"].(string)
			return WaitSecondarySynced(ctx, host, listen, DefaultSyncWaitTimeout, DefaultSyncWaitInterval)
		},
	}
}

// StepDeploySecondaryHost 导出供 CLI Clone / 单测。
func StepDeploySecondaryHost() *runner.Step {
	return stepDeploySecondaryHost()
}
