// registry.go - YMP 安装步骤注册表

package ymp

import "github.com/yinstall/internal/runner"

// GetAllSteps 返回全部 YMP 安装 steps（ID 由 BuildSteps 自动赋 H-001..H-NNN）。
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "H",
		Entries: []runner.StepEntry{
			{New: stepCheckPort},
			{New: stepCheckInstallDir},
			{New: stepCreateUser},
			{New: stepWriteLimits},
			{New: stepInstallDeps},
			{New: stepInstallJdk},
			{New: stepValidateJdk},
			{New: stepExtractYmp},
			{New: stepExtractInstantclient},
			{New: stepSetupSqlplus},
			{New: stepInstallYmp},
			{New: stepVerifyProcess},
			{New: stepVerifyPort},
			{New: stepShowPorts},
		},
	})
}

// StepIDByName returns the registry ID for a step Name.
func StepIDByName(name string) string {
	return runner.StepIDByName(GetAllSteps(), name)
}
