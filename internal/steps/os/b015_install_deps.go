package os

import (
	"fmt"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// collectMissingDependencyPackages 返回尚未安装的 DB 依赖包名列表；YAC 模式下若 multipath 未装则返回其包名（单字符串）。
func collectMissingDependencyPackages(ctx *runner.StepContext) (missingDB []string, missingMultipath string) {
	pkgManager := commonos.GetPkgManager(ctx.OSInfo)
	dbPackages := ctx.GetParamString("os_deps_db_packages", "libzstd zlib lz4 openssl openssl-devel libaio")
	if dbPackages != "" {
		missingDB = commonos.FilterUninstalledPackages(ctx, dbPackages, pkgManager)
	}
	if ctx.GetParamBool("yac_mode", false) {
		mp := getMultipathPackage(ctx.OSInfo)
		if !commonos.IsPackageInstalled(ctx, mp, pkgManager) {
			missingMultipath = mp
		}
	}
	return
}

// areRequiredPackagesInstalled 判断是否已安装所需依赖包
func areRequiredPackagesInstalled(ctx *runner.StepContext) bool {
	missingDB, missingMP := collectMissingDependencyPackages(ctx)
	if len(missingDB) > 0 {
		ctx.Logger.Info("Some DB dependency packages are not installed: %v", missingDB)
		return false
	}
	if missingMP != "" {
		ctx.Logger.Info("Multipath package '%s' not installed, need to install dependencies", missingMP)
		return false
	}

	ctx.Logger.Info("All required packages already installed")
	return true
}

// b015YumRepoHint 只读探测仓库提示，供 precheck 告警 remediation 使用（不安装包）。
func b015YumRepoHint(ctx *runner.StepContext) string {
	pm := commonos.GetPkgManager(ctx.OSInfo)
	mode := ctx.GetParamString("os_yum_mode", "none")
	if pm == "apt" {
		return fmt.Sprintf("apt: ensure /etc/apt/sources.list (or .list.d) is correct; run apt-get update if needed. --os-yum-mode=%s mainly applies to yum/dnf in this tool.", mode)
	}
	res, _ := ctx.Execute(fmt.Sprintf("%s repolist 2>/dev/null", pm), false)
	out := ""
	if res != nil {
		out = strings.TrimSpace(res.GetStdout())
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(strings.ToLower(s), "repo id") {
			continue
		}
		n++
	}
	if n > 0 {
		return fmt.Sprintf("%s: repolist shows enabled repos (~%d lines). Use --os-yum-mode none|online|local-iso as fits your network/ISO; current=%s.", pm, n, mode)
	}
	return fmt.Sprintf("%s: repolist empty or unavailable; for offline nodes use --os-yum-mode=local-iso (see B-013 mount ISO) or register the system. current --os-yum-mode=%s.", pm, mode)
}

// b015StillMissingPackages 检查 wanted 列表中仍未安装的包名（rpm/dpkg）。
func b015StillMissingPackages(ctx *runner.StepContext, wanted []string, pkgManager string) []string {
	if len(wanted) == 0 {
		return nil
	}
	return commonos.FilterUninstalledPackages(ctx, strings.Join(wanted, " "), pkgManager)
}

// b015MergeUniqueStrings appends items from add that are not already in base.
func b015MergeUniqueStrings(base, add []string) []string {
	seen := make(map[string]bool)
	for _, s := range base {
		seen[s] = true
	}
	for _, s := range add {
		if !seen[s] {
			seen[s] = true
			base = append(base, s)
		}
	}
	return base
}

func b015SliceContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func b015SliceRemove(list []string, s string) []string {
	out := list[:0]
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// b015TryInstallLibzstdFromSourceEL7 在 RHEL7/OL7/CentOS7 等无 libzstd RPM 时，从 zstd 源码包编译安装。
func b015TryInstallLibzstdFromSourceEL7(ctx *runner.StepContext) error {
	explicit := ctx.GetParamString("os_zstd_source_tarball", "")
	nameOrPath, err := commonfile.FindZstdSourceTarball(ctx, ctx.LocalSoftwareDirs, ctx.RemoteSoftwareDir, explicit)
	if err != nil {
		return fmt.Errorf("zstd source tarball not found (EL7 repos often lack libzstd RPM); place zstd-1.5.7.tar.gz under --local-software-dirs or remote --remote-software-dir: %w", err)
	}

	remoteTar, err := commonfile.FindAndDistribute(ctx, nameOrPath, ctx.LocalSoftwareDirs, ctx.RemoteSoftwareDir)
	if err != nil {
		return fmt.Errorf("failed to distribute zstd source tarball: %w", err)
	}

	shQuote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	qTar := shQuote(remoteTar)

	// Privileged operations run via ExecuteWithCheck(..., sudo=true). For non-root SSH users the
	// executor wraps this script as `sudo -n bash -c '...'` (same model as yum installs in this step).
	// Requires passwordless sudo (NOPASSWD) or sudo -n success; interactive sudo is not supported.
	// Build + install + ld.so config therefore run as root even when the login user is not root.
	script := fmt.Sprintf(`set -euo pipefail
TAR=%s
BUILD=$(mktemp -d /tmp/yinstall-zstd.XXXXXX)
cleanup() { rm -rf "$BUILD"; }
trap cleanup EXIT
tar -zxf "$TAR" -C "$BUILD"
TOP=$(find "$BUILD" -maxdepth 1 -type d -name 'zstd-*' | head -1)
test -n "$TOP"
cd "$TOP"
make
make install
mkdir -p /etc/ld.so.conf.d
if [ ! -f /etc/ld.so.conf.d/zstd-local.conf ] || ! grep -qxF '/usr/local/lib' /etc/ld.so.conf.d/zstd-local.conf 2>/dev/null; then
  echo '/usr/local/lib' > /etc/ld.so.conf.d/zstd-local.conf
fi
ldconfig
command -v zstd >/dev/null
test -f /usr/local/lib/libzstd.so.1 -o -f /usr/local/lib/libzstd.so -o -f /usr/local/lib64/libzstd.so.1 -o -f /usr/local/lib64/libzstd.so
`, qTar)

	if _, err := ctx.ExecuteWithCheck(script, true); err != nil {
		return fmt.Errorf("zstd build/install failed (requires gcc and make on target; see debug log for details): %w", err)
	}
	ctx.Logger.Info("zstd built from source under /usr/local; ldconfig run")
	return nil
}

// b015MaybeBuildLibzstdForEL7 在待装列表含 libzstd 且为 EL7 时先尝试源码安装，并从 yum 列表中移除 libzstd（避免对不存在的包反复 yum）
func b015MaybeBuildLibzstdForEL7(ctx *runner.StepContext, packagesToInstall []string, ignoreErrors bool, failedPackages *[]string) ([]string, error) {
	if !commonos.IsRHEL7(ctx.OSInfo) || !b015SliceContains(packagesToInstall, "libzstd") {
		return packagesToInstall, nil
	}
	if commonos.IsDepPackageSatisfied(ctx, "libzstd", commonos.GetPkgManager(ctx.OSInfo)) {
		return b015SliceRemove(packagesToInstall, "libzstd"), nil
	}
	ctx.Logger.Info("EL7: building libzstd from zstd-*.tar.gz (libzstd RPM may be absent from repos)")
	err := b015TryInstallLibzstdFromSourceEL7(ctx)
	if err == nil {
		return b015SliceRemove(packagesToInstall, "libzstd"), nil
	}
	if ignoreErrors {
		ctx.Logger.Warn("zstd/libzstd source install failed (ignored; post-check will reflect): %v", err)
		if failedPackages != nil {
			*failedPackages = b015MergeUniqueStrings(*failedPackages, []string{"libzstd"})
		}
		return b015SliceRemove(packagesToInstall, "libzstd"), nil
	}
	return nil, err
}

// StepB015InstallDeps 安装 DB 依赖与常用工具包
func StepB015InstallDeps() *runner.Step {
	return &runner.Step{
		ID:          "B-015",
		Name:        "Install Dependencies",
		Description: "Install YashanDB dependency packages and common tools",
		Tags:        []string{"os", "deps"},
		Optional:    true, // Allow skipping when packages are already installed

		PreCheck: func(ctx *runner.StepContext) error {
			missDB, missMP := collectMissingDependencyPackages(ctx)

			// --precheck：缺失依赖时给出 warn，提示配置 yum/dnf/apt 与 os_yum_mode（apply 仍将由本步安装）
			if ctx.Precheck && (len(missDB) > 0 || missMP != "") {
				var parts []string
				if len(missDB) > 0 {
					parts = append(parts, "missing DB dependency package(s): "+strings.Join(missDB, ", "))
				}
				if missMP != "" {
					parts = append(parts, "missing YAC multipath package: "+missMP)
				}
				hint := b015YumRepoHint(ctx)
				ctx.ReportPrecheckIssue(runner.PrecheckIssue{
					StepID:      "B-015",
					StepName:    "Install Dependencies",
					Severity:    runner.PrecheckSeverityWarn,
					Code:        "PC.OS.B015.MISSING_PACKAGES",
					Message:     strings.Join(parts, "; ") + ". Install Dependencies will run package installs on apply.",
					Remediation: "Tune --os-yum-mode (none|online|local-iso), --os-deps-db-packages, optional --os-deps-tools-packages; use --os-ignore-install-errors only if partial install is acceptable. " + hint,
				})
			}

			// 强制模式下，即使包已安装也继续执行（重新安装）
			if ctx.IsForceStep() {
				ctx.Logger.Info("Force mode: will reinstall packages even if already installed")
				return nil
			}
			// 检查必需的软件包是否已安装，如果都已安装则跳过
			if len(missDB) == 0 && missMP == "" {
				ctx.Logger.Info("All required packages already installed")
				return fmt.Errorf("all required packages already installed, skipping installation (use -f B-015 or --force-steps B-015 to reinstall)")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbPackages := ctx.GetParamString("os_deps_db_packages", "libzstd zlib lz4 openssl openssl-devel libaio")
			toolsPackages := ctx.GetParamString("os_deps_tools_packages", "")
			yumMode := ctx.GetParamString("os_yum_mode", "none")
			ignoreErrors := ctx.GetParamBool("os_ignore_install_errors", false)
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			isYACMode := ctx.GetParamBool("yac_mode", false)

			var failedPackages []string

			// 安装 DB 依赖包
			if dbPackages != "" {
				ctx.Logger.Info("Checking DB dependencies: %s", dbPackages)

				// 检查哪些包需要安装
				packagesToInstall := commonos.FilterUninstalledPackages(ctx, dbPackages, pkgManager)

				var err error
				packagesToInstall, err = b015MaybeBuildLibzstdForEL7(ctx, packagesToInstall, ignoreErrors, &failedPackages)
				if err != nil {
					return err
				}

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
						// yum 可能对某些包名返回 0 但未装上：再以 rpm/dpkg 校验并并入失败列表
						still := b015StillMissingPackages(ctx, packagesToInstall, pkgManager)
						failedPackages = b015MergeUniqueStrings(failedPackages, still)
						for _, p := range still {
							ctx.Logger.Warn("DB dependency still not installed after yum (may be missing from repos): %s (--os-ignore-install-errors)", p)
						}
					} else {
						// 批量安装：命令失败则退出；yum/dnf 对部分包名报 No package 仍可能 exit 0，必须装后校验
						cmd := commonos.BuildInstallCmd(pkgManager, yumMode, strings.Join(packagesToInstall, " "), commonos.IsRHEL8(ctx.OSInfo))
						if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
							return fmt.Errorf("failed to install DB dependencies: %w", err)
						}
						still := b015StillMissingPackages(ctx, packagesToInstall, pkgManager)
						if len(still) > 0 {
							return fmt.Errorf("DB dependencies still not installed after yum (repos may lack these packages; try --os-yum-mode / different ISO / adjust --os-deps-db-packages): %s", strings.Join(still, ", "))
						}
					}
				}
			}

			// YAC 模式：安装 multipath 相关包
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
						if !commonos.IsPackageInstalled(ctx, multipathPkg, pkgManager) {
							failedPackages = b015MergeUniqueStrings(failedPackages, []string{multipathPkg})
							ctx.Logger.Warn("Multipath package %s still not present after yum (--os-ignore-install-errors)", multipathPkg)
						}
					} else {
						if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
							return fmt.Errorf("failed to install multipath software: %w", err)
						}
						if !commonos.IsPackageInstalled(ctx, multipathPkg, pkgManager) {
							return fmt.Errorf("multipath package %s still not installed after yum", multipathPkg)
						}
						ctx.Logger.Info("Multipath software installed successfully")
					}
				}
			}

			// 安装常用工具包（可选；允许部分包不可用）
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
					// YAC 模式下 multipath 若已由上文安装则跳过重复安装
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
			ignoreErrors := ctx.GetParamBool("os_ignore_install_errors", false)
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			dbPackages := ctx.GetParamString("os_deps_db_packages", "libzstd zlib lz4 openssl openssl-devel libaio")
			isYACMode := ctx.GetParamBool("yac_mode", false)

			if dbPackages != "" {
				missing := commonos.FilterUninstalledPackages(ctx, dbPackages, pkgManager)
				if len(missing) > 0 {
					if ignoreErrors {
						ctx.Logger.Warn("B-015 post-check: DB dependency package(s) still missing (allowed by --os-ignore-install-errors): %s", strings.Join(missing, ", "))
					} else {
						return fmt.Errorf("DB dependency packages not installed: %s", strings.Join(missing, ", "))
					}
				}
			}

			var cmd string
			if pkgManager == "apt" {
				cmd = "dpkg -l | grep openssl"
			} else {
				cmd = "rpm -q openssl"
			}
			result, err := ctx.Execute(cmd, false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				if ignoreErrors {
					ctx.Logger.Warn("B-015 post-check: openssl not detected (--os-ignore-install-errors)")
				} else {
					return fmt.Errorf("openssl package not installed")
				}
			}

			if isYACMode {
				mp := getMultipathPackage(ctx.OSInfo)
				if !commonos.IsPackageInstalled(ctx, mp, pkgManager) {
					if ignoreErrors {
						ctx.Logger.Warn("B-015 post-check: multipath package %s not installed (--os-ignore-install-errors)", mp)
					} else {
						return fmt.Errorf("multipath software not installed (%s)", mp)
					}
				} else {
					result, _ := ctx.Execute("which multipath 2>/dev/null || rpm -q device-mapper-multipath 2>/dev/null || dpkg -l multipath-tools 2>/dev/null", false)
					if result == nil || result.GetExitCode() != 0 {
						if ignoreErrors {
							ctx.Logger.Warn("B-015 post-check: multipath binary not on PATH (--os-ignore-install-errors)")
						} else {
							return fmt.Errorf("multipath software not installed")
						}
					}
					ctx.Logger.Info("Multipath software verified")
				}
			}

			return nil
		},
	}
}

// getMultipathPackage 返回当前 OS 对应的 multipath 包名
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

// isMultipathPackage 判断包名是否为 multipath 相关包
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
