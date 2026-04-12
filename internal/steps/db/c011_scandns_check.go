package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// scanPingResultAdapter 将 runner.ExecResult 适配为 commonos.PingExecResult
type scanPingResultAdapter struct {
	r runner.ExecResult
}

func (a *scanPingResultAdapter) GetExitCode() int {
	if a.r == nil {
		return -1
	}
	return a.r.GetExitCode()
}

// scanPingExecutorAdapter 将 runner.Executor 适配为 commonos.PingExecutor
type scanPingExecutorAdapter struct {
	e runner.Executor
}

func (a *scanPingExecutorAdapter) Execute(cmd string, sudo bool) (commonos.PingExecResult, error) {
	r, err := a.e.Execute(cmd, sudo)
	if err != nil || r == nil {
		return nil, err
	}
	return &scanPingResultAdapter{r: r}, nil
}

// StepC004ScanDNS Validate SCAN DNS resolution
func StepC011ScanDNS() *runner.Step {
	return &runner.Step{
		ID:          "C-011",
		Name:        "Validate SCAN DNS",
		Description: "Validate SCAN DNS resolution, subnet and IP availability for YAC cluster",
		Tags:        []string{"db", "yac", "dns", "scan"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if !isYACMode {
				return fmt.Errorf("not in YAC mode, skipping")
			}

			accessMode := ctx.GetParamString("yac_access_mode", "vip")
			if accessMode != "scan" {
				return fmt.Errorf("not using SCAN mode, skipping")
			}

			scanMode := ctx.GetParamString("yac_scan_mode", "")
			if scanMode == "local" {
				return fmt.Errorf("local SCAN mode: DNS validation not required (using /etc/hosts)")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			scanName := ctx.GetParamString("yac_scanname", "")
			publicNet := ctx.GetParamString("yac_public_network", "")
			interCIDR := ctx.GetParamString("yac_inter_cidr", "")

			// 1. 解析 SCAN 名为 IP 地址列表
			hctx := ctx.ForHost(ctx.HostsToRun()[0])
			hctx.Logger.Info("Resolving SCAN name: %s", scanName)
			resolvedIPs, err := commonos.ResolveHostnameToIP(scanName)
			if err != nil {
				return fmt.Errorf("failed to resolve SCAN name %s: %w", scanName, err)
			}
			if len(resolvedIPs) == 0 {
				return fmt.Errorf("SCAN name %s resolved to no IP addresses", scanName)
			}
			hctx.Logger.Info("Resolved %s to IP(s): %v", scanName, resolvedIPs)

			// 2. 确定用于"同网段"判断的 CIDR（业务网段）
			var cidr string
			if strings.Contains(publicNet, "/") {
				cidr = strings.TrimSpace(publicNet)
			} else if strings.Contains(interCIDR, "/") {
				cidr = strings.TrimSpace(interCIDR)
			} else {
				// 从第一个节点的永久 IP 推导 /24 网段
				firstHostIP := ctx.HostsToRun()[0].Host
				var errCIDR error
				cidr, errCIDR = commonos.CIDRFromIP(firstHostIP, 24)
				if errCIDR != nil {
					return fmt.Errorf("cannot derive CIDR from permanent IP %s (yac_public_network and yac_inter_cidr are not CIDR): %w", firstHostIP, errCIDR)
				}
				hctx.Logger.Info("Using derived CIDR for subnet check: %s", cidr)
			}
			hctx.Logger.Info("Business network CIDR: %s", cidr)

			// 3. 校验解析出的 IP 均在业务网段内
			for _, ip := range resolvedIPs {
				inSubnet, errSub := commonos.IPInSubnet(ip, cidr)
				if errSub != nil {
					return fmt.Errorf("check resolved IP %s: %w", ip, errSub)
				}
				if !inSubnet {
					return fmt.Errorf("resolved IP %s for SCAN name %s is not in the same subnet as business network (CIDR %s)", ip, scanName, cidr)
				}
				hctx.Logger.Info("  ✓ IP %s is in subnet %s", ip, cidr)
			}

			// 4. 对所有解析出的 IP 进行 ping 探测，如果有 IP 存活，报错退出
			// 使用第一个节点的执行器进行 ping 探测
			pingExecutor := &scanPingExecutorAdapter{e: hctx.Executor}
			for _, ip := range resolvedIPs {
				hctx.Logger.Info("Checking if IP %s is alive...", ip)
				isAlive, err := commonos.PingFromHost(pingExecutor, ip)
				if err != nil {
					return fmt.Errorf("failed to ping SCAN IP %s: %w", ip, err)
				}
				if isAlive {
					return fmt.Errorf("SCAN IP %s is already in use (ping responded); SCAN IPs must not be in use before installation", ip)
				}
				hctx.Logger.Info("  ✓ IP %s is not in use", ip)
			}

			// 5. 在所有节点上验证 DNS 解析（保持原有逻辑）
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				hctx.Logger.Info("Validating SCAN DNS resolution on %s...", th.Host)
				hctx.Logger.Info("  SCAN Name: %s", scanName)
				result, _ := hctx.Execute(fmt.Sprintf("host %s 2>/dev/null || nslookup %s 2>/dev/null", scanName, scanName), false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("failed to resolve SCAN name %s on %s", scanName, th.Host)
				}
				hctx.Logger.Info("DNS resolution result:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if strings.TrimSpace(line) != "" {
						hctx.Logger.Info("  %s", line)
					}
				}
				result, _ = hctx.Execute(fmt.Sprintf("getent hosts %s", scanName), false)
				if result != nil && result.GetExitCode() == 0 {
					hctx.Logger.Info("Host entry: %s", strings.TrimSpace(result.GetStdout()))
				}
			}

			hctx.Logger.Info("SCAN DNS validation completed successfully")
			hctx.Logger.Info("  SCAN Name: %s", scanName)
			hctx.Logger.Info("  Resolved IPs: %v", resolvedIPs)
			hctx.Logger.Info("  All IPs are in subnet: %s", cidr)
			hctx.Logger.Info("  All IPs are not in use (ping check passed)")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
