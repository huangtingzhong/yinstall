package os

import (
	"fmt"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	commoninstall "github.com/yinstall/internal/common/install"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// collectMissingDependencyPackages 返回尚未安装的 DB 依赖包名列表；YAC 模式下若 multipath 未装则返回其包名（单字符串）。
func collectMissingDependencyPackages(ctx *runner.StepContext) (missingDB []string, missingMultipath string) {
	pkgManager := commonos.GetPkgManager(ctx.OSInfo)
	dbPackages := ctx.GetParamString("os_deps_db_packages", "libzstd zlib lz4 openssl openssl-devel libaio tar unzip")
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
		osLogPhase(ctx, "deps-check-missing-db", fmt.Sprintf("packages=%v", missingDB))
		return false
	}
	if missingMP != "" {
		osLogPhase(ctx, "deps-check-missing-multipath", fmt.Sprintf("package=%s", missingMP))
		return false
	}

	osLogPhase(ctx, "deps-check-all-installed", "all_required_packages_installed=true")
	return true
}

// b015YumRepoHint 只读探测仓库提示，供 precheck 告警 remediation 使用（不安装包）。
func b015YumRepoHint(ctx *runner.StepContext) string {
	pm := commonos.GetPkgManager(ctx.OSInfo)
	mode := commonos.GetYumMode(ctx)
	display := mode
	if display == "" {
		display = "(empty/system repos)"
	}
	if pm == "apt" {
		return fmt.Sprintf("apt: ensure /etc/apt/sources.list (or .list.d) is correct; run apt-get update if needed. --os-yum-mode=%s mainly applies to yum/dnf in this tool.", display)
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
		return fmt.Sprintf("%s: repolist shows enabled repos (~%d lines). Use --os-yum-mode empty (system), local (ISO), or IP/http URL (custom yum); current=%s.", pm, n, display)
	}
	return fmt.Sprintf("%s: repolist empty or unavailable; use --os-yum-mode=local, IP/http URL for custom yum, or fix system repos. current --os-yum-mode=%s.", pm, display)
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

	osLogPhase(ctx, "build-start", "libzstd from zstd source EL7")
	if _, err := commoninstall.RunShellScript(ctx, script, true); err != nil {
		osLogPhase(ctx, "build-fail", runner.TruncateForLog(err.Error(), 120))
		return fmt.Errorf("zstd build/install failed (requires gcc and make on target; see debug log for details): %w", err)
	}
	osLogPhase(ctx, "build-done", "libzstd installed under /usr/local; ldconfig run")
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
	osLogPhase(ctx, "libzstd-build-start", "EL7 building libzstd from zstd source tarball (libzstd RPM may be absent from repos)")
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

// stepInstallDeps 安装 DB 依赖与常用工具包
func stepInstallDeps() *runner.Step {
	return &runner.Step{
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
					StepName:    "Install Dependencies",
					Severity:    runner.PrecheckSeverityWarn,
					Code:        "PC.OS.B015.MISSING_PACKAGES",
					Message:     strings.Join(parts, "; ") + ". Install Dependencies will run package installs on apply.",
					Remediation: "Tune --os-yum-mode (empty or local), --os-deps-db-packages, optional --os-deps-tools-packages; use --os-ignore-install-errors only if partial install is acceptable. " + hint,
				})
			}

			// 强制模式下，即使包已安装也继续执行（重新安装）
			if ctx.IsForceStep() {
				osLogPhase(ctx, "force-reinstall", "will reinstall packages even if already installed")
				return nil
			}
			// 检查必需的软件包是否已安装，如果都已安装则跳过
			if len(missDB) == 0 && missMP == "" {
				osLogPhase(ctx, "deps-skip", "all_required_packages_already_installed=true")
				return fmt.Errorf("all required packages already installed, skipping installation (use -f %s or %s to reinstall)", ctx.CurrentStepID, ctx.ForceStepsHint())
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-015: Install Dependencies")

			dbPackages := ctx.GetParamString("os_deps_db_packages", "libzstd zlib lz4 openssl openssl-devel libaio tar unzip")
			toolsPackages := ctx.GetParamString("os_deps_tools_packages", "")
			ignoreErrors := ctx.GetParamBool("os_ignore_install_errors", false)
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			isYACMode := ctx.GetParamBool("yac_mode", false)

			// 安装 DB 依赖包
			if dbPackages != "" {
				osLogPhase(ctx, "db-check-start", fmt.Sprintf("packages=%s pkg_mgr=%s", dbPackages, pkgManager))
				packagesToInstall := commonos.FilterUninstalledPackages(ctx, dbPackages, pkgManager)

				var err error
				packagesToInstall, err = b015MaybeBuildLibzstdForEL7(ctx, packagesToInstall, ignoreErrors, nil)
				if err != nil {
					return err
				}

				if len(packagesToInstall) == 0 {
					osLogPhase(ctx, "db-skip", "all_db_dependencies_already_installed=true")
				} else {
					osLogPhase(ctx, "db-install-start", fmt.Sprintf("packages=%s", strings.Join(packagesToInstall, " ")))
					if err := commonos.InstallPackages(ctx, strings.Join(packagesToInstall, " ")); err != nil {
						return fmt.Errorf("failed to install DB dependencies: %w", err)
					}
					osLogPhase(ctx, "db-install-done", fmt.Sprintf("packages=%s", strings.Join(packagesToInstall, " ")))
				}
			}

			// YAC 模式：安装 multipath 相关包
			if isYACMode {
				multipathPkg := getMultipathPackage(ctx.OSInfo)
				osLogPhase(ctx, "multipath-check-start", fmt.Sprintf("package=%s", multipathPkg))
				if commonos.IsPackageInstalled(ctx, multipathPkg, pkgManager) {
					osLogPhase(ctx, "multipath-skip", fmt.Sprintf("package=%s already_installed=true", multipathPkg))
				} else {
					osLogPhase(ctx, "multipath-install-start", fmt.Sprintf("package=%s", multipathPkg))
					if err := commonos.InstallPackages(ctx, multipathPkg); err != nil {
						return fmt.Errorf("failed to install multipath software: %w", err)
					}
					osLogPhase(ctx, "multipath-install-done", fmt.Sprintf("package=%s", multipathPkg))
				}
			}

			// 安装常用工具包（可选；允许部分包不可用）
			if toolsPackages != "" {
				osLogPhase(ctx, "tools-install-start", fmt.Sprintf("packages=%s", toolsPackages))
				successCount := 0
				failCount := 0
				prevIgnore := ignoreErrors
				ctx.Params["os_ignore_install_errors"] = true

				for _, pkg := range strings.Fields(toolsPackages) {
					pkg = strings.TrimSpace(pkg)
					if pkg == "" {
						continue
					}
					if isYACMode && isMultipathPackage(pkg) {
						osLogPhase(ctx, "tools-pkg-skip-yac", fmt.Sprintf("package=%s reason=yac_multipath", pkg))
						successCount++
						continue
					}
					if commonos.IsPackageInstalled(ctx, pkg, pkgManager) {
						successCount++
						continue
					}
					if err := commonos.InstallPackages(ctx, pkg); err != nil {
						failCount++
						osLogPhase(ctx, "tools-pkg-unavailable", fmt.Sprintf("package=%s reason=install_failed", pkg))
					} else if commonos.IsPackageInstalled(ctx, pkg, pkgManager) {
						successCount++
					} else {
						failCount++
						osLogPhase(ctx, "tools-pkg-unavailable", fmt.Sprintf("package=%s reason=still_missing_after_install", pkg))
					}
				}
				ctx.Params["os_ignore_install_errors"] = prevIgnore
				osLogPhase(ctx, "tools-install-done", fmt.Sprintf("succeeded=%d skipped=%d", successCount, failCount))
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			ignoreErrors := ctx.GetParamBool("os_ignore_install_errors", false)
			pkgManager := commonos.GetPkgManager(ctx.OSInfo)
			dbPackages := ctx.GetParamString("os_deps_db_packages", "libzstd zlib lz4 openssl openssl-devel libaio tar unzip")
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
					osLogPhase(ctx, "multipath-verify-done", "multipath_software_verified=true")
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
