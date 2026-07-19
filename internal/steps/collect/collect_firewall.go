// r017_firewall.go - 防火墙状态采集（可选）
// 采集 firewalld 和 iptables 规则，写入 os/firewall.txt。
// 若两者均不可用则跳过（Optional=true）。
package collect

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// stepFirewall 返回 R-017 步骤：采集防火墙规则（Optional）。
func stepFirewall() *runner.Step {
	return &runner.Step{
		Name:     "Collect firewall status",
		Optional: true,
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "os")

			cmds := []struct {
				cmd  string
				dest string
				sudo bool
			}{
				// firewall-cmd --list-all 需要 root 或 firewalld admin 权限
				{"firewall-cmd --list-all 2>/dev/null || true", filepath.Join(dir, "firewalld.txt"), true},
				{"firewall-cmd --list-all-zones 2>/dev/null || true", filepath.Join(dir, "firewalld-zones.txt"), true},
				// iptables -L 需要 root
				{"iptables -L -n -v 2>/dev/null || true", filepath.Join(dir, "iptables.txt"), true},
				{"ip6tables -L -n -v 2>/dev/null || true", filepath.Join(dir, "ip6tables.txt"), true},
				// systemctl 状态查询和 sestatus 无需 root
				{"systemctl is-active firewalld 2>/dev/null || true", filepath.Join(dir, "firewalld-status.txt"), false},
				{"systemctl is-active iptables 2>/dev/null || true", filepath.Join(dir, "iptables-status.txt"), false},
				{"sestatus 2>/dev/null || true", filepath.Join(dir, "selinux.txt"), false},
			}

			collectLogPhase(ctx, "plan", fmt.Sprintf("cmds=%d dir=os (firewalld+iptables)", len(cmds)))

			for _, c := range cmds {
				if err := runAndSave(ctx, c.cmd, c.dest, c.sudo); err != nil {
					appendWarning(ctx, err.Error())
				}
			}

			ctx.Logger.Info("[R-017] firewall status collected to %s", dir)
			return nil
		},
	}
}
