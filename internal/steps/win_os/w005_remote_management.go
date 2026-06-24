package win_os

import (
	"github.com/yinstall/internal/runner"
)

func StepW005RemoteManagement() *runner.Step {
	return &runner.Step{
		ID:          "W-005",
		Name:        "Remote Management",
		Description: "Check or enable OpenSSH / WinRM",
		Tags:        []string{"win-os", "win-os-both", "remote"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("os_remote_mgmt_enable", false) && !ctx.IsForceStep() {
				return runner.NewStepSkippedError("os_remote_mgmt_enable=false")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-005 remote mgmt")
			_, _ = ctx.Execute(`powershell -NoProfile -Command "Get-Service sshd -ErrorAction SilentlyContinue | Select-Object Status"`, false)
			_, _ = ctx.Execute(`powershell -NoProfile -Command "Test-WSMan -ErrorAction SilentlyContinue"`, false)
			if ctx.IsForceStep() {
				script := `Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0 -ErrorAction SilentlyContinue; Enable-PSRemoting -Force -SkipNetworkProfileCheck`
				ctx.LogScriptPreview("powershell", "W-005 enable remote", script)
				_, _ = ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false)
			}
			return nil
		},
	}
}
