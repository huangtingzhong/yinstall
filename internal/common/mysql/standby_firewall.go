package mysql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/common/netutil"
	"github.com/yinstall/internal/runner"
)

// StandbyPeerPorts returns primary and replica MySQL TCP ports for firewall checks.
func StandbyPeerPorts(ctx *runner.StepContext) (primaryPort, replicaPort int) {
	primaryPort = 3306
	replicaPort = 0
	if ctx != nil {
		if p := ctx.GetParamInt("primary_port", 0); p > 0 {
			primaryPort = p
		}
		if p := ctx.GetParamInt("replica_port", 0); p > 0 {
			replicaPort = p
		}
	}
	return primaryPort, replicaPort
}

func standbyReplicaHosts(ctx *runner.StepContext) []string {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Params["replica_hosts"].([]string); ok {
		return v
	}
	return nil
}

func standbyPrimaryHost(ctx *runner.StepContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.GetParamString("primary_host", ""))
}

func standbySelfHost(ctx *runner.StepContext) string {
	if ctx == nil || ctx.Executor == nil {
		return ""
	}
	return strings.TrimSpace(ctx.Executor.Host())
}

func isStandbyPrimaryHost(ctx *runner.StepContext) bool {
	self := strings.ToLower(standbySelfHost(ctx))
	primary := strings.ToLower(standbyPrimaryHost(ctx))
	return self != "" && primary != "" && self == primary
}

// EnsureStandbyFirewallInbound opens inbound rules for this host's MySQL port(s).
func EnsureStandbyFirewallInbound(ctx *runner.StepContext) error {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return nil
	}
	primaryPort, replicaPort := StandbyPeerPorts(ctx)
	if isStandbyPrimaryHost(ctx) {
		if err := netutil.EnsureInboundTCPPort(ctx, "yinstall-mysql", primaryPort); err != nil {
			return err
		}
		return nil
	}
	if replicaPort > 0 {
		if err := netutil.EnsureInboundTCPPort(ctx, "yinstall-mysql", replicaPort); err != nil {
			return err
		}
	}
	return nil
}

func testPeersTCP(ctx *runner.StepContext, stepID string, tests []struct {
	host string
	port int
}) error {
	self := standbySelfHost(ctx)
	var failures []string
	for _, t := range tests {
		if strings.TrimSpace(t.host) == "" || t.port <= 0 {
			continue
		}
		if err := netutil.TestTCPPort(ctx, t.host, t.port); err != nil {
			failures = append(failures, fmt.Sprintf("%s:%d from %s", t.host, t.port, self))
		} else {
			ctx.Logger.Info("%s: TCP %s:%d reachable from %s", stepID, t.host, t.port, self)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s: inter-server TCP not reachable: %s", stepID, strings.Join(failures, "; "))
	}
	return nil
}

// VerifyStandbyInterServerPorts ensures firewall rules and cross-server MySQL port reachability.
// Run on primary (tests each replica:replica_port) and on replica (tests primary:primary_port).
func VerifyStandbyInterServerPorts(ctx *runner.StepContext, stepID string) error {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return nil
	}
	primaryHost := standbyPrimaryHost(ctx)
	primaryPort, replicaPort := StandbyPeerPorts(ctx)
	if primaryHost == "" {
		return fmt.Errorf("%s: primary_host is required", stepID)
	}

	if err := EnsureStandbyFirewallInbound(ctx); err != nil {
		return fmt.Errorf("%s: %w", stepID, err)
	}

	if isStandbyPrimaryHost(ctx) {
		if err := netutil.VerifyLocalTCPListening(ctx, primaryPort); err != nil {
			return fmt.Errorf("%s: primary MySQL port %d: %w", stepID, primaryPort, err)
		}
		var tests []struct {
			host string
			port int
		}
		for _, rh := range standbyReplicaHosts(ctx) {
			rh = strings.TrimSpace(rh)
			if rh == "" || replicaPort <= 0 {
				continue
			}
			if strings.EqualFold(rh, primaryHost) {
				continue
			}
			tests = append(tests, struct {
				host string
				port int
			}{rh, replicaPort})
		}
		if len(tests) == 0 {
			ctx.Logger.Info("%s: no remote replica hosts to probe from primary", stepID)
			return nil
		}
		if err := testPeersTCP(ctx, stepID, tests); err != nil {
			if retryErr := EnsureStandbyFirewallInbound(ctx); retryErr != nil {
				return fmt.Errorf("%s: %w", stepID, retryErr)
			}
			if err2 := testPeersTCP(ctx, stepID, tests); err2 != nil {
				return fmt.Errorf("%s: still not reachable after firewall rules: %w", stepID, err2)
			}
		}
		return nil
	}

	if replicaPort <= 0 {
		return fmt.Errorf("%s: replica_port is required", stepID)
	}
	if err := netutil.VerifyLocalTCPListening(ctx, replicaPort); err != nil {
		return fmt.Errorf("%s: replica MySQL port %d: %w", stepID, replicaPort, err)
	}
	tests := []struct {
		host string
		port int
	}{{primaryHost, primaryPort}}
	if err := testPeersTCP(ctx, stepID, tests); err != nil {
		if retryErr := EnsureStandbyFirewallInbound(ctx); retryErr != nil {
			return fmt.Errorf("%s: %w", stepID, retryErr)
		}
		if err2 := testPeersTCP(ctx, stepID, tests); err2 != nil {
			return fmt.Errorf("%s: still not reachable after firewall rules: %w", stepID, err2)
		}
	}
	return nil
}
