// pkg.go - 软件包管理公共函数
// 提供包检测、安装命令构建等通用逻辑，被 OS 安装步骤和 YCM 安装步骤共用

package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// IsPackageInstalled checks if a package is already installed via rpm or dpkg
func IsPackageInstalled(ctx *runner.StepContext, pkg, pkgManager string) bool {
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

// IsDepPackageSatisfied 判断依赖包是否已满足（含 libzstd：EL7 源码、Debian/Ubuntu 别名、RPM 系 zstd 主包）
func IsDepPackageSatisfied(ctx *runner.StepContext, pkg, pkgManager string) bool {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return false
	}
	if IsPackageInstalled(ctx, pkg, pkgManager) {
		return true
	}
	if pkg == "libzstd" {
		if ctx.OSInfo != nil && IsRHEL7(ctx.OSInfo) && libzstdSourceSatisfiedRHEL7(ctx) {
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
			ctx.Logger.Info("  Package '%s' already installed", pkg)
		}
	}

	return uninstalled
}

// BuildInstallCmd builds the install command based on package manager and yum mode
// yumMode 取值: "local-iso"（使用本地 ISO 仓库）、"none"（使用默认/网络仓库）
func BuildInstallCmd(pkgManager, yumMode, packages string, isRHEL8 bool) string {
	if yumMode == "local-iso" {
		if isRHEL8 {
			return fmt.Sprintf("%s -y install --disablerepo=\\* --enablerepo=local-baseos --enablerepo=local-appstream %s", pkgManager, packages)
		}
		return fmt.Sprintf("%s -y install --disablerepo=\\* --enablerepo=local %s", pkgManager, packages)
	}

	if pkgManager == "apt" {
		return fmt.Sprintf("apt-get -y install %s", packages)
	}
	return fmt.Sprintf("%s -y install %s", pkgManager, packages)
}
