package db

import (
	"fmt"
	"net"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
)

const (
	YACAccessModeVIP    = "vip"
	YACAccessModeScan   = "scan"
	YACAccessModeDirect = "direct"

	// DefaultYACDiskFoundPath yasboot --disk-found-path 默认（db/standby 共用语义；CLI 默认可带尾斜杠）。
	DefaultYACDiskFoundPath = "/dev/yfs"

	// DefaultSysPassword DB SYS 默认密码：db 安装 --db-sys-password flag 默认值、PDB admin 默认值、
	// 以及 standby CE 路径未显式传入 --db-admin-password 时的回退值，统一在此常量去重。
	DefaultSysPassword = "Yashan1!"
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
	ReplicaCIDR                                             string // 对应 --db-replica-cidr；空则不传 yasboot --replica-cidr
	AccessMode                                              string
	ScanName                                                string
	VIPs                                                    []string
	DiskFoundPath, SystemDisks, DataDisks                   string
}

// ReplicaPort 返回主备复制监听端口：单机 beginPort+1，YAC beginPort+2（与官方网络准备一致）。
func ReplicaPort(beginPort int, yac bool) int {
	if yac {
		return beginPort + 2
	}
	return beginPort + 1
}

// AppendReplicaCIDRFlag 非空时在 gen 命令末尾追加 --replica-cidr（se/ce 共用）。
func AppendReplicaCIDRFlag(genCmd, replicaCIDR string) string {
	replicaCIDR = strings.TrimSpace(replicaCIDR)
	if replicaCIDR == "" {
		return genCmd
	}
	return genCmd + " \\\n--replica-cidr " + replicaCIDR
}

// BuildYACCeGenCommand 生成 package ce gen 命令（不含 yasboot_gen_extra_args）。
func BuildYACCeGenCommand(p YACCeGenParams) string {
	mode := NormalizeYACAccessMode(p.AccessMode)
	var cmd string
	switch mode {
	case YACAccessModeScan:
		cmd = fmt.Sprintf(`cd %s && %s package ce gen -c %s -f \
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
		cmd = fmt.Sprintf(`cd %s && %s package ce gen -c %s -f \
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
		vipStr := FormatVIPListForCeGen(p.VIPs, p.PublicNetwork, p.InterCIDR)
		cmd = fmt.Sprintf(`cd %s && %s package ce gen -c %s -f \
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
	return AppendReplicaCIDRFlag(cmd, p.ReplicaCIDR)
}

// NormalizeYACVIPHost 去掉 VIP 上的 /prefix（如 10.0.0.1/24 -> 10.0.0.1），供校验与 gen 共用。
func NormalizeYACVIPHost(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if host, _, err := net.ParseCIDR(v); err == nil && host != nil {
		if ip4 := host.To4(); ip4 != nil {
			return ip4.String()
		}
		return host.String()
	}
	if i := strings.Index(v, "/"); i > 0 {
		return strings.TrimSpace(v[:i])
	}
	return v
}

// FormatVIPListForCeGen 将 VIP 格式化为 yasboot --vips 所需的 ip/prefix 列表。
// 会先 Normalize 剥掉已有后缀，再按 publicNetwork（否则 interCIDR）前缀长度补全；默认 /24。
func FormatVIPListForCeGen(vips []string, publicNetwork, interCIDR string) string {
	vipNetmask := strings.TrimSpace(publicNetwork)
	if vipNetmask == "" {
		vipNetmask = strings.TrimSpace(interCIDR)
	}
	prefixLen := 24
	if vipNetmask != "" {
		if pl, err := commonos.CIDRPrefixLen(vipNetmask); err == nil {
			prefixLen = pl
		}
	}
	var vipParts []string
	for _, v := range vips {
		host := NormalizeYACVIPHost(v)
		if host != "" {
			vipParts = append(vipParts, fmt.Sprintf("%s/%d", host, prefixLen))
		}
	}
	return strings.Join(vipParts, ",")
}

// ValidateYACVIPList 校验 VIP：非空、数量=nodeCount、每项为合法 IPv4（允许输入带 /prefix）。
// 不含 ping / 自动生成（装库见 RunVIPValidationOrAutoGenerate；standby CE 强制手填）。
func ValidateYACVIPList(vips []string, nodeCount int) error {
	if nodeCount < 1 {
		return fmt.Errorf("node count must be >= 1")
	}
	if len(vips) == 0 {
		return fmt.Errorf("--yac-vips is required")
	}
	if len(vips) != nodeCount {
		return fmt.Errorf("--yac-vips count (%d) must equal node count (%d)", len(vips), nodeCount)
	}
	for i, v := range vips {
		host := NormalizeYACVIPHost(v)
		if host == "" {
			return fmt.Errorf("VIP at index %d is empty", i)
		}
		if !commonos.IsValidIPv4(host) {
			return fmt.Errorf("VIP %q is not a valid IPv4 address", v)
		}
	}
	return nil
}
