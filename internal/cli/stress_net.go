package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	commonos "github.com/yinstall/internal/common/os"
)

const (
	stressNetModeSkip = "skip"
	stressNetModePing = "ping"
	// stressNetModeYAC: ping mesh; iperf3 server=first -t, all other -t nodes as clients.
	stressNetModeYAC = "yac"
)

// stressNetPlan holds resolved network benchmark settings for stressos.
type stressNetPlan struct {
	Enabled      bool
	Mode         string
	PingTarget   string
	SkipReason   string
	YACTargets   []string
	IperfServer  string
	IperfClients []string
}

// resolveStressNetPlan picks ping vs YAC (mesh ping + iperf3) from -t, --ping-target, and local gateway.
func resolveStressNetPlan(cmd *cobra.Command, flags GlobalFlags, netEnabled bool, explicitPing string) stressNetPlan {
	if !netEnabled {
		return stressNetPlan{Enabled: false, Mode: stressNetModeSkip, SkipReason: "network benchmark disabled (--net=false)"}
	}

	targets := flags.Targets
	explicit := strings.TrimSpace(explicitPing)
	pingExplicit := cmd != nil && cmd.Flags().Changed("ping-target") && explicit != ""

	// YAC: >=2 targets → ping mesh; iperf3 server=targets[0], each other target runs as client.
	if len(targets) >= 2 {
		clients := append([]string(nil), targets[1:]...)
		return stressNetPlan{
			Enabled:      true,
			Mode:         stressNetModeYAC,
			YACTargets:   append([]string(nil), targets...),
			IperfServer:  targets[0],
			IperfClients: clients,
		}
	}

	if pingExplicit {
		if explicit == "" {
			return stressNetPlan{
				Enabled:    false,
				Mode:       stressNetModeSkip,
				SkipReason: "--ping-target is empty; skipping network benchmark",
			}
		}
		return stressNetPlan{Enabled: true, Mode: stressNetModePing, PingTarget: explicit}
	}

	if flags.Local || len(targets) == 0 {
		gw := discoverDefaultGatewayLocal()
		if gw == "" {
			return stressNetPlan{
				Enabled:    false,
				Mode:       stressNetModeSkip,
				SkipReason: "no default route gateway found; skipping network benchmark",
			}
		}
		return stressNetPlan{Enabled: true, Mode: stressNetModePing, PingTarget: gw}
	}

	if len(targets) == 1 {
		return stressNetPlan{Enabled: true, Mode: stressNetModePing, PingTarget: targets[0]}
	}

	return stressNetPlan{
		Enabled:    false,
		Mode:       stressNetModeSkip,
		SkipReason: "no ping target available; skipping network benchmark",
	}
}

func discoverDefaultGatewayLocal() string {
	out, err := exec.Command("bash", "-c", commonos.DefaultGatewayShell()).Output()
	if err != nil {
		return ""
	}
	return commonos.ParseDefaultGatewayOutput(string(out))
}

func applyStressNetPlanToParams(params map[string]interface{}, plan stressNetPlan, logger interface {
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
}) {
	params["stress_net_mode"] = plan.Mode
	params["yac_targets"] = plan.YACTargets
	params["iperf3_server_host"] = plan.IperfServer
	params["iperf3_client_hosts"] = plan.IperfClients
	params["ping_target"] = plan.PingTarget

	if !plan.Enabled {
		params["stress_net"] = false
		if plan.SkipReason != "" {
			logger.Warn("Network benchmark: %s", plan.SkipReason)
			fmt.Printf("Warning: %s\n", plan.SkipReason)
		}
		return
	}

	switch plan.Mode {
	case stressNetModeYAC:
		logger.Info("Network benchmark: YAC mode targets=%v (ping mesh; iperf3 server=%s clients=%v)",
			plan.YACTargets, plan.IperfServer, plan.IperfClients)
	case stressNetModePing:
		logger.Info("Network benchmark: ping target=%s", plan.PingTarget)
	}
}
