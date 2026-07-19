// om_ipchange.go - 同机修改 yasom 监听 IP
package om

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func stepIpchangeYasom() *runner.Step {
	return &runner.Step{
		Name:        "OM Ipchange Yasom",
		Description: "Change yasom listen IP on current host via yasboot ipchange yasom",
		Tags:        []string{"om", "ipchange"},
		Dangerous:   true,

		PreCheck: func(ctx *runner.StepContext) error {
			newIP := strings.TrimSpace(ctx.GetParamString("om_ipchange_new_ip", ""))
			if newIP == "" {
				return runner.NewStepSkippedError("om_ipchange_new_ip not set")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			omLogPhase(ctx, "plan", "OM Ipchange Yasom")
			newIP := strings.TrimSpace(ctx.GetParamString("om_ipchange_new_ip", ""))
			toml := strings.TrimSpace(ctx.GetParamString("om_ipchange_toml", ""))
			if toml == "" {
				toml = omStageDir(ctx) + "/hosts.toml"
			}
			return IpchangeYasom(ctx, toml, newIP)
		},

		PostCheck: func(ctx *runner.StepContext) error {
			newIP := strings.TrimSpace(ctx.GetParamString("om_ipchange_new_ip", ""))
			rows, _, err := YasomStatus(ctx)
			if err != nil {
				return err
			}
			listen, _ := YasomListenAddr(newIP, omBeginPort(ctx))
			r := FindRowByIP(rows, newIP)
			if r == nil {
				return fmt.Errorf("after ipchange, host %s not in yasom status", newIP)
			}
			if r.LocalYasomAddr != "" && r.LocalYasomAddr != "-" && listen != "" && r.LocalYasomAddr != listen {
				ctx.Logger.Warn("local_yasom_addr=%s want %s (may still be syncing)", r.LocalYasomAddr, listen)
			}
			return nil
		},
	}
}
