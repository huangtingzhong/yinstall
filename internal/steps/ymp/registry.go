// registry.go - YMP 安装步骤注册表

package ymp

import "github.com/yinstall/internal/runner"

// GetAllSteps returns all YMP installation steps in execution order
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		// Pre-installation checks
		StepH000CheckPort(),
		StepH001CheckInstallDir(),

		// Environment preparation
		StepH002CreateUser(),
		StepH003WriteLimits(),
		StepH004InstallDeps(),

		// JDK：先安装（可选），再校验（必须）
		StepH006InstallJDK(),
		StepH005ValidateJDK(),

		// Software extraction
		StepH007ExtractYMP(),
		StepH008ExtractInstantclient(),
		StepH009SetupSQLPlus(),

		// Installation
		StepH010InstallYMP(),

		// Verification
		StepH011VerifyProcess(),
		StepH012VerifyPort(),
		StepH013ShowPorts(),
	}
}
