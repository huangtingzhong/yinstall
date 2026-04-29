// h005_validate_jdk.go - 校验 JDK 版本与架构约束
// H-007: ARM 架构仅支持 JDK 11/17

package ymp

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepH007ValidateJDK 校验 JDK 版本与 CPU 架构约束
func StepH007ValidateJDK() *runner.Step {
	return &runner.Step{
		ID:          "H-007",
		Name:        "Validate JDK Version",
		Description: "Validate JDK version and architecture constraints (ARM only supports JDK 11/17)",
		Tags:        []string{"ymp", "jdk"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			// 检查是否存在 java 命令
			result, _ := ctx.Execute("which java 2>/dev/null", false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("java not found on target host; " +
					"either pre-install JDK manually, or add --ymp-jdk-enable=true --ymp-jdk-package=<jdk.rpm> to let H-006 install it automatically")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			expectedVersion := ctx.GetParamString("ymp_jdk_version", "17")

			// 检测 CPU 架构
			result, _ := ctx.Execute("uname -m", false)
			arch := ""
			if result != nil {
				arch = strings.TrimSpace(result.GetStdout())
			}
			ctx.Logger.Info("CPU architecture: %s", arch)

			isARM := strings.Contains(strings.ToLower(arch), "aarch64") || strings.Contains(strings.ToLower(arch), "arm")

			// ARM 架构仅支持 JDK 11 和 17
			if isARM {
				if expectedVersion != "11" && expectedVersion != "17" {
					return fmt.Errorf("ARM architecture only supports JDK 11 and 17, requested: %s", expectedVersion)
				}
				ctx.Logger.Info("ARM architecture detected, JDK %s is supported", expectedVersion)
			}

			// 检查当前 JDK 版本
			result, _ = ctx.Execute("java -version 2>&1 | head -1", false)
			if result != nil {
				versionOutput := strings.TrimSpace(result.GetStdout())
				if versionOutput == "" {
					versionOutput = strings.TrimSpace(result.GetStderr())
				}
				ctx.Logger.Info("Current JDK: %s", versionOutput)

				// 检查版本是否匹配
				if !strings.Contains(versionOutput, fmt.Sprintf("\"%s.", expectedVersion)) &&
					!strings.Contains(versionOutput, fmt.Sprintf("version \"%s", expectedVersion)) {
					ctx.Logger.Warn("JDK version may not match expected version %s", expectedVersion)
				}
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("java -version 2>&1 | head -1", false)
			if result == nil || (result.GetExitCode() != 0 && strings.TrimSpace(result.GetStdout()) == "" && strings.TrimSpace(result.GetStderr()) == "") {
				return fmt.Errorf("java is not available")
			}
			versionOutput := strings.TrimSpace(result.GetStdout())
			if versionOutput == "" {
				versionOutput = strings.TrimSpace(result.GetStderr())
			}
			ctx.Logger.Info("OK: JDK validated: %s", versionOutput)
			return nil
		},
	}
}
