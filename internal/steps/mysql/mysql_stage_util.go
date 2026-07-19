package mysql

import (
	"fmt"
	"strings"

	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

func installStage(ctx *runner.StepContext) string {
	stage, err := commonmysql.ParseStage(ctx.GetParamString("mysql_stage", commonmysql.DefaultInstallStage()))
	if err != nil {
		return commonmysql.StageAll
	}
	return stage
}

func softwareMysqldExists(ctx *runner.StepContext, layout Layout) bool {
	if strings.TrimSpace(layout.Home) == "" {
		return false
	}
	if ctx.GetTargetPlatform() == PlatformWindows {
		home := filepathToSlash(layout.Home)
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "Test-Path -LiteralPath '%s/bin/mysqld.exe'"`, home)
		res, _ := ctx.Execute(cmd, false)
		return powershellStdoutTrue(res)
	}
	cmd := fmt.Sprintf("test -x %s/bin/mysqld", shellQuote(layout.Home))
	res, _ := ctx.Execute(cmd, false)
	return res != nil && res.GetExitCode() == 0
}

// powershellStdoutTrue reports Test-Path / boolean PowerShell output (exit 0 alone is not enough).
func powershellStdoutTrue(res runner.ExecResult) bool {
	if res == nil || res.GetExitCode() != 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(res.GetStdout()), "True")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func dirsForInstallStage(stage string, layout Layout) []string {
	switch stage {
	case commonmysql.StageSoftware:
		return []string{layout.Base, layout.Home}
	case commonmysql.StageInstance:
		return []string{layout.Data, layout.Other}
	default:
		return []string{layout.Base, layout.Home, layout.Data, layout.Other}
	}
}

func requireSoftwareForInstanceStage(ctx *runner.StepContext, layout Layout) error {
	if installStage(ctx) != commonmysql.StageInstance {
		return nil
	}
	if softwareMysqldExists(ctx, layout) {
		return nil
	}
	if layout.Home != "" {
		return fmt.Errorf("mysql software not found at %s (run --stage software or all first)", layout.Home)
	}
	return fmt.Errorf("mysql software home unknown; pass --mysql-version or --mysql-package")
}
