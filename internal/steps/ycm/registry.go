// registry.go - YCM 安装步骤注册表
// 返回所有 YCM 安装步骤，按执行顺序排列

package ycm

import "github.com/yinstall/internal/runner"

// GetAllSteps 返回全部 YCM 安装 steps（顺序即默认执行顺序）。
// 步骤编号为 G-001 … G-010，与文件名 g001_*.go … g010_*.go 一一对应。
func GetAllSteps() []*runner.Step {
	return []*runner.Step{
		// Dependencies
		StepG001InstallDeps(),

		// Package extraction and setup
		StepG002ExtractPackage(),
		StepG003SetOwnership(),

		// Configuration
		StepG004CheckDeployConfig(),
		StepG005ConfigurePorts(),

		// Port validation
		StepG006CheckPorts(),

		// Deployment
		StepG007Deploy(),

		// Verification
		StepG008VerifyProcess(),
		StepG009VerifyPorts(),
		StepG010VerifyWeb(),
	}
}
