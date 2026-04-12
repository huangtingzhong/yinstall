package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC010WriteHosts writes VIP and SCAN entries to /etc/hosts on all nodes
func StepC010WriteHosts() *runner.Step {
	return &runner.Step{
		ID:          "C-010",
		Name:        "Write Cluster Hosts",
		Description: "Write VIP and SCAN entries to /etc/hosts on all YAC nodes",
		Tags:        []string{"db", "yac", "hosts"},
		Optional:    true,
		Global:      true,

		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("yac_mode", false) {
				ctx.Logger.Info("Not YAC mode, skipping")
				return fmt.Errorf("not YAC mode")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			hosts := ctx.HostsToRun()
			accessMode := ctx.GetParamString("yac_access_mode", "vip")

			existingEntries := commonos.ReadManagedHostsEntries(ctx.ForHost(hosts[0]))
			ctx.Logger.Info("Existing managed /etc/hosts entries: %v", existingEntries)

			var newEntries []string
			newEntries = append(newEntries, existingEntries...)

			vips := ctx.GetParamStringSlice("yac_vips")
			if len(vips) > 0 {
				for i, th := range hosts {
					hostname := getHostname(ctx.ForHost(th))
					if hostname == "" {
						hostname = fmt.Sprintf("node%d", i+1)
					}
					if i < len(vips) {
						vipEntry := fmt.Sprintf("%s  %s-vip", strings.TrimSpace(vips[i]), hostname)
						newEntries = appendIfNotExists(newEntries, vipEntry)
						ctx.Logger.Info("Adding VIP entry: %s", vipEntry)
					}
				}
			}

			if accessMode == "scan" {
				scanMode := ctx.GetParamString("yac_scan_mode", "")
				if scanMode == "local" {
					scanName := ctx.GetParamString("yac_scanname", "")
					scanIPs := ctx.GetParamStringSlice("yac_scan_ips_list")
					for _, ip := range scanIPs {
						scanEntry := fmt.Sprintf("%s  %s", strings.TrimSpace(ip), scanName)
						newEntries = appendIfNotExists(newEntries, scanEntry)
						ctx.Logger.Info("Adding SCAN entry: %s", scanEntry)
					}
				}
			}

			if len(newEntries) == 0 {
				ctx.Logger.Info("No entries to write to /etc/hosts")
				return nil
			}

			ctx.Logger.Info("Writing %d entries to /etc/hosts on all nodes", len(newEntries))
			for _, th := range hosts {
				if err := commonos.UpdateManagedHostsBlock(ctx.ForHost(th), newEntries); err != nil {
					return fmt.Errorf("[%s] failed to update /etc/hosts: %w", th.Host, err)
				}
				ctx.Logger.Info("[%s] /etc/hosts updated", th.Host)
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			hosts := ctx.HostsToRun()
			accessMode := ctx.GetParamString("yac_access_mode", "vip")

			for _, th := range hosts {
				hctx := ctx.ForHost(th)
				hostname := getHostname(hctx)
				if hostname == "" {
					continue
				}
				result, _ := hctx.Execute(fmt.Sprintf("getent hosts %s", hostname), false)
				if result != nil && result.GetStdout() != "" {
					ctx.Logger.Info("[%s] getent hosts %s: %s", th.Host, hostname, strings.TrimSpace(result.GetStdout()))
				}
			}

			if accessMode == "scan" {
				scanMode := ctx.GetParamString("yac_scan_mode", "")
				if scanMode == "local" {
					scanName := ctx.GetParamString("yac_scanname", "")
					if scanName != "" {
						hctx := ctx.ForHost(hosts[0])
						result, _ := hctx.Execute(fmt.Sprintf("getent hosts %s", scanName), false)
						if result != nil && result.GetStdout() != "" {
							ctx.Logger.Info("getent hosts %s: %s", scanName, strings.TrimSpace(result.GetStdout()))
						}
					}
				}
			}
			return nil
		},
	}
}

func getHostname(ctx *runner.StepContext) string {
	result, _ := ctx.Execute("hostname", false)
	if result != nil {
		return strings.TrimSpace(result.GetStdout())
	}
	return ""
}

func appendIfNotExists(entries []string, entry string) []string {
	for _, e := range entries {
		if e == entry {
			return entries
		}
	}
	return append(entries, entry)
}
