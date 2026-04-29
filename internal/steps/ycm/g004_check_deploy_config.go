// g004_check_deploy_config.go - 校验 deploy 配置文件
// G-004: 检查 deploy.yml 是否存在且可读

package ycm

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepG004CheckDeployConfig 校验 deploy 配置文件
func StepG004CheckDeployConfig() *runner.Step {
	return &runner.Step{
		ID:          "G-004",
		Name:        "Check Deploy Config",
		Description: "Verify YCM deploy.yml configuration file exists and is readable",
		Tags:        []string{"ycm", "config"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")
			if deployFile == "" {
				return fmt.Errorf("ycm_deploy_file parameter is required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")

			ctx.Logger.Info("Checking deploy config: %s", deployFile)

			// 检查文件存在性
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", deployFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("deploy config file not found: %s", deployFile)
			}

			// 检查文件可读
			result, _ = ctx.Execute(fmt.Sprintf("test -r %s", deployFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("deploy config file is not readable: %s", deployFile)
			}

			// 输出文件内容摘要（前几行）
			result, _ = ctx.Execute(fmt.Sprintf("head -20 %s", deployFile), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("Deploy config contents (first 20 lines):\n%s", result.GetStdout())
			}

			ctx.Logger.Info("OK: Deploy config file exists and is readable: %s", deployFile)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			deployFile := ctx.GetParamString("ycm_deploy_file", "/opt/ycm/etc/deploy.yml")
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s && test -r %s", deployFile, deployFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("deploy config file not accessible: %s", deployFile)
			}
			return nil
		},
	}
}
