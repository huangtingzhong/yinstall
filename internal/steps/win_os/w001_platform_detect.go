package win_os

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

func StepW001WindowsPlatformDetect() *runner.Step {
	return &runner.Step{
		ID:          "W-001",
		Name:        "Windows Platform Detect",
		Description: "Detect Windows Server edition, domain, FQDN, memory, CPU",
		Tags:        []string{"win-os", "win-os-both", "platform"},
		PreCheck: func(ctx *runner.StepContext) error {
			if p := ctx.GetTargetPlatform(); p != "" && p != "windows" {
				return fmt.Errorf("W-001 requires Windows target, got %s", p)
			}
			if !commonos.IsWindowsTarget(ctx) {
				return fmt.Errorf("target is not Windows")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-001 platform detect")
			ctx.OSInfo = commonos.DetectWindowsOSInfo(ctx)
			joined, domainName := commonos.WindowsDomainInfo(ctx)
			ctx.SetResult("target_platform", "windows")
			ctx.SetResult("domain_joined", joined)
			if joined {
				ctx.SetResult("domain_name", domainName)
			} else {
				ctx.SetResult("workgroup_name", domainName)
			}
			ctx.SetResult("computer_name", commonos.WindowsComputerName(ctx))
			ctx.SetResult("fqdn", commonos.WindowsFQDN(ctx))
			ctx.SetResult("total_memory", commonos.WindowsMemoryGB(ctx))
			ctx.SetResult("cpu_cores", commonos.WindowsLogicalCPUs(ctx))
			return nil
		},
	}
}
