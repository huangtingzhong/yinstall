package db

import (
	"fmt"
	"net"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/logging"
)

// netResultAdapter adapts ExecResultForC001 to commonos.NetExecResult
type netResultAdapter struct{ r ExecResultForC001 }

func (a *netResultAdapter) GetExitCode() int {
	if a.r == nil {
		return -1
	}
	return a.r.GetExitCode()
}

func (a *netResultAdapter) GetStdout() string {
	if a.r == nil {
		return ""
	}
	return a.r.GetStdout()
}

// netExecutorAdapter adapts ExecutorForC001 to commonos.NetExecutor
type netExecutorAdapter struct {
	e    ExecutorForC001
	host string
}

func (a *netExecutorAdapter) Execute(cmd string, sudo bool) (commonos.NetExecResult, error) {
	r, err := a.e.Execute(cmd, sudo)
	if err != nil || r == nil {
		return nil, err
	}
	return &netResultAdapter{r: r}, nil
}

func (a *netExecutorAdapter) Host() string {
	return a.host
}

func newNetAdapter(h HostExec) *netExecutorAdapter {
	return &netExecutorAdapter{e: h.Executor, host: h.Host}
}

// RunNetworkValidation validates and/or auto-detects yac-public-network and yac-inter-cidr.
// It writes final values back to params and returns an error if validation fails.
func RunNetworkValidation(hosts []HostExec, params map[string]interface{}, logger *logging.Logger) error {
	if len(hosts) == 0 {
		return fmt.Errorf("no hosts for network validation")
	}

	firstHost := hosts[0].Host
	logger.ConsoleWithType("C-001A", "Network CIDR Validation", firstHost, "start", "", "", 0)
	logger.Info("Running network CIDR validation and auto-detection...")

	publicNetwork := getParamString(params, "yac_public_network", "")
	interCIDR := getParamString(params, "yac_inter_cidr", "")

	// === Phase 1: public-network ===
	detectedPublic, publicIface, err := detectOrValidatePublicNetwork(hosts, publicNetwork, logger)
	if err != nil {
		return fmt.Errorf("public-network validation failed: %w", err)
	}
	params["yac_public_network"] = detectedPublic
	logger.Info("Public network: %s (interface %s)", detectedPublic, publicIface)

	// === Phase 2: inter-cidr ===
	detectedInter, err := detectOrValidateInterCIDR(hosts, interCIDR, detectedPublic, logger)
	if err != nil {
		return fmt.Errorf("inter-cidr validation failed: %w", err)
	}
	params["yac_inter_cidr"] = detectedInter
	logger.Info("Inter-connect CIDR: %s", detectedInter)

	logger.ConsoleWithType("C-001A", "Network CIDR Validation", firstHost, "success", "",
		fmt.Sprintf("public=%s inter=%s", detectedPublic, detectedInter), time.Duration(0))
	return nil
}

// detectOrValidatePublicNetwork handles public-network: auto-detect when empty, validate when set.
// Returns the CIDR string and interface name.
func detectOrValidatePublicNetwork(hosts []HostExec, publicNetwork string, logger *logging.Logger) (string, string, error) {
	if publicNetwork == "" {
		return autoDetectPublicNetwork(hosts, logger)
	}
	return validatePublicNetwork(hosts, publicNetwork, logger)
}

// autoDetectPublicNetwork finds the CIDR that contains all host public IPs.
func autoDetectPublicNetwork(hosts []HostExec, logger *logging.Logger) (string, string, error) {
	firstAdapter := newNetAdapter(hosts[0])
	info, err := commonos.GetInterfaceForIP(firstAdapter, hosts[0].Host)
	if err != nil {
		return "", "", fmt.Errorf("cannot find interface for public IP %s on first node: %w", hosts[0].Host, err)
	}

	logger.Info("Auto-detected public network: %s (from interface %s on %s)", info.CIDR, info.Name, hosts[0].Host)

	for i := 1; i < len(hosts); i++ {
		inSubnet, err := commonos.IPInSubnet(hosts[i].Host, info.CIDR)
		if err != nil {
			return "", "", fmt.Errorf("failed to check node %s against CIDR %s: %w", hosts[i].Host, info.CIDR, err)
		}
		if !inSubnet {
			return "", "", fmt.Errorf("node %s (IP %s) is not in auto-detected public network %s", hosts[i].Host, hosts[i].Host, info.CIDR)
		}
	}

	return info.CIDR, info.Name, nil
}

// validatePublicNetwork checks that the user-provided public-network CIDR contains all host IPs.
func validatePublicNetwork(hosts []HostExec, publicNetwork string, logger *logging.Logger) (string, string, error) {
	_, _, err := net.ParseCIDR(publicNetwork)
	if err != nil {
		return "", "", fmt.Errorf("invalid public-network CIDR format %q: %w", publicNetwork, err)
	}

	firstAdapter := newNetAdapter(hosts[0])
	info, err := commonos.GetInterfaceForIP(firstAdapter, hosts[0].Host)
	if err != nil {
		return "", "", fmt.Errorf("cannot find interface for public IP %s on first node: %w", hosts[0].Host, err)
	}

	actualCIDR := info.CIDR
	if !sameCIDR(actualCIDR, publicNetwork) {
		return "", "", fmt.Errorf("provided public-network %s does not match actual public CIDR %s on node %s",
			publicNetwork, actualCIDR, hosts[0].Host)
	}

	for i := 1; i < len(hosts); i++ {
		inSubnet, err := commonos.IPInSubnet(hosts[i].Host, publicNetwork)
		if err != nil {
			return "", "", fmt.Errorf("failed to check node %s against CIDR %s: %w", hosts[i].Host, publicNetwork, err)
		}
		if !inSubnet {
			return "", "", fmt.Errorf("node %s (IP %s) is not in provided public network %s", hosts[i].Host, hosts[i].Host, publicNetwork)
		}
	}

	logger.Info("Public network %s validated: all nodes in subnet, interface %s", publicNetwork, info.Name)
	return publicNetwork, info.Name, nil
}

// detectOrValidateInterCIDR handles inter-cidr: auto-detect when empty, validate when set.
func detectOrValidateInterCIDR(hosts []HostExec, interCIDR, publicNetwork string, logger *logging.Logger) (string, error) {
	if interCIDR == "" {
		return autoDetectInterCIDR(hosts, publicNetwork, logger)
	}
	return validateInterCIDR(hosts, interCIDR, logger)
}

// autoDetectInterCIDR finds a consistent non-public network across all nodes.
// If none found, falls back to publicNetwork.
func autoDetectInterCIDR(hosts []HostExec, publicNetwork string, logger *logging.Logger) (string, error) {
	type ifaceKey struct {
		CIDR string
		Name string
	}

	// Collect non-public interfaces from each node
	nodeIfaces := make([][]commonos.InterfaceInfo, len(hosts))
	for i, h := range hosts {
		adapter := newNetAdapter(h)
		ifaces, err := commonos.GetHostInterfaces(adapter, h.Host)
		if err != nil {
			logger.Warn("Failed to get interfaces on node %s: %v", h.Host, err)
			nodeIfaces[i] = nil
			continue
		}
		nodeIfaces[i] = ifaces
	}

	// Build candidate map: for each (CIDR, ifaceName), count how many nodes have it
	candidateCounts := make(map[ifaceKey]int)
	for _, ifaces := range nodeIfaces {
		if ifaces == nil {
			continue
		}
		seen := make(map[ifaceKey]bool)
		for _, info := range ifaces {
			key := ifaceKey{CIDR: info.CIDR, Name: info.Name}
			if !seen[key] {
				seen[key] = true
				candidateCounts[key]++
			}
		}
	}

	// Find a candidate present on all nodes with same CIDR + same interface name
	nodeCount := len(hosts)
	var bestCandidate ifaceKey
	found := false
	for key, count := range candidateCounts {
		if count == nodeCount {
			if !found {
				bestCandidate = key
				found = true
			} else if key.CIDR < bestCandidate.CIDR {
				bestCandidate = key
			}
		}
	}

	if found {
		logger.Info("Auto-detected inter-connect network: %s (interface %s, consistent on all %d nodes)",
			bestCandidate.CIDR, bestCandidate.Name, nodeCount)
		return bestCandidate.CIDR, nil
	}

	logger.Warn("No dedicated inter-connect network found, using public network %s", publicNetwork)
	return publicNetwork, nil
}

// validateInterCIDR checks that the user-provided inter-cidr exists on all nodes
// and the interface name is consistent.
func validateInterCIDR(hosts []HostExec, interCIDR string, logger *logging.Logger) (string, error) {
	_, _, err := net.ParseCIDR(interCIDR)
	if err != nil {
		return "", fmt.Errorf("invalid inter-cidr CIDR format %q: %w", interCIDR, err)
	}

	var ifaceNames []string
	for _, h := range hosts {
		adapter := newNetAdapter(h)
		ifaces, err := commonos.GetHostInterfaces(adapter, "")
		if err != nil {
			return "", fmt.Errorf("failed to get interfaces on node %s: %w", h.Host, err)
		}

		var matchedIface string
		for _, info := range ifaces {
			if sameCIDR(info.CIDR, interCIDR) {
				matchedIface = info.Name
				break
			}
		}
		if matchedIface == "" {
			return "", fmt.Errorf("inter-cidr %s not found on node %s (no interface matches this subnet)", interCIDR, h.Host)
		}
		ifaceNames = append(ifaceNames, matchedIface)
		logger.Info("Node %s: inter-cidr %s matched interface %s", h.Host, interCIDR, matchedIface)
	}

	refIface := ifaceNames[0]
	for i := 1; i < len(ifaceNames); i++ {
		if ifaceNames[i] != refIface {
			var details []string
			for j, h := range hosts {
				details = append(details, fmt.Sprintf("node %s uses %s", h.Host, ifaceNames[j]))
			}
			return "", fmt.Errorf("inter-cidr %s interface mismatch: %s", interCIDR, strings.Join(details, ", "))
		}
	}

	logger.Info("Inter-connect CIDR %s validated: interface %s on all %d nodes", interCIDR, refIface, len(hosts))
	return interCIDR, nil
}

// sameCIDR checks if two CIDR strings represent the same network.
func sameCIDR(a, b string) bool {
	_, netA, errA := net.ParseCIDR(a)
	_, netB, errB := net.ParseCIDR(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return netA.String() == netB.String()
}
