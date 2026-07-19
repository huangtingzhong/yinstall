package ycm

import "github.com/yinstall/internal/runner"

// GetAllSteps 返回 yinstall ycm 安装 steps（ID 由 BuildSteps 自动赋 G-001..G-NNN）。
func GetAllSteps() []*runner.Step {
	return runner.BuildSteps(runner.StepSpec{
		Prefix: "G",
		Entries: []runner.StepEntry{
			{New: stepCheckPreinstall},
			{New: stepInstallDeps},
			{New: stepExtractPackage},
			{New: stepSetOwnership},
			{New: stepCheckDeployConfig},
			{New: stepConfigurePorts},
			{New: stepDeploy},
			{New: stepVerifyProcess},
			{New: stepVerifyPorts},
			{New: stepVerifyWeb},
			{New: stepConfigAutostartService},
		},
	})
}

// StepIDByName returns the registry ID for a step Name.
func StepIDByName(name string) string {
	return runner.StepIDByName(GetAllSteps(), name)
}
