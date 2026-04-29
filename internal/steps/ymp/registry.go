// registry.go - YMP 安装步骤注册表

package ymp

import "github.com/yinstall/internal/runner"

// GetAllSteps 返回全部 YMP 安装 steps（顺序即默认执行顺序）。
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		// Pre-installation checks
		StepH001CheckPort(),
		StepH002CheckInstallDir(),

		// Environment preparation
		StepH003CreateUser(),
		StepH004WriteLimits(),
		StepH005InstallDeps(),

		// JDK：先安装（可选），再校验（必须）
		StepH006InstallJDK(),
		StepH007ValidateJDK(),

		// Software extraction
		StepH008ExtractYMP(),
		StepH009ExtractInstantclient(),
		StepH010SetupSQLPlus(),

		// Installation
		StepH011InstallYMP(),

		// Verification
		StepH012VerifyProcess(),
		StepH013VerifyPort(),
		StepH014ShowPorts(),
	}
}
