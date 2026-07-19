// r015_network_interfaces.go - 网卡信息采集
// 采集网络接口详情，RHEL8+ 优先使用 nmcli，RHEL7/旧系统使用 /proc/net/bonding 等，
// 同时采集 IRQ/RPS/XPS 绑核信息，写入 os/network/ 目录。
package collect

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// stepNetworkInterfaces 返回 R-015 步骤：采集网卡信息。
func stepNetworkInterfaces() *runner.Step {
	return &runner.Step{
		Name: "Collect network interfaces",
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "os", "network")

			branch := "rhel8+_nmcli"
			if ctx.OSInfo != nil && ctx.OSInfo.IsRHEL7 {
				branch = "rhel7_bonding"
			}
			collectLogPhase(ctx, "plan",
				fmt.Sprintf("dir=os/network branch=%s common=5 +nmcli/bonding +irq_rps_xps", branch))

			// 通用命令（RHEL7/8 均适用）
			commonCmds := []struct {
				cmd  string
				dest string
			}{
				{"ip addr show", filepath.Join(dir, "ip-addr.txt")},
				{"ip link show", filepath.Join(dir, "ip-link.txt")},
				{"ip -s link show", filepath.Join(dir, "ip-link-stats.txt")},
				{"ethtool -i $(ip route show default | awk '/dev/{print $5}' | head -1) 2>/dev/null || true", filepath.Join(dir, "ethtool-default-iface.txt")},
				{"cat /proc/net/dev 2>/dev/null || true", filepath.Join(dir, "proc-net-dev.txt")},
			}

			for _, c := range commonCmds {
				if err := runAndSave(ctx, c.cmd, c.dest); err != nil {
					appendWarning(ctx, err.Error())
				}
			}

			// RHEL8+ / 非 RHEL7：优先使用 nmcli 获取详细设备信息
			if ctx.OSInfo == nil || !ctx.OSInfo.IsRHEL7 {
				nmcliCmds := []struct {
					cmd  string
					dest string
				}{
					{"nmcli device show 2>/dev/null || true", filepath.Join(dir, "nmcli-device.txt")},
					{"nmcli connection show 2>/dev/null || true", filepath.Join(dir, "nmcli-conn.txt")},
				}
				for _, c := range nmcliCmds {
					if err := runAndSave(ctx, c.cmd, c.dest); err != nil {
						appendWarning(ctx, err.Error())
					}
				}
			}

			// RHEL7：使用 /proc/net/bonding 采集 bond 信息
			if ctx.OSInfo != nil && ctx.OSInfo.IsRHEL7 {
				_ = runAndSave(ctx, "ls /proc/net/bonding/ 2>/dev/null && cat /proc/net/bonding/* 2>/dev/null || true", filepath.Join(dir, "bonding.txt"))
			} else {
				_ = runAndSave(ctx, "ls /proc/net/bonding/ 2>/dev/null && cat /proc/net/bonding/* 2>/dev/null || true", filepath.Join(dir, "bonding.txt"))
			}

			// IRQ / RPS / XPS 绑核信息
			_ = runAndSave(ctx, "cat /proc/interrupts 2>/dev/null | head -100 || true", filepath.Join(dir, "interrupts.txt"))
			_ = runAndSave(ctx, "for f in /sys/class/net/*/queues/*/rps_cpus; do echo \"$f: $(cat $f 2>/dev/null)\"; done 2>/dev/null || true", filepath.Join(dir, "rps-cpus.txt"))
			_ = runAndSave(ctx, "for f in /sys/class/net/*/queues/*/xps_cpus; do echo \"$f: $(cat $f 2>/dev/null)\"; done 2>/dev/null || true", filepath.Join(dir, "xps-cpus.txt"))

			ctx.Logger.Info("[R-015] network interfaces collected to %s", dir)
			return nil
		},
	}
}
