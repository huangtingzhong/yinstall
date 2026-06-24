package mysql

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepM013ConfigureAutostart configures systemd or Windows service.
func StepM013ConfigureAutostart() *runner.Step {
	return &runner.Step{
		ID:          "M-013",
		Name:        "Configure Autostart",
		Description: "Install systemd unit or Windows service",
		Tags:        []string{"mysql", "service", "mysql-instance"},
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetTargetPlatform() == PlatformDarwin {
				return fmt.Errorf("systemd not available on darwin")
			}
			if ctx.GetParamBool("mysql_skip_systemd", false) && ctx.GetTargetPlatform() == PlatformLinux {
				return fmt.Errorf("mysql_skip_systemd enabled")
			}
			if ctx.GetTargetPlatform() == PlatformLinux && !commonos.CheckSystemdAvailable(ctx) {
				return fmt.Errorf("systemd not available")
			}
			return nil
		},
		Optional: true,
		Action: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return err
			}
			port := layout.Port
			cfgPath := layout.Other + "/my.cnf"
			if ctx.GetTargetPlatform() == PlatformWindows {
				cfgPath = layout.Other + "/my.ini"
				svc := fmt.Sprintf("MySQL%d", port)
				cmd := fmt.Sprintf(`"%s/bin/mysqld.exe" --install %s --defaults-file="%s"`,
					filepathToSlash(layout.Home), svc, filepathToSlash(cfgPath))
				_, err = ctx.ExecuteWithCheck(cmd, false)
				return err
			}
			if ctx.GetParamBool("mysql_skip_systemd", false) {
				return nil
			}
			osUser := ctx.GetParamString("os_user", "mysql")
			osGroup := ctx.GetParamString("os_group", "mysql")
			mkdirCmd := fmt.Sprintf("mkdir -p /var/lib/mysql && chown %s:%s /var/lib/mysql",
				commonos.ShellSingleQuote(osUser), commonos.ShellSingleQuote(osGroup))
			if _, err := ctx.ExecuteWithCheck(mkdirCmd, true); err != nil {
				return err
			}
			unit := fmt.Sprintf("mysqld%d.service", port)
			unitPath := "/etc/systemd/system/" + unit
			pidFile := layout.Other + "/mysqld.pid"
			content := fmt.Sprintf(`[Unit]
Description=MySQL Server %d
After=network.target

[Service]
Type=forking
User=%s
Group=%s
WorkingDirectory=%s
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
Environment=HOME=/var/lib/mysql
LimitNOFILE=8192
PIDFile=%s
ExecStart=/bin/sh %s/bin/mysqld_safe --defaults-file=%s
KillMode=mixed
TimeoutStopSec=120

[Install]
WantedBy=multi-user.target
`, port, osUser, osGroup, layout.Home, pidFile, layout.Home, cfgPath)
			ctx.LogScriptPreview("file", "systemd-unit", content)
			cmd := fmt.Sprintf("cat > %s << 'EOF'\n%sEOF", commonos.ShellSingleQuote(unitPath), content)
			if _, err = ctx.ExecuteWithCheck(cmd, true); err != nil {
				return err
			}
			_, err = ctx.ExecuteWithCheck("systemctl daemon-reload && systemctl enable "+unit, true)
			ctx.SetResult("mysql_systemd_unit", unit)
			return err
		},
	}
}
