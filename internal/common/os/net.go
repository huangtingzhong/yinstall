package os

import (
	"fmt"
	"net"
	"strings"
)

// PingExecResult 供 PingFromHost 使用的执行结果接口（仅需 GetExitCode）
type PingExecResult interface {
	GetExitCode() int
}

// PingExecutor 供 PingFromHost 使用的执行器接口；实现方需能在目标主机上执行命令
type PingExecutor interface {
	Execute(cmd string, sudo bool) (PingExecResult, error)
}

// IsValidIPv4 判断字符串是否为合法 IPv4 地址
func IsValidIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}

// NextIPv4 返回 s 的下一个 IPv4 地址；若溢出或非法则返回 "", false
func NextIPv4(s string) (string, bool) {
	ip := net.ParseIP(s)
	if ip == nil {
		return "", false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return "", false
	}
	n := uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
	n++
	if n == 0 {
		return "", false // 255.255.255.255 + 1 overflow
	}
	ip4[0] = byte(n >> 24)
	ip4[1] = byte(n >> 16)
	ip4[2] = byte(n >> 8)
	ip4[3] = byte(n)
	return net.IP(ip4).String(), true
}

// PingFromHost 从指定执行器所在主机 ping 目标 IP；返回 true 表示有响应（已被占用）
func PingFromHost(exec PingExecutor, ip string) (bool, error) {
	cmd := fmt.Sprintf("ping -c 1 -W 2 %s 2>/dev/null", ip)
	result, err := exec.Execute(cmd, false)
	if err != nil {
		return false, err
	}
	return result != nil && result.GetExitCode() == 0, nil
}

// ResolveHostnameToIP 解析主机名/域名为 IPv4 地址列表；解析失败或无 IPv4 返回错误
func ResolveHostnameToIP(host string) ([]string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("host is empty")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	var out []string
	for _, ip := range ips {
		if ip != nil && ip.To4() != nil {
			out = append(out, ip.To4().String())
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("resolve %s: no IPv4 address found", host)
	}
	return out, nil
}

// CIDRPrefixLen 从 CIDR 字符串解析出前缀长度，如 "10.10.10.0/24" -> 24
func CIDRPrefixLen(cidr string) (int, error) {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return 0, fmt.Errorf("cidr is empty")
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return 0, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}
	ones, _ := ipNet.Mask.Size()
	return ones, nil
}

// IPInSubnet 判断 IP 是否在给定 CIDR 网段内
func IPInSubnet(ipStr, cidr string) (bool, error) {
	ipStr = strings.TrimSpace(ipStr)
	cidr = strings.TrimSpace(cidr)
	if ipStr == "" || cidr == "" {
		return false, fmt.Errorf("ip or cidr is empty")
	}
	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To4() == nil {
		return false, fmt.Errorf("invalid IPv4: %s", ipStr)
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}
	return ipNet.Contains(ip), nil
}

// NetExecResult extends PingExecResult with stdout access
type NetExecResult interface {
	GetExitCode() int
	GetStdout() string
}

// NetExecutor extends PingExecutor with stdout-capable results
type NetExecutor interface {
	Execute(cmd string, sudo bool) (NetExecResult, error)
	Host() string
}

// InterfaceInfo describes a network interface on a remote host
type InterfaceInfo struct {
	Name string // e.g. eth1
	IP   string // e.g. 192.168.1.10
	CIDR string // network CIDR, e.g. 192.168.1.0/24
}

// GetHostInterfaces returns all global-scope IPv4 interfaces on a host,
// excluding loopback and the interface that carries excludeIP.
func GetHostInterfaces(exec NetExecutor, excludeIP string) ([]InterfaceInfo, error) {
	result, err := exec.Execute("ip -o -4 addr show scope global", false)
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %w", err)
	}
	if result == nil || result.GetExitCode() != 0 {
		return nil, fmt.Errorf("ip addr show failed (exit %d)", result.GetExitCode())
	}

	var excludeIface string
	var ifaces []InterfaceInfo

	for _, line := range strings.Split(result.GetStdout(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "2: eth0    inet 10.10.10.125/24 brd 10.10.10.255 scope global eth0"
		info, ok := parseIPAddrLine(line)
		if !ok {
			continue
		}
		if info.IP == excludeIP {
			excludeIface = info.Name
		}
		ifaces = append(ifaces, info)
	}

	if excludeIface == "" {
		return ifaces, nil
	}

	var filtered []InterfaceInfo
	for _, info := range ifaces {
		if info.Name != excludeIface {
			filtered = append(filtered, info)
		}
	}
	return filtered, nil
}

// GetInterfaceForIP returns the interface info for a specific IP on the host.
func GetInterfaceForIP(exec NetExecutor, targetIP string) (*InterfaceInfo, error) {
	result, err := exec.Execute("ip -o -4 addr show scope global", false)
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %w", err)
	}
	if result == nil || result.GetExitCode() != 0 {
		return nil, fmt.Errorf("ip addr show failed")
	}

	for _, line := range strings.Split(result.GetStdout(), "\n") {
		info, ok := parseIPAddrLine(strings.TrimSpace(line))
		if !ok {
			continue
		}
		if info.IP == targetIP {
			return &info, nil
		}
	}
	return nil, fmt.Errorf("no interface found for IP %s", targetIP)
}

// parseIPAddrLine parses one line of "ip -o -4 addr show scope global" output.
// Example: "2: eth0    inet 10.10.10.125/24 brd 10.10.10.255 scope global eth0"
func parseIPAddrLine(line string) (InterfaceInfo, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return InterfaceInfo{}, false
	}

	ifaceName := strings.TrimSuffix(fields[1], ":")

	inetIdx := -1
	for i, f := range fields {
		if f == "inet" {
			inetIdx = i
			break
		}
	}
	if inetIdx < 0 || inetIdx+1 >= len(fields) {
		return InterfaceInfo{}, false
	}

	ipCIDR := fields[inetIdx+1] // e.g. "10.10.10.125/24"
	parts := strings.SplitN(ipCIDR, "/", 2)
	if len(parts) != 2 {
		return InterfaceInfo{}, false
	}
	ipStr := parts[0]
	prefixStr := parts[1]

	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To4() == nil {
		return InterfaceInfo{}, false
	}

	var prefixLen int
	if _, err := fmt.Sscanf(prefixStr, "%d", &prefixLen); err != nil || prefixLen <= 0 || prefixLen > 32 {
		return InterfaceInfo{}, false
	}

	networkCIDR, err := CIDRFromIP(ipStr, prefixLen)
	if err != nil {
		return InterfaceInfo{}, false
	}

	return InterfaceInfo{
		Name: ifaceName,
		IP:   ipStr,
		CIDR: networkCIDR,
	}, true
}

// CIDRFromIP 根据 IP 和前缀长度生成 CIDR（如 10.10.10.1 + 24 -> 10.10.10.0/24）
func CIDRFromIP(ipStr string, prefixLen int) (string, error) {
	ipStr = strings.TrimSpace(ipStr)
	if ipStr == "" {
		return "", fmt.Errorf("ip is empty")
	}
	if prefixLen <= 0 || prefixLen > 32 {
		return "", fmt.Errorf("invalid prefix length: %d", prefixLen)
	}
	ip := net.ParseIP(ipStr)
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("invalid IPv4: %s", ipStr)
	}
	ip4 := ip.To4()
	mask := net.CIDRMask(prefixLen, 32)
	network := make(net.IP, 4)
	for i := range ip4 {
		network[i] = ip4[i] & mask[i]
	}
	return fmt.Sprintf("%s/%d", network.String(), prefixLen), nil
}

// 防火墙模式（--os-firewall-mode）
const (
	FirewallModeEnable  = "enable"
	FirewallModeDisable = "disable"
)

// NormalizeFirewallMode 规范化防火墙模式：enable/disable；空与 keep/open-ports 映射为 enable。
func NormalizeFirewallMode(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", FirewallModeEnable, "keep", "open-ports":
		return FirewallModeEnable, nil
	case FirewallModeDisable:
		return FirewallModeDisable, nil
	default:
		return "", fmt.Errorf("invalid firewall mode %q (use enable or disable)", s)
	}
}

// YasomListenPort 由 DB begin-port 推导 yasom 监听端口 (begin-13)。
func YasomListenPort(beginPort int) int {
	if beginPort <= 13 {
		return 0
	}
	return beginPort - 13
}

// YasomAgentListenPort 由 begin-port 推导 yasagent 端口 (begin-12)。
func YasomAgentListenPort(beginPort int) int {
	if beginPort <= 12 {
		return 0
	}
	return beginPort - 12
}

// ReplicaPort 返回主备复制监听端口：单机 beginPort+1，YAC beginPort+2（与官方网络准备一致）。
func ReplicaPort(beginPort int, yac bool) int {
	if yac {
		return beginPort + 2
	}
	return beginPort + 1
}

// YashanFirewallTCPPorts 返回 enable 模式下自动放行的 TCP 端口集（begin/yasom/yasagent/复制口）。
func YashanFirewallTCPPorts(beginPort int, yac bool) []int {
	seen := make(map[int]struct{}, 4)
	var out []int
	add := func(p int) {
		if p <= 0 {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	add(beginPort)
	add(YasomListenPort(beginPort))
	add(YasomAgentListenPort(beginPort))
	add(ReplicaPort(beginPort, yac))
	return out
}

// TrustedSourcesContain 判断 firewalld trusted --list-sources 输出是否含指定 CIDR（字段精确匹配）。
func TrustedSourcesContain(listStdout, cidr string) bool {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return false
	}
	for _, s := range strings.Fields(strings.TrimSpace(listStdout)) {
		if s == cidr {
			return true
		}
	}
	return false
}
