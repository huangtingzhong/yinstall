package mysql

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepM011InitializeDatadir runs mysqld --initialize-insecure.
func StepM011InitializeDatadir() *runner.Step {
	return &runner.Step{
		ID:          "M-011",
		Name:        "Initialize Datadir",
		Description: "Initialize empty mysql datadir",
		Tags:        []string{"mysql", "initialize", "mysql-instance"},
		Action: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return err
			}
			cfgPath := layout.Other + "/my.cnf"
			if ctx.GetTargetPlatform() == PlatformWindows {
				cfgPath = layout.Other + "/my.ini"
			}
			mysqlLogPhase(ctx, "plan", "M-011 initialize")
			user := ctx.GetParamString("os_user", "mysql")
			var cmd string
			switch ctx.GetTargetPlatform() {
			case PlatformWindows:
				cmd = fmt.Sprintf(`"%s/bin/mysqld.exe" --defaults-file="%s" --initialize-insecure --console`,
					filepathToSlash(layout.Home), filepathToSlash(cfgPath))
			case PlatformDarwin:
				cmd = fmt.Sprintf("%s --defaults-file=%s --basedir=%s --datadir=%s --initialize-insecure",
					quotedBin(layout.Home, "mysqld"), commonos.ShellSingleQuote(cfgPath),
					commonos.ShellSingleQuote(layout.Home), commonos.ShellSingleQuote(layout.Data))
			default:
				cmd = fmt.Sprintf("%s --defaults-file=%s --user=%s --basedir=%s --datadir=%s --initialize-insecure",
					quotedBin(layout.Home, "mysqld"), commonos.ShellSingleQuote(cfgPath), user,
					commonos.ShellSingleQuote(layout.Home), commonos.ShellSingleQuote(layout.Data))
			}
			if ctx.GetTargetPlatform() == PlatformLinux {
				// Run as root with --user=mysql; product user is nologin and cannot su/sudo -iu.
				_, err = ctx.ExecuteWithCheck(cmd, UseSudo(ctx))
			} else {
				_, err = ctx.ExecuteWithCheck(cmd, false)
			}
			return err
		},
	}
}
