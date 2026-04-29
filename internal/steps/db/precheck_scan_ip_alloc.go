package db

import (
	"fmt"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/logging"
)

// RunScanIPAllocation 为本地 SCAN 模式分配 SCAN IP。
// 若提供 --yac-scan-ips 则校验；否则从最后一个 VIP 之后的下一 IP 起自动分配 1 个 SCAN IP。
func RunScanIPAllocation(hosts []HostExec, params map[string]interface{}, logger *logging.Logger) error {
	if len(hosts) == 0 {
		return fmt.Errorf("no hosts for SCAN IP allocation")
	}

	firstHost := hosts[0].Host
	logger.ConsoleWithType("C-013-SCAN", "Local SCAN IP Allocation", firstHost, "start", "", "", 0)
	logger.Info("Running local SCAN IP allocation...")

	scanIPsRaw := getParamString(params, "yac_scan_ips", "")
	scanName := getParamString(params, "yac_scanname", "")

	if scanIPsRaw != "" {
		scanIPs := parseScanIPs(scanIPsRaw)
		for i, ip := range scanIPs {
			ip = strings.TrimSpace(ip)
			if ip == "" {
				return fmt.Errorf("SCAN IP at index %d is empty", i)
			}
			if !commonos.IsValidIPv4(ip) {
				return fmt.Errorf("SCAN IP %s is not a valid IPv4 address", ip)
			}
		}
		for _, ip := range scanIPs {
			inUse, err := commonos.PingFromHost(&pingExecutorAdapter{e: hosts[0].Executor}, ip)
			if err != nil {
				return fmt.Errorf("failed to check SCAN IP %s: %w", ip, err)
			}
			if inUse {
				return fmt.Errorf("SCAN IP %s is already in use (ping responded)", ip)
			}
		}
		params["yac_scan_ips_list"] = scanIPs
		logger.Info("SCAN IPs validated: %v", scanIPs)
		logger.ConsoleWithType("C-013-SCAN", "Local SCAN IP Allocation", firstHost, "success", "",
			fmt.Sprintf("SCAN: %s -> %v", scanName, scanIPs), time.Duration(0))
		return nil
	}

	// 从最后一个 VIP 之后的下一地址起自动分配 1 个 SCAN IP
	excludeIPs := make(map[string]bool)
	for _, h := range hosts {
		excludeIPs[strings.TrimSpace(h.Host)] = true
	}

	vips := getParamStringSliceFromParams(params, "yac_vips")
	for _, vip := range vips {
		excludeIPs[strings.TrimSpace(vip)] = true
	}

	var startIP string
	if len(vips) > 0 {
		lastVIP := strings.TrimSpace(vips[len(vips)-1])
		next, ok := commonos.NextIPv4(lastVIP)
		if !ok {
			return fmt.Errorf("cannot compute next IP after last VIP %s", lastVIP)
		}
		startIP = next
	} else {
		lastHost := strings.TrimSpace(hosts[len(hosts)-1].Host)
		next, ok := commonos.NextIPv4(lastHost)
		if !ok {
			return fmt.Errorf("cannot compute next IP after last host %s", lastHost)
		}
		startIP = next
	}

	var scanIP string
	candidate := startIP
	for attempt := 0; attempt < maxVIPSearchAttempts; attempt++ {
		if excludeIPs[candidate] {
			next, ok := commonos.NextIPv4(candidate)
			if !ok {
				break
			}
			candidate = next
			continue
		}
		inUse, err := commonos.PingFromHost(&pingExecutorAdapter{e: hosts[0].Executor}, candidate)
		if err != nil {
			return fmt.Errorf("failed to check candidate SCAN IP %s: %w", candidate, err)
		}
		if !inUse {
			scanIP = candidate
			break
		}
		next, ok := commonos.NextIPv4(candidate)
		if !ok {
			break
		}
		candidate = next
	}

	if scanIP == "" {
		return fmt.Errorf("no available SCAN IP found after %d attempts; please specify --yac-scan-ips manually", maxVIPSearchAttempts)
	}

	params["yac_scan_ips_list"] = []string{scanIP}
	logger.Info("Auto-generated SCAN IP: %s (name: %s)", scanIP, scanName)
	logger.ConsoleWithType("C-013-SCAN", "Local SCAN IP Allocation", firstHost, "success", "",
		fmt.Sprintf("SCAN: %s -> %s", scanName, scanIP), time.Duration(0))
	return nil
}

func parseScanIPs(raw string) []string {
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
