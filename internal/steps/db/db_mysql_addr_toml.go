// db_mysql_addr_toml.go - 集群 TOML 中 mysql_addr 端口与 begin-port 对齐
// 单机 mysql 模式：端口 = begin+2；YAC（若将来支持）：begin+3（见 commonos.MysqlListenPort）。
// 写法对齐 db_replication_addr_toml.go：内容纠偏 + AlreadyCorrect + ensure*InClusterTOML。

package db

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

var reMysqlAddrLine = regexp.MustCompile(`^(\s*mysql_addr\s*=\s*")([^"]+)(")\s*$`)

// EnsureMysqlAddrPortInTOMLContent 将所有 mysql_addr = "host:port" 的 port 改为 wantPort，保留 host。
// 无 mysql_addr 行时返回错误（--mode mysql 的 gen 产物应含该字段）。
func EnsureMysqlAddrPortInTOMLContent(content string, wantPort int) (string, error) {
	if wantPort <= 0 || wantPort > 65535 {
		return "", fmt.Errorf("invalid mysql listen port %d", wantPort)
	}
	lines := normalizeTomlLines(content)
	out := make([]string, 0, len(lines))
	n := 0
	for _, ln := range lines {
		m := reMysqlAddrLine.FindStringSubmatch(ln)
		if m == nil {
			out = append(out, ln)
			continue
		}
		hostPort := m[2]
		host, _, ok := splitHostPortLoose(hostPort)
		if !ok {
			return "", fmt.Errorf("cannot parse mysql_addr value %q", hostPort)
		}
		out = append(out, m[1]+host+":"+strconv.Itoa(wantPort)+m[3])
		n++
	}
	if n == 0 {
		return "", fmt.Errorf("no mysql_addr lines in cluster toml (expected after --mode mysql gen)")
	}
	return strings.Join(out, "\n"), nil
}

// MysqlAddrPortsAlreadyCorrect 判断 toml 中全部 mysql_addr 端口是否已为 wantPort。
func MysqlAddrPortsAlreadyCorrect(content string, wantPort int) bool {
	if wantPort <= 0 || wantPort > 65535 {
		return false
	}
	n := 0
	for _, ln := range normalizeTomlLines(content) {
		m := reMysqlAddrLine.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		n++
		_, port, ok := splitHostPortLoose(m[2])
		if !ok || port != wantPort {
			return false
		}
	}
	return n > 0
}

// splitHostPortLoose 解析 "host:port"（IPv4/主机名）；IPv6 带方括号时取最后一个 ':'。
func splitHostPortLoose(s string) (host string, port int, ok bool) {
	s = strings.TrimSpace(s)
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", 0, false
	}
	host = s[:i]
	p, err := strconv.Atoi(s[i+1:])
	if err != nil || p <= 0 || p > 65535 {
		return "", 0, false
	}
	return host, p, true
}

// ensureMysqlAddrPortInClusterTOML gen 后纠偏远程集群 toml 的 mysql_addr 端口（对齐 ensureReplicationAddrInClusterTOML）。
// 返回 changed=true 表示发生了写盘；已正确则 skip 写盘并返回 false。
func ensureMysqlAddrPortInClusterTOML(ctx *runner.StepContext, configPath string, beginPort int, yac bool) (bool, error) {
	want := commonos.MysqlListenPort(beginPort, yac)
	dbLogPhase(ctx, "mysql-addr-start", fmt.Sprintf("path=%s want_port=%d begin=%d yac=%v", configPath, want, beginPort, yac))
	content, err := readRemoteTextFile(ctx, configPath)
	if err != nil {
		dbLogPhase(ctx, "mysql-addr-fail", runner.TruncateForLog(err.Error(), 120))
		return false, err
	}
	if MysqlAddrPortsAlreadyCorrect(content, want) {
		dbLogPhase(ctx, "mysql-addr-skip", fmt.Sprintf("already_correct port=%d", want))
		return false, nil
	}
	newContent, err := EnsureMysqlAddrPortInTOMLContent(content, want)
	if err != nil {
		dbLogPhase(ctx, "mysql-addr-fail", runner.TruncateForLog(err.Error(), 120))
		return false, err
	}
	if strings.TrimSpace(content) == strings.TrimSpace(newContent) {
		dbLogPhase(ctx, "mysql-addr-skip", "content_unchanged")
		return false, nil
	}
	if err := writeRemoteTextViaUpload(ctx, configPath, newContent); err != nil {
		dbLogPhase(ctx, "mysql-addr-fail", runner.TruncateForLog(err.Error(), 120))
		return false, err
	}
	after, err := readRemoteTextFile(ctx, configPath)
	if err != nil {
		dbLogPhase(ctx, "mysql-addr-fail", runner.TruncateForLog(err.Error(), 120))
		return false, err
	}
	if !MysqlAddrPortsAlreadyCorrect(after, want) {
		err := fmt.Errorf("mysql_addr port not %d after write to %s", want, configPath)
		dbLogPhase(ctx, "mysql-addr-fail", runner.TruncateForLog(err.Error(), 120))
		return true, err
	}
	dbLogPhase(ctx, "mysql-addr-done", fmt.Sprintf("rewritten port=%d", want))
	return true, nil
}
