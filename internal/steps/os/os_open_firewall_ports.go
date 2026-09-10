// os_open_firewall_ports.go - --os-firewall-mode=enable 且 firewalld 已 active 时幂等放行端口
// 永不 start firewalld；公有网已在 trusted 则跳过 public 加端口；YAC 另将 yac_inter_cidr 加入 trusted

package os

import (
	"fmt"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepOpenFirewallPorts 在 firewalld 已 active 时幂等放行 Yashan 端口（--os-firewall-mode=enable）
func stepOpenFirewallPorts() *runner.Step {
	return &runner.Step{
		Name:        "Open Firewall Ports",
		Description: "Open specified ports in firewall",
		Tags:        []string{"os", "firewall"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			mode, err := commonos.NormalizeFirewallMode(ctx.GetParamString("os_firewall_mode", commonos.FirewallModeEnable))
			if err != nil {
				return err
			}
			if mode != commonos.FirewallModeEnable {
				return fmt.Errorf("firewall mode is not enable")
			}
			result, _ := ctx.Execute("systemctl is-active firewalld 2>/dev/null", false)
			if result == nil || strings.TrimSpace(result.GetStdout()) != "active" {
				return fmt.Errorf("firewalld is not active, skipping port open (will not start firewalld)")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-018: Open Firewall Ports")
			needReload := false

			skipPorts, pubCIDR, err := publicNetworkAlreadyTrusted(ctx)
			if err != nil {
				return err
			}
			if skipPorts {
				ctx.Logger.Info("Public network %s already in trusted; skip public zone ports", pubCIDR)
			} else {
				want, err := resolveFirewallWantPorts(ctx)
				if err != nil {
					return err
				}
				listed, err := listFirewallPublicPorts(ctx)
				if err != nil {
					return err
				}
				have := make(map[string]bool, len(listed))
				for _, p := range listed {
					have[p] = true
				}
				for _, p := range want {
					if have[p] {
						ctx.Logger.Info("Firewall port already open: %s", p)
						continue
					}
					cmd := fmt.Sprintf("firewall-cmd --zone=public --add-port=%s --permanent", p)
					if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
						return fmt.Errorf("failed to add firewall port %s: %w", p, err)
					}
					needReload = true
				}
			}

			yacMode := ctx.GetParamBool("yac_mode", false)
			interCIDR := strings.TrimSpace(ctx.GetParamString("yac_inter_cidr", ""))
			if yacMode {
				if interCIDR == "" {
					ctx.Logger.Warn("YAC mode: yac_inter_cidr empty, skip trusted private-network allow")
				} else {
					added, err := ensureTrustedSource(ctx, interCIDR)
					if err != nil {
						return err
					}
					if added {
						needReload = true
					}
				}
			}

			if needReload {
				if _, err := ctx.ExecuteWithCheck("firewall-cmd --reload", true); err != nil {
					return fmt.Errorf("failed to reload firewalld: %w", err)
				}
			} else {
				osLogPhase(ctx, "skip", "already_configured=firewall_ports")
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			skipPorts, _, err := publicNetworkAlreadyTrusted(ctx)
			if err != nil {
				return err
			}
			if !skipPorts {
				want, err := resolveFirewallWantPorts(ctx)
				if err != nil {
					return err
				}
				listed, err := listFirewallPublicPorts(ctx)
				if err != nil {
					return err
				}
				have := make(map[string]bool, len(listed))
				for _, p := range listed {
					have[p] = true
				}
				for _, p := range want {
					if !have[p] {
						return fmt.Errorf("firewall port %s not open after configure", p)
					}
				}
			}
			yacMode := ctx.GetParamBool("yac_mode", false)
			interCIDR := strings.TrimSpace(ctx.GetParamString("yac_inter_cidr", ""))
			if yacMode && interCIDR != "" {
				ok, err := trustedSourcePresent(ctx, interCIDR)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("trusted source %s not present after configure", interCIDR)
				}
			}
			return nil
		},
	}
}

func resolveFirewallWantPorts(ctx *runner.StepContext) ([]string, error) {
	portsStr := strings.TrimSpace(ctx.GetParamString("os_firewall_ports", ""))
	if portsStr != "" {
		var want []string
		for _, port := range strings.Split(portsStr, ",") {
			port = strings.TrimSpace(port)
			if port == "" {
				continue
			}
			if strings.Contains(port, "/") {
				want = append(want, port)
			} else {
				want = append(want, port+"/tcp")
			}
		}
		if len(want) == 0 {
			return nil, fmt.Errorf("os_firewall_ports is empty after parse")
		}
		return want, nil
	}
	begin := ctx.GetParamInt("db_begin_port", 1688)
	yac := ctx.GetParamBool("yac_mode", false)
	ports := commonos.YashanFirewallTCPPorts(begin, yac)
	if len(ports) == 0 {
		return nil, fmt.Errorf("auto firewall port set is empty (begin_port=%d)", begin)
	}
	want := make([]string, 0, len(ports))
	for _, p := range ports {
		want = append(want, strconv.Itoa(p)+"/tcp")
	}
	ctx.Logger.Info("Firewall auto port set: %v (begin=%d yac=%v)", want, begin, yac)
	return want, nil
}

func listFirewallPublicPorts(ctx *runner.StepContext) ([]string, error) {
	result, err := ctx.Execute("firewall-cmd --zone=public --list-ports 2>/dev/null", false)
	if err != nil || result == nil || result.GetExitCode() != 0 {
		return nil, fmt.Errorf("failed to list firewall ports")
	}
	return strings.Fields(strings.TrimSpace(result.GetStdout())), nil
}

// publicNetworkAlreadyTrusted 若 yac_public_network 已在 trusted sources 中则无需再开 public 端口。
func publicNetworkAlreadyTrusted(ctx *runner.StepContext) (bool, string, error) {
	pub := strings.TrimSpace(ctx.GetParamString("yac_public_network", ""))
	if pub == "" {
		return false, "", nil
	}
	ok, err := trustedSourcePresent(ctx, pub)
	if err != nil {
		return false, pub, err
	}
	return ok, pub, nil
}

func trustedSourcePresent(ctx *runner.StepContext, cidr string) (bool, error) {
	result, err := ctx.Execute("firewall-cmd --zone=trusted --list-sources 2>/dev/null", false)
	if err != nil || result == nil || result.GetExitCode() != 0 {
		return false, fmt.Errorf("failed to list trusted sources")
	}
	return commonos.TrustedSourcesContain(result.GetStdout(), cidr), nil
}

// ensureTrustedSource 若 trusted zone 尚无该 CIDR 则 permanent 添加；返回是否新增。
func ensureTrustedSource(ctx *runner.StepContext, cidr string) (bool, error) {
	ok, err := trustedSourcePresent(ctx, cidr)
	if err != nil {
		return false, err
	}
	if ok {
		ctx.Logger.Info("Trusted source already present: %s", cidr)
		return false, nil
	}
	cmd := fmt.Sprintf("firewall-cmd --permanent --zone=trusted --add-source=%s", cidr)
	if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
		return false, fmt.Errorf("failed to add trusted source %s: %w", cidr, err)
	}
	return true, nil
}
