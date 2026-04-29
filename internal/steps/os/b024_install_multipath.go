package os

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB024InstallMultipath 安装 multipath 相关软件（device-mapper-multipath）
// 注意：在 YAC 模式下，多路径软件已在 B-015 安装，此步骤会跳过
// 此步骤仅用于非 YAC 模式但显式启用多路径的场景
func StepB024InstallMultipath() *runner.Step {
	return &runner.Step{
		ID:          "B-024",
		Name:        "Install Multipath",
		Description: "Install device-mapper-multipath (skipped in YAC mode, already installed in B-015)",
		Tags:        []string{"os", "yac", "multipath"},
		Optional:    true, // 在 YAC 模式下会跳过

		PreCheck: func(ctx *runner.StepContext) error {
			isYACMode := ctx.GetParamBool("yac_mode", false)

			// YAC 模式下，多路径软件已在 B-015 安装
			if isYACMode {
				return fmt.Errorf("multipath already installed in B-015 (YAC mode)")
			}

			// 非 YAC 模式，检查是否显式启用或由 B-022 自动启用
			enabled := ctx.GetParamBool("yac_multipath_enable", false)
			needMultipath := ctx.GetParamBool("yac_need_multipath", false)

			if !enabled && !needMultipath {
				return fmt.Errorf("multipath not enabled")
			}

			// 检查多路径软件是否已安装
			result, _ := ctx.Execute("which multipath 2>/dev/null || rpm -q device-mapper-multipath 2>/dev/null || dpkg -l multipath-tools 2>/dev/null", false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("Multipath software already installed, skipping")
				return fmt.Errorf("multipath software already installed")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			// 检查是否已安装
			result, _ := ctx.Execute("which multipath 2>/dev/null || rpm -q device-mapper-multipath 2>/dev/null", false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("Multipath software already installed")
				return nil
			}

			// 安装多路径软件
			multipathPkg := getMultipathPackage(ctx.OSInfo)
			yumMode := ctx.GetParamString("os_yum_mode", "none")
			pkgManager := "yum"
			if ctx.OSInfo != nil && ctx.OSInfo.PkgManager != "" {
				pkgManager = ctx.OSInfo.PkgManager
			}

			ctx.Logger.Info("Installing multipath software: %s", multipathPkg)
			isRHEL8 := ctx.OSInfo != nil && ctx.OSInfo.IsRHEL8
			cmd := commonos.BuildInstallCmd(pkgManager, yumMode, multipathPkg, isRHEL8)
			_, err := ctx.ExecuteWithCheck(cmd, true)
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("which multipath 2>/dev/null || rpm -q device-mapper-multipath 2>/dev/null", false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("multipath command not found")
			}
			return nil
		},
	}
}
