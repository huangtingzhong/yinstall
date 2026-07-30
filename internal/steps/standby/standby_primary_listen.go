// primary_listen.go - 从主库 LISTEN_ADDR 推导备库扩容用的 db_begin_port

package standby

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// PortFromListenAddr 从 LISTEN_ADDR 参数值中解析 TCP 端口（如 10.10.10.130:3988、*:3988、[::1]:1688）。
func PortFromListenAddr(listen string) (int, error) {
	s := strings.TrimSpace(listen)
	if s == "" {
		return 0, fmt.Errorf("empty LISTEN_ADDR value")
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		s = s[1 : len(s)-1]
	}
	if host, portStr, err := net.SplitHostPort(s); err == nil {
		_ = host
		p, err := strconv.Atoi(portStr)
		if err != nil || p < 1 || p > 65535 {
			return 0, fmt.Errorf("invalid port %q", portStr)
		}
		return p, nil
	}
	// *:3988 等 net.SplitHostPort 无法解析的情况：取最后一个冒号后的端口
	idx := strings.LastIndex(s, ":")
	if idx < 0 || idx >= len(s)-1 {
		return 0, fmt.Errorf("no port in LISTEN_ADDR: %q", listen)
	}
	portStr := s[idx+1:]
	p, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("invalid port suffix in LISTEN_ADDR: %q", listen)
	}
	return p, nil
}

// parseListenAddrPortFromYasqlStdout 从 yasql 输出中提取 LISTEN_ADDR 一行并解析端口。
func parseListenAddrPortFromYasqlStdout(stdout string) (int, error) {
	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		if strings.HasPrefix(line, "-") {
			continue
		}
		if strings.Contains(low, "row fetched") || strings.Contains(low, "rows fetched") {
			continue
		}
		if strings.Contains(low, "value") && (strings.Contains(low, "name") || strings.Count(line, " ") > 5) {
			continue
		}
		// 两列表头 / 数据行：VALUE | 10.10.10.130:3988
		if strings.Contains(line, "|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 2 {
				val := strings.TrimSpace(parts[len(parts)-1])
				if p, err := PortFromListenAddr(val); err == nil {
					return p, nil
				}
			}
		}
		if p, err := PortFromListenAddr(line); err == nil {
			return p, nil
		}
	}
	return 0, fmt.Errorf("could not parse LISTEN_ADDR port from yasql output")
}

// FillBeginPortFromPrimaryListenAddr 在主库上执行 yasql 查询 v$parameter.LISTEN_ADDR，将端口写入 ctx.Params["db_begin_port"]。
// 需已能 SSH 到主库；会先 GetPrimaryEnvFile + SyncPrimaryClusterNameFromEnvFile（与 yasql 环境一致）。
func FillBeginPortFromPrimaryListenAddr(ctx *runner.StepContext) error {
	if ctx == nil {
		return fmt.Errorf("step context is required")
	}
	envFile, err := GetPrimaryEnvFile(ctx)
	if err != nil {
		return fmt.Errorf("primary env file: %w", err)
	}
	if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
		return err
	}
	osUser := GetPrimaryOSUser(ctx)
	clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
	sql := `SELECT value FROM v$parameter WHERE name = 'LISTEN_ADDR';`
	res, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, osUser, envFile, clusterName, sql, false)
	if err != nil {
		return fmt.Errorf("query LISTEN_ADDR: %w", err)
	}
	port, err := parseListenAddrPortFromYasqlStdout(res.Stdout)
	if err != nil {
		return fmt.Errorf("%w\nstdout:\n%s", err, res.Stdout)
	}
	if ctx.Params == nil {
		ctx.Params = make(map[string]interface{})
	}
	ctx.Params["db_begin_port"] = port
	return nil
}

// IPFromListenAddr 从 LISTEN_ADDR 参数值中解析主机 IPv4（如 10.10.10.214:1688 -> 10.10.10.214）。
// 用于在主库查其所属接口网段以推导 yac_inter_cidr。
func IPFromListenAddr(listen string) (string, error) {
	s := strings.TrimSpace(listen)
	if s == "" {
		return "", fmt.Errorf("empty LISTEN_ADDR value")
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		s = s[1 : len(s)-1]
	}
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		return "", fmt.Errorf("no host:port in LISTEN_ADDR: %q", listen)
	}
	if host == "" || host == "*" {
		return "", fmt.Errorf("no concrete host in LISTEN_ADDR: %q", listen)
	}
	if ip := net.ParseIP(host); ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("LISTEN_ADDR host is not IPv4: %q", listen)
	}
	return host, nil
}

// parseListenAddrIPFromYasqlStdout 从 yasql 两列输出中提取 LISTEN_ADDR 行并解析主机 IPv4。
func parseListenAddrIPFromYasqlStdout(stdout string) (string, error) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "row fetched") || strings.Contains(low, "rows fetched") {
			continue
		}
		if strings.Contains(low, "value") && (strings.Contains(low, "name") || strings.Count(line, " ") > 5) {
			continue
		}
		if strings.Contains(line, "|") {
			parts := strings.Split(line, "|")
			if len(parts) >= 2 {
				if ip, err := IPFromListenAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
					return ip, nil
				}
			}
		}
		if ip, err := IPFromListenAddr(line); err == nil {
			return ip, nil
		}
	}
	return "", fmt.Errorf("could not parse LISTEN_ADDR host IP from yasql output")
}

// primaryNetExecutor 适配 StepContext 的主库执行器为 commonos.NetExecutor，
// 供 commonos.GetInterfaceForIP 查 LISTEN_ADDR 对应接口的真实 CIDR。
// runner.ExecResult 已实现 GetExitCode/GetStdout，直接满足 commonos.NetExecResult，无需结果包装。
type primaryNetExecutor struct{ ctx *runner.StepContext }

func (a *primaryNetExecutor) Execute(cmd string, sudo bool) (commonos.NetExecResult, error) {
	r, err := a.ctx.Execute(cmd, sudo)
	if err != nil || r == nil {
		return nil, err
	}
	return r, nil
}

func (a *primaryNetExecutor) Host() string {
	if a.ctx == nil || a.ctx.Executor == nil {
		return ""
	}
	return a.ctx.Executor.Host()
}

// FillInterCIDRFromPrimaryListenAddr 在主库查询 v$parameter.LISTEN_ADDR，取其 IPv4，
// 再用 commonos.GetInterfaceForIP 取该 IP 所属接口的真实网段，写入 ctx.Params["yac_inter_cidr"]。
// 与 FillBeginPortFromPrimaryListenAddr 同一套路（yasql 取 LISTEN_ADDR），用于 standby CE 路径 inter-cidr 自动发现。
// 注意：LISTEN_ADDR 在业务/服务网，故推导出的是业务网 CIDR——单网环境等同内置网；
// 多网卡且内置网独立（如 inter_ip 在另一子网）时，应改用 primary toml inter_ip 源，否则 REPLICATION_ADDR 会落在业务网卡。
func FillInterCIDRFromPrimaryListenAddr(ctx *runner.StepContext) error {
	if ctx == nil {
		return fmt.Errorf("step context is required")
	}
	envFile, err := GetPrimaryEnvFile(ctx)
	if err != nil {
		return fmt.Errorf("primary env file: %w", err)
	}
	if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
		return err
	}
	osUser := GetPrimaryOSUser(ctx)
	clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
	sql := `SELECT value FROM v$parameter WHERE name = 'LISTEN_ADDR';`
	res, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, osUser, envFile, clusterName, sql, false)
	if err != nil {
		return fmt.Errorf("query LISTEN_ADDR: %w", err)
	}
	ip, err := parseListenAddrIPFromYasqlStdout(res.Stdout)
	if err != nil {
		return fmt.Errorf("%w\nstdout:\n%s", err, res.Stdout)
	}
	iface, err := commonos.GetInterfaceForIP(&primaryNetExecutor{ctx: ctx}, ip)
	if err != nil {
		return fmt.Errorf("find primary interface for %s: %w", ip, err)
	}
	if ctx.Params == nil {
		ctx.Params = make(map[string]interface{})
	}
	ctx.Params["yac_inter_cidr"] = iface.CIDR
	return nil
}
