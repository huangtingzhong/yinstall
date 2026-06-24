package mssql

import (
	"fmt"
	"strings"

	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

// Topology identifies MSSQL deployment topology for OS verify extensions.
type Topology string

const (
	TopologyStandalone Topology = "standalone"
	TopologyMirror     Topology = "mirror"
	TopologyAGWSFC     Topology = "ag_wsfc"
)

// WinOSProfileForMssql builds Windows OS profile for MSSQL install/ha.
func WinOSProfileForMssql(topology Topology, params map[string]interface{}) commonwin.Profile {
	p := commonwin.ProfileMssql()
	if ports, ok := params["os_firewall_ports"].(string); ok {
		p.FirewallPorts = ports
	}
	domainMode := DomainModeFromParams(params)
	p.VerifyExtra = mssqlVerifyOSBaseline(topology, domainMode)
	return commonwin.ApplyParams(p, params)
}

func mssqlVerifyOSBaseline(topology Topology, domainMode string) func(*runner.StepContext) error {
	return func(ctx *runner.StepContext) error {
		if topology != TopologyAGWSFC {
			return nil
		}
		if domainMode == DomainModeWorkgroup {
			return nil
		}
		res, _ := ctx.Execute(`powershell -NoProfile -Command "`+WSFCClusterNamePowerShell()+`"`, false)
		if res == nil || strings.TrimSpace(res.GetStdout()) == "" {
			return fmt.Errorf("Get-Cluster failed or no WSFC (required for %s); configure WSFC externally before mssql ha", topology)
		}
		return nil
	}
}
