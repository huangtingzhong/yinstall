package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// areRequiredPackagesInstalled checks if all required packages are already installed
func areRequiredPackagesInstalled(ctx *runner.StepContext) bool {
	pkgManager := commonos.GetPkgManager(ctx.OSInfo)
	isYACMode := ctx.GetParamBool("yac_mode", false)

	// 获取需要安装的 DB 依赖包列表（与 Action 中使用的包列表一致）
	dbPackages := ctx.GetParamString("os_deps_db_packages", "libzstd zlib lz4 openssl openssl-devel")
	if dbPackages != "" {
		// 检查所有 DB 依赖包是否已安装
		packagesToInstall := commonos.FilterUninstalledPackages(ctx, dbPackages, pkgManager)
		if len(packagesToInstall) > 0 {
			ctx.Logger.Info("Some DB dependency packages are not installed: %v", packagesToInstall)
			return false
		}
	}

	// YAC 模式下检查 multipath
	if isYACMode {
		multipathPkg := getMultipathPackage(ctx.OSInfo)
		if !commonos.IsPackageInstalled(ctx, multipathPkg, pkgManager) {
			ctx.Logger.Info("Multipath package '%s' not installed, need to install dependencies", multipathPkg)
			return false
		}
	}

	ctx.Logger.Info("All required packages already installed")
	return true
}

// StepB015InstallDeps Install dependency packages
func StepB015InstallDeps() *runner.Step {
	return &runner.Step{
		ID:          "B-015",
		Name:        "Install Dependencies",
		Description: "Install YashanDB dependency packages and common tools",
		Tags:        []string{"os", "deps"},
		Optional:    true, // Allow skipping when packages are already installed

		PreCheck: func(ctx *runner.StepContext) error {
			// 强制模式下，即使包已安装也继续执行（重新安装）
			if ctx.IsForceStep() {
				ctx.Logger.Info("Force mode: will reinstall packages even if already installed")
				return nil
			}
			// 检查必需的软件包是否已安装，如果都已安装则跳过
			if areRequiredPackagesInstalled(ctx) {
				return fmt.Errorf("all required packages already installed, skipping installation (use --force B-015 to reinstall)")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbPackages := ctx.GetParamString("os_deps_db_packages", "libzstd zlib lz4 openssl openssl-devel")
			toolsPackages := ctx.GetParamString("os_deps_tools_packages", "")
			yumMode := ctx.GetParamString("os_yum_mode", "none")
			ignoreErrors := ctx.GetParamBool("os_ignore_install_errors", false)
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			isYACMode := ctx.GetParamBool("yac_mode", false)

			var failedPackages []string

			// Install DB dependency packages
			if dbPackages != "" {
				ctx.Logger.Info("Checking DB dependencies: %s", dbPackages)

				// 检查哪些包需要安装
				packagesToInstall := commonos.FilterUninstalledPackages(ctx, dbPackages, pkgManager)

				if len(packagesToInstall) == 0 {
					ctx.Logger.Info("All DB dependencies already installed, skipping")
				} else {
					ctx.Logger.Info("Installing missing DB dependencies: %s", strings.Join(packagesToInstall, " "))

					if ignoreErrors {
						// 逐个安装,记录失败的包
						for _, pkg := range packagesToInstall {
							cmd := commonos.BuildInstallCmd(pkgManager, yumMode, pkg, commonos.IsRHEL8(ctx.OSInfo))
							result, _ := ctx.Execute(cmd, true)
							if result == nil || result.GetExitCode() != 0 {
								failedPackages = append(failedPackages, pkg)
								ctx.Logger.Warn("Failed to install DB dependency: %s (ignored)", pkg)
							} else {
								ctx.Logger.Info("Successfully installed: %s", pkg)
							}
						}
					} else {
						// 批量安装,失败则退出
						cmd := commonos.BuildInstallCmd(pkgManager, yumMode, strings.Join(packagesToInstall, " "), commonos.IsRHEL8(ctx.OSInfo))
						if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
							return fmt.Errorf("failed to install DB dependencies: %w", err)
						}
					}
				}
			}

			// In YAC mode, install multipath software
			if isYACMode {
				multipathPkg := getMultipathPackage(ctx.OSInfo)
				ctx.Logger.Info("YAC mode detected, checking multipath software: %s", multipathPkg)

				// 检查 multipath 是否已安装
				if commonos.IsPackageInstalled(ctx, multipathPkg, pkgManager) {
					ctx.Logger.Info("Multipath software already installed, skipping")
				} else {
					ctx.Logger.Info("Installing multipath software: %s", multipathPkg)
					cmd := commonos.BuildInstallCmd(pkgManager, yumMode, multipathPkg, commonos.IsRHEL8(ctx.OSInfo))

					if ignoreErrors {
						result, _ := ctx.Execute(cmd, true)
						if result == nil || result.GetExitCode() != 0 {
							failedPackages = append(failedPackages, multipathPkg)
							ctx.Logger.Warn("Failed to install multipath software: %s (ignored)", multipathPkg)
						} else {
							ctx.Logger.Info("Multipath software installed successfully")
						}
					} else {
						if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
							return fmt.Errorf("failed to install multipath software: %w", err)
						}
						ctx.Logger.Info("Multipath software installed successfully")
					}
				}
			}

			// Install common tools packages (optional, allow partial failures)
			if toolsPackages != "" {
				ctx.Logger.Info("Installing common tools: %s", toolsPackages)
				packages := strings.Fields(toolsPackages)
				successCount := 0
				failCount := 0

				for _, pkg := range packages {
					pkg = strings.TrimSpace(pkg)
					if pkg == "" {
						continue
					}
					// Skip multipath package if already installed in YAC mode
					if isYACMode && isMultipathPackage(pkg) {
						ctx.Logger.Info("  Package '%s' already installed (YAC mode)", pkg)
						successCount++
						continue
					}
					cmd := commonos.BuildInstallCmd(pkgManager, yumMode, pkg, commonos.IsRHEL8(ctx.OSInfo))
					result, _ := ctx.Execute(cmd, true)
					if result != nil && result.GetExitCode() == 0 {
						successCount++
					} else {
						failCount++
						ctx.Logger.Info("  Package '%s' not available (skipped)", pkg)
					}
				}
				ctx.Logger.Info("Tools installation: %d succeeded, %d skipped", successCount, failCount)
			}

			// 如果有失败的包,给出汇总提示
			if len(failedPackages) > 0 {
				ctx.Logger.Warn("The following packages failed to install: %s", strings.Join(failedPackages, ", "))
				if !ignoreErrors {
					return fmt.Errorf("package installation failed")
				}
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			var cmd string
			if commonos.GetPkgManager(ctx.OSInfo) == "apt" {
				cmd = "dpkg -l | grep openssl"
			} else {
				cmd = "rpm -q openssl"
			}
			result, err := ctx.Execute(cmd, false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("openssl package not installed")
			}

			// In YAC mode, verify multipath is installed
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if isYACMode {
				result, _ := ctx.Execute("which multipath 2>/dev/null || rpm -q device-mapper-multipath 2>/dev/null || dpkg -l multipath-tools 2>/dev/null", false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("multipath software not installed")
				}
				ctx.Logger.Info("Multipath software verified")
			}

			return nil
		},
	}
}

// getMultipathPackage returns the multipath package name for the current OS
// 不同平台的多路径软件包名称：
// - RHEL/CentOS/Oracle Linux/Rocky/Alma: device-mapper-multipath
// - Debian/Ubuntu: multipath-tools
// - SUSE/openSUSE: multipath-tools
// - Kylin/UOS: device-mapper-multipath (基于 RHEL)
func getMultipathPackage(osInfo *runner.OSInfo) string {
	if osInfo == nil {
		return "device-mapper-multipath" // 默认
	}

	pkgManager := osInfo.PkgManager
	switch pkgManager {
	case "apt":
		return "multipath-tools"
	case "zypper":
		return "multipath-tools"
	default:
		// yum/dnf (RHEL, CentOS, Oracle Linux, Kylin, UOS)
		return "device-mapper-multipath"
	}
}

// isMultipathPackage checks if a package name is a multipath package
func isMultipathPackage(pkg string) bool {
	multipathPackages := []string{
		"device-mapper-multipath",
		"multipath-tools",
	}
	for _, mp := range multipathPackages {
		if pkg == mp {
			return true
		}
	}
	return false
}
