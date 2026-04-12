// primary_listen.go - 从主库 LISTEN_ADDR 推导备库扩容用的 db_begin_port

package standby

import (
	"fmt"
	"net"
	"strconv"
	"strings"

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
