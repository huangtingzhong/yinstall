package mysql

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepPrepareLogFiles creates error.log with correct ownership.
func stepPrepareLogFiles() *runner.Step {
	return &runner.Step{
		Name:        "Prepare Log Files",
		Description: "Create error.log under MYSQL_OTHER",
		Tags:        []string{"mysql", "logs", "mysql-instance"},
		Action: func(ctx *runner.StepContext) error {
			layout, _ := layoutFromCtx(ctx)
			logPath := layout.Other + "/error.log"
			user := ctx.GetParamString("os_user", "mysql")
			if ctx.GetTargetPlatform() == PlatformWindows {
				cmd := fmt.Sprintf(`powershell -NoProfile -Command "New-Item -ItemType File -Force -Path '%s'"`, filepathToSlash(logPath))
				_, err := ctx.ExecuteWithCheck(cmd, false)
				return err
			}
			if ctx.GetTargetPlatform() == PlatformDarwin || ctx.GetParamBool("local_mode", false) {
				cmd := fmt.Sprintf("touch %s", commonos.ShellSingleQuote(logPath))
				_, err := ctx.ExecuteWithCheck(cmd, UseSudo(ctx))
				return err
			}
			cmd := fmt.Sprintf("touch %s && chown %s:%s %s",
				commonos.ShellSingleQuote(logPath), user, user, commonos.ShellSingleQuote(logPath))
			_, err := ctx.ExecuteWithCheck(cmd, UseSudo(ctx))
			return err
		},
	}
}
