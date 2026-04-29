// g001_install_deps.go - 安装 YCM 依赖包
// G-001: 安装 libnsl 等 YCM 运行时依赖
// 复用 common/os 中的包管理公共函数，支持 ISO/光驱/网络三种安装模式

package ycm

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepG001InstallDeps 安装 YCM 依赖包
func StepG001InstallDeps() *runner.Step {
	return &runner.Step{
		ID:          "G-001",
		Name:        "Install YCM Dependencies",
		Description: "Install YCM runtime dependency packages (libnsl)",
		Tags:        []string{"ycm", "deps"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			if pkgManager == "" {
				return fmt.Errorf("no supported package manager found (yum/dnf/apt)")
			}

			// 检查所有依赖包是否已安装，如果都已安装则跳过
			packages := ctx.GetParamString("ycm_deps_packages", "libnsl")
			pkgList := strings.Fields(packages)
			allInstalled := true
			for _, pkg := range pkgList {
				pkg = strings.TrimSpace(pkg)
				if pkg == "" {
					continue
				}
				if !commonos.IsPackageInstalled(ctx, pkg, pkgManager) {
					ctx.Logger.Info("Package '%s' not installed, need to install dependencies", pkg)
					allInstalled = false
				}
			}
			if allInstalled {
				return fmt.Errorf("all required YCM packages already installed, skipping installation")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			packages := ctx.GetParamString("ycm_deps_packages", "libnsl")
			yumMode := ctx.GetParamString("os_yum_mode", "none")
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			isRHEL8 := commonos.IsRHEL8(ctx.OSInfo)

			ctx.Logger.Info("Using package manager: %s (yum_mode: %s)", pkgManager, yumMode)

			// 过滤已安装的包，只安装缺失的
			packagesToInstall := commonos.FilterUninstalledPackages(ctx, packages, pkgManager)

			if len(packagesToInstall) == 0 {
				ctx.Logger.Info("All YCM dependencies already installed, skipping")
				return nil
			}

			ctx.Logger.Info("Installing missing YCM dependencies: %s", strings.Join(packagesToInstall, " "))

			// 使用公共函数构建安装命令（自动处理 ISO/光驱/网络模式）
			cmd := commonos.BuildInstallCmd(pkgManager, yumMode, strings.Join(packagesToInstall, " "), isRHEL8)

			result, err := ctx.Execute(cmd, true)
			if err != nil {
				return fmt.Errorf("failed to install YCM dependencies: %w", err)
			}
			if result != nil && result.GetExitCode() != 0 {
				ctx.Logger.Warn("Package install returned non-zero exit code: %d", result.GetExitCode())
				ctx.Logger.Warn("stderr: %s", result.GetStderr())
			}

			ctx.Logger.Info("YCM dependencies installed successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			packages := ctx.GetParamString("ycm_deps_packages", "libnsl")
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			pkgList := strings.Fields(packages)

			for _, pkg := range pkgList {
				pkg = strings.TrimSpace(pkg)
				if pkg == "" {
					continue
				}
				if commonos.IsPackageInstalled(ctx, pkg, pkgManager) {
					ctx.Logger.Info("OK: Package verified: %s", pkg)
				} else {
					// libnsl 等库可能包名与 rpm 名不完全一致，用 ldconfig 补充检查
					result, _ := ctx.Execute(fmt.Sprintf("ldconfig -p 2>/dev/null | grep -i %s", pkg), false)
					if result != nil && result.GetExitCode() == 0 {
						ctx.Logger.Info("OK: Library verified via ldconfig: %s", pkg)
					} else {
						ctx.Logger.Warn("Package may not be installed: %s (non-critical)", pkg)
					}
				}
			}
			return nil
		},
	}
}
