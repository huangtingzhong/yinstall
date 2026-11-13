// pkg.go - 软件包管理公共函数
// 提供包检测、安装命令构建等通用逻辑，被 OS 安装步骤和 YCM 安装步骤共用

package os

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/yinstall/internal/runner"
)

// isSafePkgToken 限制拼进 shell 的包名/能力名字符，避免注入。
func isSafePkgToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '-', '_', '.', '+', ':', '@', '/':
			continue
		default:
			return false
		}
	}
	return true
}

// ResolveInstallNames 将逻辑包名解析为当前 OS 上应传给包管理器的真实安装名（有序）。
// 无映射时返回 []string{logicalName}；依赖 DetectOSType 填充的 OSInfo 族标志。
func ResolveInstallNames(osInfo *runner.OSInfo, logicalName string) []string {
	name := strings.TrimSpace(logicalName)
	if name == "" {
		return nil
	}
	switch name {
	case "dstat":
		// EL8 族 / openEuler / 麒麟：无独立 dstat RPM，能力由 pcp-system-tools 提供
		if IsRHEL8(osInfo) || IsOpenEuler(osInfo) || IsKylin(osInfo) {
			return []string{"pcp-system-tools"}
		}
	}
	return []string{name}
}

// ExpandLogicalInstallPackages 将空格分隔的逻辑包名展开为安装名串；logical 为保序去重后的逻辑名列表。
func ExpandLogicalInstallPackages(osInfo *runner.OSInfo, packages string) (logical []string, install string) {
	seenLogical := map[string]struct{}{}
	seenInstall := map[string]struct{}{}
	var installNames []string
	for _, raw := range strings.Fields(packages) {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := seenLogical[name]; !ok {
			seenLogical[name] = struct{}{}
			logical = append(logical, name)
		}
		for _, n := range ResolveInstallNames(osInfo, name) {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if _, ok := seenInstall[n]; ok {
				continue
			}
			seenInstall[n] = struct{}{}
			installNames = append(installNames, n)
		}
	}
	return logical, strings.Join(installNames, " ")
}

// IsPackageInstalled checks if a package is already installed via rpm or dpkg
func IsPackageInstalled(ctx *runner.StepContext, pkg, pkgManager string) bool {
	pkg = strings.TrimSpace(pkg)
	if !isSafePkgToken(pkg) {
		return false
	}
	var checkCmd string
	if pkgManager == "apt" {
		// 保留 dpkg-query 的 stdout/stderr，便于 debug 日志分析；不再用 grep -q 吞掉输出
		checkCmd = fmt.Sprintf("dpkg-query -W -f '${db:Status-Status}' %s 2>&1", pkg)
	} else {
		checkCmd = fmt.Sprintf("rpm -q %s 2>&1", pkg)
	}

	result, _ := ctx.Execute(checkCmd, false)
	if result == nil {
		return false
	}
	if pkgManager == "apt" {
		return result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) == "installed"
	}
	return result.GetExitCode() == 0
}

// rpmProvidesSatisfied 用 rpm --whatprovides 判断能力/文件是否已由某已装包提供。
func rpmProvidesSatisfied(ctx *runner.StepContext, pkg string) bool {
	if ctx == nil || !isSafePkgToken(pkg) {
		return false
	}
	result, _ := ctx.Execute(fmt.Sprintf("rpm -q --whatprovides %s 2>&1", pkg), false)
	return result != nil && result.GetExitCode() == 0
}

// libzstdSourceSatisfiedRHEL7 判断 EL7 系列是否已通过源码安装获得 libzstd（仓库常无 libzstd RPM）
func libzstdSourceSatisfiedRHEL7(ctx *runner.StepContext) bool {
	r1, _ := ctx.Execute("command -v zstd 2>&1", false)
	if r1 == nil || r1.GetExitCode() != 0 {
		return false
	}
	r2, _ := ctx.Execute("test -f /usr/local/lib/libzstd.so.1 -o -f /usr/local/lib/libzstd.so -o -f /usr/local/lib64/libzstd.so.1 -o -f /usr/local/lib64/libzstd.so", false)
	return r2 != nil && r2.GetExitCode() == 0
}

// libzstdSatisfiedByZstdRPM 部分 RPM 系发行版（如麒麟 Kylin、部分 RHEL/Fedora 衍生）不提供独立 libzstd 包名，
// 运行时由 zstd 主包提供；dnf install libzstd 可能解析为已装 zstd，但 rpm -q libzstd 仍会失败。
func libzstdSatisfiedByZstdRPM(ctx *runner.StepContext, pkgManager string) bool {
	if pkgManager == "apt" {
		return false
	}
	r, _ := ctx.Execute("rpm -q zstd 2>&1", false)
	return r != nil && r.GetExitCode() == 0
}

// libzstdSatisfiedByAptAliases Debian/Ubuntu 上 dpkg 常为 libzstd1（共享库），CLI 为 zstd 元包；
// 无独立 ii 记录名为 libzstd 时仍应视为 libzstd 依赖已满足。
func libzstdSatisfiedByAptAliases(ctx *runner.StepContext, pkgManager string) bool {
	if pkgManager != "apt" {
		return false
	}
	for _, name := range []string{"libzstd1", "zstd"} {
		if IsPackageInstalled(ctx, name, "apt") {
			return true
		}
	}
	return false
}

// IsDepPackageSatisfied 判断逻辑依赖/工具名是否已满足。
// 含：字面已装、RPM Provides（whatprovides）、以及 libzstd 历史特例。
func IsDepPackageSatisfied(ctx *runner.StepContext, pkg, pkgManager string) bool {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return false
	}
	if IsPackageInstalled(ctx, pkg, pkgManager) {
		return true
	}
	if pkgManager != "apt" && rpmProvidesSatisfied(ctx, pkg) {
		if ctx != nil {
			ctx.LogPhase("pkg-satisfied", fmt.Sprintf("package=%s via=whatprovides", pkg))
		}
		return true
	}
	if pkg == "libzstd" {
		if ctx != nil && ctx.OSInfo != nil && IsRHEL7(ctx.OSInfo) && libzstdSourceSatisfiedRHEL7(ctx) {
			return true
		}
		if libzstdSatisfiedByAptAliases(ctx, pkgManager) {
			return true
		}
		if libzstdSatisfiedByZstdRPM(ctx, pkgManager) {
			return true
		}
	}
	return false
}

// FilterUninstalledPackages returns only packages that are not yet installed
func FilterUninstalledPackages(ctx *runner.StepContext, packages, pkgManager string) []string {
	pkgList := strings.Fields(packages)
	var uninstalled []string

	for _, pkg := range pkgList {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}

		if !IsDepPackageSatisfied(ctx, pkg, pkgManager) {
			uninstalled = append(uninstalled, pkg)
		} else {
			ctx.LogPhase("pkg-skip-installed", fmt.Sprintf("package=%s pkg_mgr=%s", pkg, pkgManager))
		}
	}

	return uninstalled
}

// BuildInstallCmd builds the install command based on package manager and yum mode.
// yumMode: "" = system repos; YumModeLocal = local ISO repo; YumModeHTTP = custom HTTP repo.
// isRHEL8：与 BaseURLs / ensureRepoFile 一致；仅 RHEL8 族双仓，openEuler/RHEL7 等单仓。
func BuildInstallCmd(pkgManager, yumMode, packages string, isRHEL8 bool) string {
	if IsLocalYumMode(yumMode) {
		if isRHEL8 {
			return fmt.Sprintf("%s -y install --disablerepo=\\* --enablerepo=local-baseos --enablerepo=local-appstream %s", pkgManager, packages)
		}
		return fmt.Sprintf("%s -y install --disablerepo=\\* --enablerepo=local %s", pkgManager, packages)
	}
	if IsHTTPYumMode(yumMode) {
		if isRHEL8 {
			return fmt.Sprintf("%s -y install --disablerepo=\\* --enablerepo=%s --enablerepo=%s %s",
				pkgManager, yumHTTPRepoBaseOS, yumHTTPRepoAppStream, packages)
		}
		return fmt.Sprintf("%s -y install --disablerepo=\\* --enablerepo=%s %s",
			pkgManager, yumHTTPRepoSingle, packages)
	}

	if pkgManager == "apt" {
		return fmt.Sprintf("apt-get -y install %s", packages)
	}
	return fmt.Sprintf("%s -y install %s", pkgManager, packages)
}
