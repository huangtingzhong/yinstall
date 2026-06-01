package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
)

const (
	YACAccessModeVIP    = "vip"
	YACAccessModeScan   = "scan"
	YACAccessModeDirect = "direct"
)

// ValidateYACAccessMode 校验 --yac-access-mode 取值。
func ValidateYACAccessMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case YACAccessModeVIP, YACAccessModeScan, YACAccessModeDirect:
		return nil
	default:
		return fmt.Errorf("invalid --yac-access-mode: %q (valid: vip, scan, direct)", mode)
	}
}

// NormalizeYACAccessMode 返回小写规范化的访问模式，未知值原样返回。
func NormalizeYACAccessMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

// YACAccessModeRequiresVIP 为 true 时须执行 VIP 校验/自动生成（vip、scan）；direct 为 false。
func YACAccessModeRequiresVIP(mode string) bool {
	switch NormalizeYACAccessMode(mode) {
	case YACAccessModeVIP, YACAccessModeScan:
		return true
	default:
		return false
	}
}

// YACCeGenParams 组装 yasboot package ce gen 命令所需参数（供 C-014 使用）。
type YACCeGenParams struct {
	StageDir, YasbootPath, ClusterName, User, Password, IPs string
	SSHPort, BeginPort, NodeCount                           int
	InstallPath, DataPath, LogPath                          string
	InterCIDR, PublicNetwork                                string
	AccessMode                                              string
	ScanName                                                string
	VIPs                                                    []string
	DiskFoundPath, SystemDisks, DataDisks                   string
}

// BuildYACCeGenCommand 生成 package ce gen 命令（不含 yasboot_gen_extra_args）。
func BuildYACCeGenCommand(p YACCeGenParams) string {
	mode := NormalizeYACAccessMode(p.AccessMode)
	switch mode {
	case YACAccessModeScan:
		return fmt.Sprintf(`cd %s && %s package ce gen -c %s -f \
-u %s -p %s --ip %s --port %d \
-i %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--node %d \
--inter-cidr %s \
--public-network %s \
--scanname %s \
--disk-found-path %s \
--system-data %s \
--data %s`,
			p.StageDir, p.YasbootPath, p.ClusterName,
			p.User, commonos.ShellSingleQuote(p.Password), p.IPs, p.SSHPort,
			p.InstallPath, p.DataPath, p.LogPath,
			p.BeginPort, p.NodeCount,
			p.InterCIDR, p.PublicNetwork, p.ScanName,
			p.DiskFoundPath,
			p.SystemDisks, p.DataDisks)
	case YACAccessModeDirect:
		return fmt.Sprintf(`cd %s && %s package ce gen -c %s -f \
-u %s -p %s --ip %s --port %d \
-i %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--node %d \
--inter-cidr %s \
--public-network %s \
--disk-found-path %s \
--system-data %s \
--data %s`,
			p.StageDir, p.YasbootPath, p.ClusterName,
			p.User, commonos.ShellSingleQuote(p.Password), p.IPs, p.SSHPort,
			p.InstallPath, p.DataPath, p.LogPath,
			p.BeginPort, p.NodeCount,
			p.InterCIDR, p.PublicNetwork,
			p.DiskFoundPath,
			p.SystemDisks, p.DataDisks)
	default: // vip
		vipStr := formatVIPListForCeGen(p.VIPs, p.PublicNetwork, p.InterCIDR)
		return fmt.Sprintf(`cd %s && %s package ce gen -c %s -f \
-u %s -p %s --ip %s --port %d \
-i %s \
--data-path %s \
--log-path %s \
--begin-port %d \
--node %d \
--inter-cidr %s \
--public-network %s \
--vips %s \
--disk-found-path %s \
--system-data %s \
--data %s`,
			p.StageDir, p.YasbootPath, p.ClusterName,
			p.User, commonos.ShellSingleQuote(p.Password), p.IPs, p.SSHPort,
			p.InstallPath, p.DataPath, p.LogPath,
			p.BeginPort, p.NodeCount,
			p.InterCIDR, p.PublicNetwork, vipStr,
			p.DiskFoundPath,
			p.SystemDisks, p.DataDisks)
	}
}

func formatVIPListForCeGen(vips []string, publicNetwork, interCIDR string) string {
	vipNetmask := publicNetwork
	if vipNetmask == "" {
		vipNetmask = interCIDR
	}
	prefixLen := 24
	if vipNetmask != "" {
		if pl, err := commonos.CIDRPrefixLen(vipNetmask); err == nil {
			prefixLen = pl
		}
	}
	var vipParts []string
	for _, v := range vips {
		v = strings.TrimSpace(v)
		if v != "" {
			vipParts = append(vipParts, fmt.Sprintf("%s/%d", v, prefixLen))
		}
	}
	return strings.Join(vipParts, ",")
}
