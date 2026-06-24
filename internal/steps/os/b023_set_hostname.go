package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

const paramHostnameDefaultPrefix = "os_hostname_default_prefix"

// StepB023SetHostname 配置各节点主机名（及 /etc/hosts 托管块）
func StepB023SetHostname() *runner.Step {
	return &runner.Step{
		ID:          "B-023",
		Name:        "Set Hostname",
		Description: "Configure system hostname on all nodes",
		Tags:        []string{"os", "hostname"},
		Optional:    true,
		Global:      true,

		PreCheck: func(ctx *runner.StepContext) error {
			hostnameParam := ctx.GetParamString("os_hostname", "")
			hostnames := commonos.ParseHostnames(hostnameParam)
			targetCount := len(ctx.HostsToRun())

			if targetCount > 1 {
				if len(hostnames) > 1 && len(hostnames) != targetCount {
					return fmt.Errorf("hostname count (%d) does not match node count (%d), provide 1 prefix or %d hostnames",
						len(hostnames), targetCount, targetCount)
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			hostnameParam := ctx.GetParamString("os_hostname", "")
			explicitHostnames := commonos.ParseHostnames(hostnameParam)
			userSpecified := len(explicitHostnames) > 0
			prefix := hostnameDefaultPrefix(ctx)
			hosts := ctx.HostsToRun()
			osLogPhase(ctx, "plan", fmt.Sprintf("hosts=%d prefix=%s user_specified=%v op=hostname+hosts-block",
				len(hosts), prefix, userSpecified))

			type nodeInfo struct {
				ip       string
				hostname string
			}
			var nodes []nodeInfo

			for i, th := range hosts {
				osLogPhase(ctx, "host-start", fmt.Sprintf("host=%s idx=%d", th.Host, i+1))
				hctx := ctx.ForHost(th)
				targetHostname := commonos.TargetHostnameFromRules(prefix, len(hosts), i, explicitHostnames)

				currentHostname, err := readCurrentHostname(hctx)
				if err != nil {
					return fmt.Errorf("[%s] failed to read current hostname: %w", th.Host, err)
				}

				effectiveHostname := targetHostname
				if userSpecified || commonos.ShouldReplaceHostnameWhenUnset(currentHostname) {
					if currentHostname != targetHostname {
						ctx.Logger.Info("[%s] Setting hostname: %s -> %s", th.Host, currentHostname, targetHostname)
						if err := setSystemHostname(hctx, th.Host, targetHostname); err != nil {
							return err
						}
					} else {
						ctx.Logger.Info("[%s] Hostname already %s", th.Host, targetHostname)
					}
				} else {
					effectiveHostname = currentHostname
					ctx.Logger.Info("[%s] Keeping hostname %s (--os-hostname empty and not a system default name)",
						th.Host, currentHostname)
				}

				ip := th.Host
				if ip == "localhost" || ip == "127.0.0.1" {
					if r, _ := hctx.Execute("hostname -I | awk '{print $1}'", false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
						ip = strings.TrimSpace(r.GetStdout())
					}
				}
				nodes = append(nodes, nodeInfo{ip: ip, hostname: effectiveHostname})
				ctx.Logger.Info("[%s] Hosts entry: %s -> %s", th.Host, ip, effectiveHostname)
				osLogPhase(hctx, "host-done", fmt.Sprintf("host=%s hostname=%s", th.Host, effectiveHostname))
			}

			if len(nodes) > 0 {
				var entries []string
				for _, n := range nodes {
					entries = append(entries, fmt.Sprintf("%s  %s", n.ip, n.hostname))
				}
				ctx.Logger.Info("Writing hostname entries to /etc/hosts on all nodes: %v", entries)
				for _, th := range hosts {
					osLogPhase(ctx, "host-start", fmt.Sprintf("host=%s op=etc-hosts-block", th.Host))
					hctx := ctx.ForHost(th)
					if err := commonos.UpdateManagedHostsBlock(hctx, entries); err != nil {
						return fmt.Errorf("[%s] failed to update /etc/hosts: %w", th.Host, err)
					}
					osLogPhase(hctx, "host-done", fmt.Sprintf("host=%s op=etc-hosts-block", th.Host))
				}
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			hosts := ctx.HostsToRun()
			for _, th := range hosts {
				hctx := ctx.ForHost(th)
				result, err := hctx.Execute("hostname", false)
				if err != nil {
					return fmt.Errorf("[%s] failed to verify hostname: %w", th.Host, err)
				}
				if result != nil {
					ctx.Logger.Info("[%s] Current hostname: %s", th.Host, strings.TrimSpace(result.GetStdout()))
				}
			}
			return nil
		},
	}
}

func hostnameDefaultPrefix(ctx *runner.StepContext) string {
	prefix := strings.TrimSpace(ctx.GetParamString(paramHostnameDefaultPrefix, ""))
	if prefix == "" {
		return commonos.DefaultHostnamePrefixYashan
	}
	return prefix
}

func readCurrentHostname(hctx *runner.StepContext) (string, error) {
	result, err := hctx.Execute("hostname", false)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("empty hostname command result")
	}
	if result.GetExitCode() != 0 {
		return "", fmt.Errorf("hostname exit=%d: %s", result.GetExitCode(), strings.TrimSpace(result.GetStderr()))
	}
	return commonos.NormalizeHostname(result.GetStdout()), nil
}

func setSystemHostname(hctx *runner.StepContext, hostLabel, name string) error {
	cmd := fmt.Sprintf("hostnamectl set-hostname %s", name)
	result, err := hctx.Execute(cmd, true)
	if err != nil {
		return fmt.Errorf("[%s] failed to set hostname: %w", hostLabel, err)
	}
	if result != nil && result.GetExitCode() != 0 {
		return fmt.Errorf("[%s] hostnamectl failed: %s", hostLabel, result.GetStderr())
	}
	return nil
}
