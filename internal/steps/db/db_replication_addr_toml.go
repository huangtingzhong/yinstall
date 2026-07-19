// db_replication_addr_toml.go - 集群 TOML 写入 REPLICATION_ADDR
// 在 --db-replica-cidr 非空时于 gen 后补写 ":<port>"，与 host.yasdb_ip.replica_ip 配合

package db

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/yinstall/internal/runner"
)

var reReplicationAddrLine = regexp.MustCompile(`^\s*REPLICATION_ADDR\s*=`)

// EnsureReplicationAddrInTOMLContent 在每个 [group.node.config]（或 legacy [db]）写入 REPLICATION_ADDR = ":<port>"。
// 与官方多网段示例一致：端口写在参数值，IP 由 host.yasdb_ip.replica_ip 提供。
func EnsureReplicationAddrInTOMLContent(content string, port int) (string, error) {
	if port <= 0 || port > 65535 {
		return "", fmt.Errorf("invalid replication port %d", port)
	}
	value := fmt.Sprintf(`":%d"`, port)
	lines := normalizeTomlLines(content)
	stripped := stripReplicationAddrLines(lines)

	if containsGroupNodeConfigSection(stripped) {
		updated := insertReplicationAddrUnderEachGroupNodeConfig(stripped, value)
		if err := verifyReplicationAddrPerNodeConfigLines(updated); err != nil {
			return "", err
		}
		return strings.Join(updated, "\n"), nil
	}

	if !containsLegacyDBSection(stripped) {
		return "", fmt.Errorf("cluster toml has no [group.node.config] and no [db] section; cannot set REPLICATION_ADDR")
	}
	updated := insertOrReplaceKeyUnderLegacyDB(stripped, "REPLICATION_ADDR", value)
	return strings.Join(updated, "\n"), nil
}

func stripReplicationAddrLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if reReplicationAddrLine.MatchString(ln) {
			continue
		}
		out = append(out, ln)
	}
	return out
}

func insertReplicationAddrUnderEachGroupNodeConfig(lines []string, quotedValue string) []string {
	out := make([]string, 0, len(lines)+8)
	for i := 0; i < len(lines); {
		ln := lines[i]
		m := reGroupNodeConfigHeader.FindStringSubmatch(ln)
		if m == nil {
			out = append(out, ln)
			i++
			continue
		}
		headerSpaces := m[1]
		keyPrefix := headerSpaces + "  "
		out = append(out, ln)
		i++
		inserted := false
		j := i
		for j < len(lines) {
			nl := lines[j]
			ts := strings.TrimSpace(nl)
			if ts == "" {
				out = append(out, nl)
				j++
				continue
			}
			if strings.HasPrefix(ts, "#") {
				out = append(out, nl)
				j++
				continue
			}
			if reTableHeaderLine.MatchString(nl) {
				out = append(out, keyPrefix+fmt.Sprintf("REPLICATION_ADDR = %s", quotedValue))
				out = append(out, nl)
				j++
				inserted = true
				break
			}
			pref := leadingWhitespacePrefix(nl)
			out = append(out, pref+fmt.Sprintf("REPLICATION_ADDR = %s", quotedValue))
			out = append(out, nl)
			j++
			inserted = true
			break
		}
		if !inserted {
			out = append(out, keyPrefix+fmt.Sprintf("REPLICATION_ADDR = %s", quotedValue))
		}
		i = j
	}
	return out
}

func countReplicationAddrLines(lines []string) int {
	n := 0
	for _, ln := range lines {
		if reReplicationAddrLine.MatchString(ln) {
			n++
		}
	}
	return n
}

func verifyReplicationAddrPerNodeConfigLines(lines []string) error {
	s := countGroupNodeConfigHeaders(lines)
	k := countReplicationAddrLines(lines)
	if s == 0 || k != s {
		return fmt.Errorf("[group.node.config] sections=%d REPLICATION_ADDR lines=%d (must be equal and non-zero)", s, k)
	}
	return nil
}

// insertOrReplaceKeyUnderLegacyDB 在顶层 [db] 段写入 key = value（value 已含引号时原样写入）。
func insertOrReplaceKeyUnderLegacyDB(lines []string, key, quotedValue string) []string {
	reKey := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	out := make([]string, 0, len(lines)+1)
	inDB := false
	inserted := false
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if reLegacyDBSection.MatchString(ln) {
			inDB = true
			out = append(out, ln)
			continue
		}
		if inDB && reTableHeaderLine.MatchString(ln) && !reLegacyDBSection.MatchString(ln) {
			if !inserted {
				out = append(out, fmt.Sprintf("%s = %s", key, quotedValue))
				inserted = true
			}
			inDB = false
			out = append(out, ln)
			continue
		}
		if inDB && reKey.MatchString(ln) {
			pref := leadingWhitespacePrefix(ln)
			out = append(out, pref+fmt.Sprintf("%s = %s", key, quotedValue))
			inserted = true
			continue
		}
		out = append(out, ln)
	}
	if inDB && !inserted {
		out = append(out, fmt.Sprintf("%s = %s", key, quotedValue))
	}
	return out
}

// ReplicationAddrAlreadyCorrect 判断 toml 中 REPLICATION_ADDR 是否已全部为 ":<port>"（段数足够则无需再写）。
func ReplicationAddrAlreadyCorrect(content string, port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	want := fmt.Sprintf(":%d", port)
	lines := normalizeTomlLines(content)
	var vals []string
	for _, ln := range lines {
		if !reReplicationAddrLine.MatchString(ln) {
			continue
		}
		parts := strings.SplitN(ln, "=", 2)
		if len(parts) != 2 {
			return false
		}
		v := strings.TrimSpace(parts[1])
		v = strings.Trim(v, `"'`)
		vals = append(vals, v)
	}
	if len(vals) == 0 {
		return false
	}
	if containsGroupNodeConfigSection(lines) {
		if len(vals) != countGroupNodeConfigHeaders(lines) {
			return false
		}
	}
	for _, v := range vals {
		if v != want {
			return false
		}
	}
	return true
}

// ensureReplicationAddrInClusterTOML 在远程集群 TOML 中写入 REPLICATION_ADDR（供 --db-replica-cidr 非空时 gen 后调用）。
// 返回 changed=true 表示发生了写盘；已正确则 skip 写盘并返回 false。
func ensureReplicationAddrInClusterTOML(ctx *runner.StepContext, configPath string, port int) (bool, error) {
	content, err := readRemoteTextFile(ctx, configPath)
	if err != nil {
		return false, err
	}
	if ReplicationAddrAlreadyCorrect(content, port) {
		return false, nil
	}
	newContent, err := EnsureReplicationAddrInTOMLContent(content, port)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(content) == strings.TrimSpace(newContent) {
		return false, nil
	}
	if err := writeRemoteTextViaUpload(ctx, configPath, newContent); err != nil {
		return false, err
	}
	after, err := readRemoteTextFile(ctx, configPath)
	if err != nil {
		return false, err
	}
	lines := normalizeTomlLines(after)
	if containsGroupNodeConfigSection(lines) {
		if err := verifyReplicationAddrPerNodeConfigLines(lines); err != nil {
			return true, err
		}
		return true, nil
	}
	if countReplicationAddrLines(lines) < 1 {
		return true, fmt.Errorf("REPLICATION_ADDR missing after write to %s", configPath)
	}
	return true, nil
}
