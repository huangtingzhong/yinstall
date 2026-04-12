package os

import (
	"fmt"

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
			result, _ := ctx.Execute("which chronyd 2>/dev/null || rpm -q chrony", false)
			if result.GetExitCode() != 0 {
				return fmt.Errorf("chronyd not installed")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ntpServer := ctx.GetParamString("os_ntp_server", "ntp.aliyun.com")

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
