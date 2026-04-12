package db

import (
	"fmt"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
)

// StepC005ScanNameCheck SCAN 名解析与网段校验步骤；实际逻辑由 RunScanNameResolveAndSubnetCheck 在 db 命令中调用
func StepC013ScanNameCheck() *runner.Step {
	return &runner.Step{
		ID:          "C-013",
		Name:        "Resolve SCAN Name and Check Subnet",
		Description: "YAC scan mode: resolve SCAN name to IPs and verify same subnet as permanent IPs",
		Tags:        []string{"db", "yac", "scan", "validation"},
		Optional:    true, // Optional: skip in standalone or VIP mode

		PreCheck: func(ctx *runner.StepContext) error {
			isYACMode := ctx.GetParamBool("yac_mode", false)
			accessMode := ctx.GetParamString("yac_access_mode", "vip")
			scanName := ctx.GetParamString("yac_scanname", "")

			if !isYACMode {
				return fmt.Errorf("standalone mode: SCAN configuration not required")
			}
			if accessMode != "scan" {
				return fmt.Errorf("VIP mode: SCAN configuration not required")
			}

			scanMode := ctx.GetParamString("yac_scan_mode", "")
			if scanMode == "local" {
				return fmt.Errorf("local SCAN mode: DNS resolve check not required (using /etc/hosts)")
			}

			if scanName == "" {
				return fmt.Errorf("SCAN mode enabled but scanname not specified")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			// 实际解析与网段校验在 db.go 中通过 RunScanNameResolveAndSubnetCheck 执行，此处仅占位
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

// RunScanNameResolveAndSubnetCheck 在 YAC scan 模式下解析 SCAN 名并校验解析出的 IP 与永久 IP 同网段
func RunScanNameResolveAndSubnetCheck(hosts []HostExec, params map[string]interface{}, logger *logging.Logger) error {
	if len(hosts) == 0 {
		return nil
	}
	accessMode := getParamString(params, "yac_access_mode", "vip")
	if accessMode != "scan" {
		return nil
	}
	scanMode := getParamString(params, "yac_scan_mode", "")
	if scanMode == "local" {
		logger.Info("Local SCAN mode: skipping DNS resolve and subnet check")
		return nil
	}
	scanName := getParamString(params, "yac_scanname", "")
	if scanName == "" {
		return nil
	}

	firstHost := hosts[0].Host
	logger.ConsoleWithType("C-013-SCAN", "Resolve SCAN Name and Check Subnet", firstHost, "start", "", "", 0)
	logger.Info("Resolving SCAN name: %s", scanName)

	// 1. 解析 SCAN 名为 IP 地址列表
	resolvedIPs, err := commonos.ResolveHostnameToIP(scanName)
	if err != nil {
		return fmt.Errorf("resolve SCAN name %s failed: %w", scanName, err)
	}
	logger.Info("Resolved %s to IP(s): %v", scanName, resolvedIPs)

	// 2. 确定用于“同网段”判断的 CIDR
	publicNet := getParamString(params, "yac_public_network", "")
	interCIDR := getParamString(params, "yac_inter_cidr", "")
	var cidr string
	if strings.Contains(publicNet, "/") {
		cidr = strings.TrimSpace(publicNet)
	} else if strings.Contains(interCIDR, "/") {
		cidr = strings.TrimSpace(interCIDR)
	} else {
		// 从第一个永久 IP 推导 /24 网段
		var errCIDR error
		cidr, errCIDR = commonos.CIDRFromIP(hosts[0].Host, 24)
		if errCIDR != nil {
			return fmt.Errorf("cannot derive CIDR from permanent IP %s (yac_public_network and yac_inter_cidr are not CIDR): %w", hosts[0].Host, errCIDR)
		}
		logger.Info("Using derived CIDR for subnet check: %s", cidr)
	}

	// 3. 校验解析出的 IP 均在 CIDR 网段内
	for _, ip := range resolvedIPs {
		inSubnet, errSub := commonos.IPInSubnet(ip, cidr)
		if errSub != nil {
			return fmt.Errorf("check resolved IP %s: %w", ip, errSub)
		}
		if !inSubnet {
			return fmt.Errorf("resolved IP %s for SCAN name %s is not in the same subnet as permanent IPs (CIDR %s)", ip, scanName, cidr)
		}
	}

	// 4. 校验所有永久 IP 均在同一 CIDR 网段内（与解析 IP 同网段）
	for _, h := range hosts {
		permIP := strings.TrimSpace(h.Host)
		if permIP == "" {
			continue
		}
		inSubnet, errSub := commonos.IPInSubnet(permIP, cidr)
		if errSub != nil {
			return fmt.Errorf("check permanent IP %s: %w", permIP, errSub)
		}
		if !inSubnet {
			return fmt.Errorf("permanent IP %s is not in the same subnet as SCAN resolved IPs (CIDR %s)", permIP, cidr)
		}
	}

	logger.Info("SCAN name %s resolved to %v; all IPs are in subnet %s", scanName, resolvedIPs, cidr)
	logger.ConsoleWithType("C-013-SCAN", "Resolve SCAN Name and Check Subnet", firstHost, "success", "", fmt.Sprintf("IPs: %v", resolvedIPs), time.Duration(0))
	return nil
}
