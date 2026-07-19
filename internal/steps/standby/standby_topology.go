// standby_topology.go - OM / primary 拓扑发现
// 从 om_addr 与 cluster status 解析 OM 主机与当前主库 IP, 供 standby/clean 复用.

package standby

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	dbsteps "github.com/yinstall/internal/steps/db"
)

var (
	reOmAddrEQ  = regexp.MustCompile(`(?m)^\s*om_addr\s*=\s*"([^"]+)"\s*$`)
	reOmAddrEQ2 = regexp.MustCompile(`(?m)^\s*om_addr\s*=\s*'([^']+)'\s*$`)
	reOmAddrEQ3 = regexp.MustCompile(`(?m)^\s*om_addr\s*=\s*(\S+)\s*$`)
)

// ParseOmAddrHost 从 om_addr 值(如 10.10.10.130:1675) 解析主机部分.
func ParseOmAddrHost(omAddr string) (string, error) {
	omAddr = strings.TrimSpace(omAddr)
	omAddr = strings.Trim(omAddr, `"'`)
	if omAddr == "" {
		return "", fmt.Errorf("empty om_addr")
	}
	host, _, err := net.SplitHostPort(omAddr)
	if err != nil {
		// 无端口时整段当作 host
		if !strings.Contains(omAddr, ":") {
			return omAddr, nil
		}
		return "", fmt.Errorf("invalid om_addr %q: %w", omAddr, err)
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("empty host in om_addr %q", omAddr)
	}
	return host, nil
}

// OmHostFromEnvFileContent 从 .yasboot/*.env 或类似文本中解析 om_addr 主机.
// 多行时取最后一次出现(通常为当前集群).
func OmHostFromEnvFileContent(content string) (string, error) {
	var last string
	for _, re := range []*regexp.Regexp{reOmAddrEQ, reOmAddrEQ2, reOmAddrEQ3} {
		matches := re.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			h, err := ParseOmAddrHost(m[1])
			if err != nil {
				continue
			}
			last = h
		}
	}
	if last == "" {
		return "", fmt.Errorf("om_addr not found in env content")
	}
	return last, nil
}

// ListenIPFromAddress 从 listen_address(如 10.10.10.130:1688) 取 IP.
func ListenIPFromAddress(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" || listen == "-" {
		return ""
	}
	if i := strings.Index(listen, ":"); i > 0 {
		return listen[:i]
	}
	return listen
}

// PrimaryIPFromClusterStatus 从 yasboot cluster status -d 输出中取 role=primary 的 listen IP.
func PrimaryIPFromClusterStatus(statusOut string) string {
	rows := dbsteps.ParseClusterStatusTable(statusOut)
	for _, r := range rows {
		if !strings.EqualFold(strings.TrimSpace(r.DatabaseRole), "primary") {
			continue
		}
		if ip := ListenIPFromAddress(r.ListenAddress); ip != "" {
			return ip
		}
	}
	return ""
}

// SameHostIP 粗比较两主机是否同一目标(字面量, 忽略大小写).
func SameHostIP(a, b string) bool {
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	if a == "" || b == "" {
		return false
	}
	return a == b
}
