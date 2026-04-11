package os

import (
	"fmt"
	"regexp"
	"strings"
)

// ClusterArgBoundaryGrepE 返回用于 grep -E 的表达式：匹配命令行中**精确**的
// `-c CLUSTER`、`--cluster CLUSTER` 或 `--cluster=CLUSTER`（CLUSTER 后须为空白或行尾），
// 避免 CLUSTER 作为前缀匹配到 CLUSTER_2788 等更长集群名。
func ClusterArgBoundaryGrepE(clusterName string) string {
	c := strings.TrimSpace(clusterName)
	if c == "" {
		// 空集群名：匹配一个不可能出现在 ps 输出中的字面串
		return regexp.QuoteMeta("__YINSTALL_EMPTY_CLUSTER__")
	}
	esc := regexp.QuoteMeta(c)
	return fmt.Sprintf(
		`((^|[[:space:]])-c[[:space:]]+%s([[:space:]]|$))|((^|[[:space:]])--cluster[[:space:]]+%s([[:space:]]|$))|((^|[[:space:]])--cluster=%s([[:space:]]|$))`,
		esc, esc, esc,
	)
}

// PgrepBinaryClusterArgPattern 返回传给 pgrep -f 的扩展正则：匹配 binary 且其后命令行含精确 `-c CLUSTER`。
func PgrepBinaryClusterArgPattern(binary, clusterName string) string {
	b := strings.TrimSpace(binary)
	if b == "" {
		b = "yasom"
	}
	c := strings.TrimSpace(clusterName)
	var esc string
	if c == "" {
		esc = regexp.QuoteMeta("__YINSTALL_EMPTY_CLUSTER__")
	} else {
		esc = regexp.QuoteMeta(c)
	}
	return fmt.Sprintf(
		`%s.*-c[[:space:]]+%s([[:space:]]|$)`,
		regexp.QuoteMeta(b),
		esc,
	)
}
