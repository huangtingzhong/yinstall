package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/common/netutil"
	"github.com/yinstall/internal/runner"
)

// DefaultSAPassword is used when --mssql-sa-password is omitted and the run
// includes SQL software install (MS-* / replica install). HA-only and remove
// paths leave SA password empty so sqlcmd prefers Windows integrated auth (-E).
const DefaultSAPassword = "aaBB11@@"

// ResolveSAPassword returns explicit password when set; otherwise DefaultSAPassword
// for install runs, or empty string for HA/remove (integrated auth fallback).
func ResolveSAPassword(explicit string, useInstallDefault bool) string {
	if s := strings.TrimSpace(explicit); s != "" {
		return s
	}
	if useInstallDefault {
		return DefaultSAPassword
	}
	return ""
}

// HAFirewallTCPPorts returns SQL, HA endpoint, and SMB ports for HA/mirror.
func HAFirewallTCPPorts(ctx *runner.StepContext) []int {
	sqlPort := ResolvedListenPort(ctx)
	haPort := LocalHAEndpointPort(ctx)
	return []int{sqlPort, haPort, 445}
}

// HAPeerHosts returns primary and replica hosts (deduped).
func HAPeerHosts(ctx *runner.StepContext) []string {
	if ctx == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" || seen[strings.ToLower(h)] {
			return
		}
		seen[strings.ToLower(h)] = true
		out = append(out, h)
	}
	add(ResolvePrimaryHost(ctx))
	for _, h := range HATopologyHosts(ctx) {
		add(h)
	}
	return out
}

// HAPartnersForHost returns HA peers excluding the current executor host.
func HAPartnersForHost(ctx *runner.StepContext) []string {
	if ctx == nil || ctx.Executor == nil {
		return nil
	}
	self := strings.TrimSpace(ctx.Executor.Host())
	var partners []string
	for _, h := range HAPeerHosts(ctx) {
		if !strings.EqualFold(strings.TrimSpace(h), self) {
			partners = append(partners, h)
		}
	}
	return partners
}

// EnsureHAFirewallInbound creates inbound allow rules for HA TCP ports (idempotent).
func EnsureHAFirewallInbound(ctx *runner.StepContext) error {
	if ctx == nil {
		return nil
	}
	if ctx.DryRun || ctx.Precheck {
		ctx.Logger.Info("HA firewall: dry-run/precheck skip rule creation")
		return nil
	}
	for _, port := range HAFirewallTCPPorts(ctx) {
		if err := netutil.EnsureInboundTCPPort(ctx, "yinstall-ha", port); err != nil {
			return err
		}
	}
	return nil
}

// VerifyLocalHAEndpointListening checks HA endpoint port is listening locally.
func VerifyLocalHAEndpointListening(ctx *runner.StepContext, port int) error {
	return netutil.VerifyLocalTCPListening(ctx, port)
}

// TestTCPPortFromHost runs a TCP probe from the current host to remoteHost:port.
func TestTCPPortFromHost(ctx *runner.StepContext, remoteHost string, port int) error {
	return netutil.TestTCPPort(ctx, remoteHost, port)
}

func testPartnersHAEndpointTCP(ctx *runner.StepContext, stepID string) error {
	partners := HAPartnersForHost(ctx)
	if len(partners) == 0 {
		ctx.Logger.Info("%s: single-node HA context; skip partner HA endpoint TCP test", stepID)
		return nil
	}
	self := ""
	if ctx.Executor != nil {
		self = ctx.Executor.Host()
	}
	var failures []string
	for _, partner := range partners {
		port := HAEndpointPortForHost(ctx, partner)
		if err := netutil.TestTCPPort(ctx, partner, port); err != nil {
			failures = append(failures, fmt.Sprintf("%s:%d from %s", partner, port, self))
		} else {
			ctx.Logger.Info("%s: HA endpoint TCP %s:%d reachable from %s", stepID, partner, port, self)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s: HA endpoint TCP not reachable: %s", stepID, strings.Join(failures, "; "))
	}
	return nil
}

func testPartnersSQLTCP(ctx *runner.StepContext, stepID string) error {
	partners := HAPartnersForHost(ctx)
	if len(partners) == 0 {
		ctx.Logger.Info("%s: single-node HA context; skip partner SQL TCP test", stepID)
		return nil
	}
	self := ""
	if ctx.Executor != nil {
		self = ctx.Executor.Host()
	}
	var failures []string
	for _, partner := range partners {
		port := SQLPortForHost(ctx, partner)
		if port <= 0 {
			failures = append(failures, fmt.Sprintf("%s: unknown SQL port (resolve peer instance or set --primary/replica-mssql-port)", partner))
			continue
		}
		if err := netutil.TestTCPPort(ctx, partner, port); err != nil {
			failures = append(failures, fmt.Sprintf("%s:%d from %s", partner, port, self))
		} else {
			ctx.Logger.Info("%s: SQL TCP %s:%d reachable from %s", stepID, partner, port, self)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s: SQL TCP not reachable: %s", stepID, strings.Join(failures, "; "))
	}
	return nil
}

// VerifyHAPreEndpointConnectivity opens firewall and verifies SQL port reachability between HA peers.
func VerifyHAPreEndpointConnectivity(ctx *runner.StepContext, stepID string) error {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return nil
	}
	if err := EnsureHAFirewallInbound(ctx); err != nil {
		return fmt.Errorf("%s: %w", stepID, err)
	}
	if err := testPartnersSQLTCP(ctx, stepID); err != nil {
		if retryErr := EnsureHAFirewallInbound(ctx); retryErr != nil {
			return fmt.Errorf("%s: %w", stepID, retryErr)
		}
		if err2 := testPartnersSQLTCP(ctx, stepID); err2 != nil {
			return fmt.Errorf("%s: SQL TCP still not reachable after firewall rules: %w", stepID, err2)
		}
	}
	return nil
}

// VerifyLocalHAEndpointReady checks firewall rules and local HA endpoint listen after endpoint creation.
func VerifyLocalHAEndpointReady(ctx *runner.StepContext, stepID string) error {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return nil
	}
	port := LocalHAEndpointPort(ctx)
	if err := EnsureHAFirewallInbound(ctx); err != nil {
		return fmt.Errorf("%s: %w", stepID, err)
	}
	if err := VerifyLocalHAEndpointListening(ctx, port); err != nil {
		return fmt.Errorf("%s: %w", stepID, err)
	}
	return nil
}

// VerifyHAPeerEndpointReachability verifies HA endpoint port is reachable between all peers.
func VerifyHAPeerEndpointReachability(ctx *runner.StepContext, stepID string) error {
	if ctx == nil || ctx.DryRun || ctx.Precheck {
		return nil
	}
	if err := EnsureHAFirewallInbound(ctx); err != nil {
		return fmt.Errorf("%s: %w", stepID, err)
	}
	localPort := LocalHAEndpointPort(ctx)
	if err := VerifyLocalHAEndpointListening(ctx, localPort); err != nil {
		return fmt.Errorf("%s: %w", stepID, err)
	}
	if err := testPartnersHAEndpointTCP(ctx, stepID); err != nil {
		if retryErr := EnsureHAFirewallInbound(ctx); retryErr != nil {
			return fmt.Errorf("%s: %w", stepID, retryErr)
		}
		if err2 := testPartnersHAEndpointTCP(ctx, stepID); err2 != nil {
			return fmt.Errorf("%s: HA endpoint TCP still not reachable after firewall rules: %w", stepID, err2)
		}
	}
	return nil
}
