// r016_network_routes.go - 网络路由、hosts 与端口采集
// 采集 IP 路由表、/etc/hosts、DNS 配置及监听端口列表，写入 os/network/ 目录。
package collect

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// stepNetworkRoutes 返回 R-016 步骤：采集路由、hosts、端口信息。
func stepNetworkRoutes() *runner.Step {
	return &runner.Step{
		Name: "Collect network routes and DNS",
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "os", "network")

			cmds := []struct {
				cmd  string
				dest string
			}{
				{"ip route", filepath.Join(dir, "routes.txt")},
				{"ip -6 route 2>/dev/null || true", filepath.Join(dir, "routes-ipv6.txt")},
				{"cat /etc/hosts", filepath.Join(dir, "hosts.txt")},
				{"cat /etc/resolv.conf 2>/dev/null || true", filepath.Join(dir, "resolv.conf.txt")},
				{"cat /etc/nsswitch.conf 2>/dev/null || true", filepath.Join(dir, "nsswitch.conf.txt")},
				{"ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null || true", filepath.Join(dir, "ports.txt")},
				{"ss -s 2>/dev/null || true", filepath.Join(dir, "ss-summary.txt")},
			}

			collectLogPhase(ctx, "plan", fmt.Sprintf("cmds=%d dir=os/network", len(cmds)))

			for _, c := range cmds {
				if err := runAndSave(ctx, c.cmd, c.dest); err != nil {
					appendWarning(ctx, err.Error())
				}
			}

			ctx.Logger.Info("[R-016] network routes and DNS collected to %s", dir)
			return nil
		},
	}
}
