// h004_install_deps.go - 安装 YMP 依赖包
// H-005: 安装 libaio, lsof 等运行时依赖

package ymp

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepH005InstallDeps 安装 YMP 依赖包
func StepH005InstallDeps() *runner.Step {
	return &runner.Step{
		ID:          "H-005",
		Name:        "Install YMP Dependencies",
		Description: "Install YMP runtime dependencies (libaio, lsof)",
		Tags:        []string{"ymp", "deps"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			if pkgManager == "" {
				return fmt.Errorf("no supported package manager found")
			}

			packages := ctx.GetParamString("ymp_deps_packages", "libaio lsof")
			pkgList := strings.Fields(packages)
			allInstalled := true
			for _, pkg := range pkgList {
				if !commonos.IsPackageInstalled(ctx, pkg, pkgManager) {
					allInstalled = false
					break
				}
			}
			if allInstalled {
				return fmt.Errorf("all YMP dependencies already installed")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			// local-iso 模式：确保 ISO 已挂载、repo 文件就绪
			if err := commonos.EnsureLocalISORepo(ctx); err != nil {
				return fmt.Errorf("failed to prepare local ISO repo: %w", err)
			}

			packages := ctx.GetParamString("ymp_deps_packages", "libaio lsof")
			yumMode := ctx.GetParamString("os_yum_mode", "none")
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			isRHEL8 := commonos.IsRHEL8(ctx.OSInfo)

			packagesToInstall := commonos.FilterUninstalledPackages(ctx, packages, pkgManager)
			if len(packagesToInstall) == 0 {
				ctx.Logger.Info("All YMP dependencies already installed")
				return nil
			}

			ctx.Logger.Info("Installing YMP dependencies: %s", strings.Join(packagesToInstall, " "))
			cmd := commonos.BuildInstallCmd(pkgManager, yumMode, strings.Join(packagesToInstall, " "), isRHEL8)

			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to install YMP dependencies: %w", err)
			}

			ctx.Logger.Info("YMP dependencies installed successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			packages := ctx.GetParamString("ymp_deps_packages", "libaio lsof")
			for _, pkg := range strings.Fields(packages) {
				if commonos.IsPackageInstalled(ctx, pkg, pkgManager) {
					ctx.Logger.Info("✓ Package verified: %s", pkg)
				} else {
					ctx.Logger.Warn("Package may not be installed: %s", pkg)
				}
			}
			return nil
		},
	}
}
