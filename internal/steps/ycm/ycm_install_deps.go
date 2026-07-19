// g002_install_deps.go - 安装 YCM 依赖包
// G-002: 安装 libnsl 等 YCM 运行时依赖
// 复用 common/os 中的包管理公共函数，支持 ISO/光驱/网络三种安装模式

package ycm

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepInstallDeps 安装 YCM 依赖包
func stepInstallDeps() *runner.Step {
	return &runner.Step{
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
					ycmLogPhase(ctx, "deps-precheck-missing", fmt.Sprintf("package=%s", pkg))
					allInstalled = false
				}
			}
			if allInstalled {
				return fmt.Errorf("all required YCM packages already installed, skipping installation")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ycmLogPhase(ctx, "plan", "G-002: Install YCM Dependencies")
			packages := ctx.GetParamString("ycm_deps_packages", "libnsl")
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)

			ycmLogPhase(ctx, "deps-install-plan", fmt.Sprintf(
				"pkg_mgr=%s yum_mode=%s packages=%s",
				pkgManager, commonos.GetYumMode(ctx), packages,
			))

			packagesToInstall := commonos.FilterUninstalledPackages(ctx, packages, pkgManager)
			if len(packagesToInstall) == 0 {
				ycmLogPhase(ctx, "deps-skip", "all_ycm_dependencies_already_installed=true")
				return nil
			}

			ycmLogPhase(ctx, "deps-install-start", fmt.Sprintf("packages=%s", strings.Join(packagesToInstall, " ")))
			if err := commonos.InstallPackages(ctx, strings.Join(packagesToInstall, " ")); err != nil {
				return fmt.Errorf("failed to install YCM dependencies: %w", err)
			}

			ycmLogPhase(ctx, "deps-install-done", fmt.Sprintf("packages=%s", strings.Join(packagesToInstall, " ")))
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
					ycmLogPhase(ctx, "verify-done", fmt.Sprintf("package=%s method=rpm_dpkg", pkg))
				} else {
					// libnsl 等库可能包名与 rpm 名不完全一致，用 ldconfig 补充检查
					result, _ := ctx.Execute(fmt.Sprintf("ldconfig -p 2>/dev/null | grep -i %s", pkg), false)
					if result != nil && result.GetExitCode() == 0 {
						ycmLogPhase(ctx, "verify-done", fmt.Sprintf("package=%s method=ldconfig", pkg))
					} else {
						ctx.Logger.Warn("Package may not be installed: %s (non-critical)", pkg)
					}
				}
			}
			return nil
		},
	}
}
