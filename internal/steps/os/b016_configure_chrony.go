package os

import (
	"fmt"
	"net"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepB016ConfigureChrony Configure chrony (optional)
func StepB016ConfigureChrony() *runner.Step {
	return &runner.Step{
		ID:          "B-016",
		Name:        "Configure Chrony",
		Description: "Configure NTP time synchronization",
		Tags:        []string{"os", "time"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			ntpServer := strings.TrimSpace(ctx.GetParamString("os_ntp_server", ""))
			if ntpServer == "" {
				return fmt.Errorf("os_ntp_server not set")
			}

			// Ensure chrony is installed
			result, _ := ctx.Execute("which chronyd 2>/dev/null || rpm -q chrony", false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("chronyd not installed")
			}

			// Validate server value: IP format or resolvable domain
			if net.ParseIP(ntpServer) == nil {
				// Domain name: must be resolvable
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("getent hosts '%s' >/dev/null 2>&1", ntpServer), false); err != nil {
					return fmt.Errorf("ntp server domain not resolvable: %s", ntpServer)
				}
			}

			// Validate NTP port reachability (UDP/123)
			// Prefer bash /dev/udp which is commonly available; treat non-zero as unreachable.
			portCheck := fmt.Sprintf("timeout 3 bash -lc \"echo > /dev/udp/%s/123\" >/dev/null 2>&1", ntpServer)
			if _, err := ctx.ExecuteWithCheck(portCheck, false); err != nil {
				return fmt.Errorf("ntp server udp/123 not reachable: %s", ntpServer)
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ntpServer := strings.TrimSpace(ctx.GetParamString("os_ntp_server", ""))

			// 备份原配置
			ctx.Execute("cp /etc/chrony.conf /etc/chrony.conf.bak_$(date +%F) 2>/dev/null", true)

			config := fmt.Sprintf(`# NTP server
server %s iburst
allow 0.0.0.0/0
makestep 1.0 3
driftfile /var/lib/chrony/drift
rtcsync
logdir /var/log/chrony
`, ntpServer)

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
