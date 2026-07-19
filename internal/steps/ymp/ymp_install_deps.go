// h004_install_deps.go - 安装 YMP 依赖包
// H-005: 安装 libaio, lsof 等运行时依赖

package ymp

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepInstallDeps 安装 YMP 依赖包
func stepInstallDeps() *runner.Step {
	return &runner.Step{
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
			ympLogPhase(ctx, "plan", "H-005: Install YMP Dependencies")
			packages := ctx.GetParamString("ymp_deps_packages", "libaio lsof")
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)

			packagesToInstall := commonos.FilterUninstalledPackages(ctx, packages, pkgManager)
			if len(packagesToInstall) == 0 {
				ympLogPhase(ctx, "deps-skip", "all_ymp_dependencies_already_installed=true")
				return nil
			}

			ympLogPhase(ctx, "deps-install-start", fmt.Sprintf("pkg_mgr=%s packages=%s", pkgManager, strings.Join(packagesToInstall, " ")))
			if err := commonos.InstallPackages(ctx, strings.Join(packagesToInstall, " ")); err != nil {
				return fmt.Errorf("failed to install YMP dependencies: %w", err)
			}

			ympLogPhase(ctx, "deps-install-done", fmt.Sprintf("packages=%s", strings.Join(packagesToInstall, " ")))
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			packages := ctx.GetParamString("ymp_deps_packages", "libaio lsof")
			for _, pkg := range strings.Fields(packages) {
				if commonos.IsPackageInstalled(ctx, pkg, pkgManager) {
					ympLogPhase(ctx, "verify-done", fmt.Sprintf("package=%s", pkg))
				} else {
					ctx.Logger.Warn("Package may not be installed: %s", pkg)
				}
			}
			return nil
		},
	}
}
