package os

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

// DetectOSInfo 检测并填充操作系统信息
func DetectOSInfo(ctx *runner.StepContext) *runner.OSInfo {
	osInfo := &runner.OSInfo{}

	result, _ := ctx.Execute("cat /etc/os-release 2>/dev/null", false)
	if result != nil && result.GetExitCode() == 0 {
		ParseOSRelease(result.GetStdout(), osInfo)
	}

	result, _ = ctx.Execute("uname -r", false)
	if result != nil {
		osInfo.Kernel = strings.TrimSpace(result.GetStdout())
	}

	result, _ = ctx.Execute("uname -m", false)
	if result != nil {
		osInfo.Arch = strings.TrimSpace(result.GetStdout())
	}

	DetectOSType(osInfo)
	detectPkgManager(ctx, osInfo)

	return osInfo
}

// ParseOSRelease 解析 /etc/os-release 内容
func ParseOSRelease(content string, osInfo *runner.OSInfo) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "NAME=") {
			osInfo.Name = strings.Trim(strings.TrimPrefix(line, "NAME="), "\"")
		} else if strings.HasPrefix(line, "VERSION=") {
			osInfo.Version = strings.Trim(strings.TrimPrefix(line, "VERSION="), "\"")
		} else if strings.HasPrefix(line, "VERSION_ID=") {
			osInfo.VersionID = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		} else if strings.HasPrefix(line, "ID=") && !strings.HasPrefix(line, "ID_LIKE=") {
			osInfo.ID = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		}
	}
}

// DetectOSType 检测操作系统类型
func DetectOSType(osInfo *runner.OSInfo) {
	id := strings.ToLower(osInfo.ID)
	versionID := osInfo.VersionID

	// RHEL 系列
	if id == "rhel" || id == "centos" || id == "ol" || id == "rocky" || id == "almalinux" || id == "oracle" {
		if strings.HasPrefix(versionID, "7") {
			osInfo.IsRHEL7 = true
		} else if strings.HasPrefix(versionID, "8") || strings.HasPrefix(versionID, "9") {
			osInfo.IsRHEL8 = true
		}
	}

	// 麒麟系统
	if id == "kylin" || strings.Contains(strings.ToLower(osInfo.Name), "kylin") {
		osInfo.IsKylin = true
		// 麒麟 V10 基于 RHEL8
		if strings.Contains(versionID, "V10") || strings.HasPrefix(versionID, "10") {
			osInfo.IsRHEL8 = true
		}
	}

	// 统信 UOS
	if id == "uos" || strings.Contains(strings.ToLower(osInfo.Name), "uos") || strings.Contains(strings.ToLower(osInfo.Name), "uniontech") {
		osInfo.IsUOS = true
	}
}

// detectPkgManager 检测包管理器
func detectPkgManager(ctx *runner.StepContext, osInfo *runner.OSInfo) {
	result, _ := ctx.Execute("which dnf 2>/dev/null", false)
	if result != nil && result.GetExitCode() == 0 {
		osInfo.PkgManager = "dnf"
		return
	}

	result, _ = ctx.Execute("which yum 2>/dev/null", false)
	if result != nil && result.GetExitCode() == 0 {
		osInfo.PkgManager = "yum"
		return
	}

	result, _ = ctx.Execute("which apt 2>/dev/null", false)
	if result != nil && result.GetExitCode() == 0 {
		osInfo.PkgManager = "apt"
		return
	}

	osInfo.PkgManager = "unknown"
}

// IsRHEL7 判断是否为 RHEL7 系列
func IsRHEL7(osInfo *runner.OSInfo) bool {
	if osInfo != nil {
		return osInfo.IsRHEL7
	}
	return false
}

// IsRHEL8 判断是否为 RHEL8 系列
func IsRHEL8(osInfo *runner.OSInfo) bool {
	if osInfo != nil {
		return osInfo.IsRHEL8
	}
	return false
}

// IsKylin 判断是否为麒麟系统
func IsKylin(osInfo *runner.OSInfo) bool {
	if osInfo != nil {
		return osInfo.IsKylin
	}
	return false
}

// IsUOS 判断是否为统信 UOS
func IsUOS(osInfo *runner.OSInfo) bool {
	if osInfo != nil {
		return osInfo.IsUOS
	}
	return false
}

// GetPkgManager 获取包管理器
func GetPkgManager(osInfo *runner.OSInfo) string {
	if osInfo != nil && osInfo.PkgManager != "" {
		return osInfo.PkgManager
	}
	return "yum" // 默认
}

// GetArch 获取 CPU 架构
func GetArch(osInfo *runner.OSInfo) string {
	if osInfo != nil && osInfo.Arch != "" {
		return osInfo.Arch
	}
	return "x86_64" // 默认
}

// GetOSName 获取操作系统名称
func GetOSName(osInfo *runner.OSInfo) string {
	if osInfo != nil && osInfo.Name != "" {
		return osInfo.Name
	}
	return "Unknown"
}

// GetOSVersion 获取操作系统版本
func GetOSVersion(osInfo *runner.OSInfo) string {
	if osInfo != nil && osInfo.Version != "" {
		return osInfo.Version
	}
	return ""
}

// GetTotalMemoryGB 获取系统总内存大小（单位：GB）
// 返回内存大小（GB）和错误信息
func GetTotalMemoryGB(ctx *runner.StepContext) (int, error) {
	// 使用 free 命令获取内存信息（单位：MB）
	result, err := ctx.Execute("free -m | grep '^Mem:' | awk '{print $2}'", false)
	if err != nil || result == nil || result.GetExitCode() != 0 {
		return 0, fmt.Errorf("failed to get memory info: %w", err)
	}

	memMB := strings.TrimSpace(result.GetStdout())
	if memMB == "" {
		return 0, fmt.Errorf("empty memory output")
	}

	memMBInt, err := strconv.Atoi(memMB)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory size '%s': %w", memMB, err)
	}

	// 转换为 GB（向下取整）
	memGB := memMBInt / 1024
	return memGB, nil
}
