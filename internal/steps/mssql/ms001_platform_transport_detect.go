package mssql

import (
	"fmt"
	"runtime"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

func detectPlatform(ctx *runner.StepContext) string {
	if p := ctx.GetTargetPlatform(); p != "" {
		return p
	}
	if commonos.IsWindowsTarget(ctx) {
		return commonmssql.PlatformWindows
	}
	return "linux"
}

func StepMS001PlatformTransportDetect() *runner.Step {
	return &runner.Step{
		ID:   "MS-001",
		Name: "Platform & Transport Detect",
		Tags: []string{"mssql", "mssql-both", "platform"},
		PreCheck: func(ctx *runner.StepContext) error {
			local := ctx.GetParamBool("local_mode", false)
			if local && runtime.GOOS != "windows" {
				return fmt.Errorf("local mssql install requires Windows control host; use -t for remote")
			}
			platform := detectPlatform(ctx)
			if platform != commonmssql.PlatformWindows {
				return fmt.Errorf("mssql requires Windows target, got %s", platform)
			}
			transport := ctx.GetParamString("windows_transport", "auto")
			ctx.SetResult("target_platform", platform)
			ctx.SetResult("windows_transport", transport)
			if transport == "auto" || transport == "winrm" {
				res, _ := ctx.Execute(`powershell -NoProfile -Command "Test-WSMan -ErrorAction SilentlyContinue; if ($?) { 'ok' }"`, false)
				if res == nil || !strings.Contains(res.GetStdout(), "ok") {
					ctx.Logger.Warn("WinRM probe did not return ok; ensure WinRM is enabled")
				}
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mssqlLogPhase(ctx, "plan", "MS-001 platform=windows")
			return nil
		},
	}
}
