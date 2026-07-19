package os

import (
	"fmt"
	"net"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepConfigureChrony 配置 chrony 时间同步（可选）
func stepConfigureChrony() *runner.Step {
	return &runner.Step{
		Name:        "Configure Chrony",
		Description: "Configure NTP time synchronization",
		Tags:        []string{"os", "time"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			ntpServer := strings.TrimSpace(ctx.GetParamString("os_ntp_server", ""))
			if ntpServer == "" {
				return fmt.Errorf("os_ntp_server not set")
			}

			// 确认 chrony 已安装
			result, _ := ctx.Execute("which chronyd 2>/dev/null || rpm -q chrony", false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("chronyd not installed")
			}

			// 校验 server：合法 IP 或可解析域名
			if net.ParseIP(ntpServer) == nil {
				// 域名必须可解析
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("getent hosts '%s' >/dev/null 2>&1", ntpServer), false); err != nil {
					return fmt.Errorf("ntp server domain not resolvable: %s", ntpServer)
				}
			}

			// 校验 NTP 端口可达性（UDP/123）
			// 优先用 bash 的 /dev/udp（常见可用）；非零退出视为不可达。
			portCheck := fmt.Sprintf("timeout 3 bash -lc \"echo > /dev/udp/%s/123\" >/dev/null 2>&1", ntpServer)
			if _, err := ctx.ExecuteWithCheck(portCheck, false); err != nil {
				return fmt.Errorf("ntp server udp/123 not reachable: %s", ntpServer)
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-016: Configure Chrony")
			ntpServer := strings.TrimSpace(ctx.GetParamString("os_ntp_server", ""))

			config := fmt.Sprintf(`# NTP server
server %s iburst
allow 0.0.0.0/0
makestep 1.0 3
driftfile /var/lib/chrony/drift
rtcsync
logdir /var/log/chrony
`, ntpServer)

			if !ctx.IsForceStep() && chronyAlreadyConfigured(ctx, ntpServer) {
				ctx.Logger.Info("Chrony already configured for %s and active, skipping (use -f %s to force)", ntpServer, ctx.CurrentStepID)
				osLogPhase(ctx, "skip", "already_configured=chrony")
				return nil
			}

			// 备份原配置
			ctx.Execute("cp /etc/chrony.conf /etc/chrony.conf.bak_$(date +%F) 2>/dev/null", true)

			cmd := fmt.Sprintf("cat > /etc/chrony.conf << 'EOF'\n%sEOF", config)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return err
			}

			ctx.Execute("systemctl restart chronyd", true)
			ctx.Execute("systemctl enable chronyd", true)

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("chronyc tracking 2>/dev/null | head -5", false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("chrony tracking failed")
			}
			return nil
		},
	}
}

func chronyAlreadyConfigured(ctx *runner.StepContext, ntpServer string) bool {
	conf, _ := ctx.Execute("cat /etc/chrony.conf 2>/dev/null || true", false)
	if conf == nil || !strings.Contains(conf.GetStdout(), "server "+ntpServer) {
		return false
	}
	active, _ := ctx.Execute("systemctl is-active chronyd 2>/dev/null", false)
	return active != nil && strings.TrimSpace(active.GetStdout()) == "active"
}
