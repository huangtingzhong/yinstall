package win_os

import (
	"fmt"
	"strings"

	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

func StepW010AntivirusExclusions() *runner.Step {
	return &runner.Step{
		ID:          "W-010",
		Name:        "Antivirus Exclusions",
		Description: "Add Defender exclusions for SQL paths",
		Tags:        []string{"win-os", "win-os-install", "av"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("os_av_exclusion_enable", false) && !ctx.IsForceStep() {
				return runner.NewStepSkippedError("os_av_exclusion_enable=false")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			base := commonwin.DataMountPath(ctx)
			paths := []string{base, `C:\Program Files\Microsoft SQL Server`}
			for _, p := range paths {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				script := fmt.Sprintf(`Add-MpPreference -ExclusionPath '%s' -ErrorAction SilentlyContinue`, strings.ReplaceAll(p, "'", "''"))
				ctx.LogScriptPreview("powershell", "W-010 AV exclusion", script)
				_, _ = ctx.Execute(`powershell -NoProfile -Command "`+script+`"`, false)
			}
			return nil
		},
	}
}
