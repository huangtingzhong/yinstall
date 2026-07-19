package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	hostsBeginMarker = "# BEGIN yinstall managed hosts"
	hostsEndMarker   = "# END yinstall managed hosts"
)

// NormalizeManagedHostsEntries 规范化托管 hosts 行：压缩空白为 "ip  name"，去掉空行。
func NormalizeManagedHostsEntries(entries []string) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		fields := strings.Fields(e)
		if len(fields) < 2 {
			out = append(out, e)
			continue
		}
		// IP + 第一个主机名（与写入格式 "ip  name" 对齐）
		out = append(out, fields[0]+"  "+fields[1])
	}
	return out
}

// ManagedHostsEntriesEqual 比较两组托管条目是否等价（规范化后按顺序）。
func ManagedHostsEntriesEqual(a, b []string) bool {
	na := NormalizeManagedHostsEntries(a)
	nb := NormalizeManagedHostsEntries(b)
	if len(na) != len(nb) {
		return false
	}
	for i := range na {
		if na[i] != nb[i] {
			return false
		}
	}
	return true
}

// EnsureManagedHostsBlock 若当前托管块与 entries 一致则跳过写入；否则替换整块。
// changed=true 表示实际改写了 /etc/hosts。
func EnsureManagedHostsBlock(ctx *runner.StepContext, entries []string) (changed bool, err error) {
	desired := NormalizeManagedHostsEntries(entries)
	if len(desired) == 0 {
		return false, nil
	}
	current := NormalizeManagedHostsEntries(ReadManagedHostsEntries(ctx))
	if ManagedHostsEntriesEqual(current, desired) {
		return false, nil
	}
	if err := UpdateManagedHostsBlock(ctx, desired); err != nil {
		return false, err
	}
	return true, nil
}

// UpdateManagedHostsBlock replaces the yinstall managed block in /etc/hosts
// with the given entries. If no block exists, it appends one.
// entries example: ["10.10.10.125  yashandb01", "10.10.10.126  yashandb02"]
func UpdateManagedHostsBlock(ctx *runner.StepContext, entries []string) error {
	entries = NormalizeManagedHostsEntries(entries)
	if len(entries) == 0 {
		return nil
	}

	block := hostsBeginMarker + "\\n"
	for _, e := range entries {
		block += e + "\\n"
	}
	block += hostsEndMarker

	removeCmd := fmt.Sprintf(
		`sed -i '/%s/,/%s/d' /etc/hosts`,
		escapeForSed(hostsBeginMarker),
		escapeForSed(hostsEndMarker),
	)

	appendCmd := fmt.Sprintf(`printf '%s\n' >> /etc/hosts`, block)

	fullCmd := removeCmd + " && " + appendCmd
	result, err := ctx.Execute(fullCmd, true)
	if err != nil {
		return fmt.Errorf("failed to update /etc/hosts managed block: %w", err)
	}
	if result != nil && result.GetExitCode() != 0 {
		return fmt.Errorf("failed to update /etc/hosts: %s", result.GetStderr())
	}
	return nil
}

// ReadManagedHostsEntries reads the current entries from the yinstall managed
// block in /etc/hosts. Returns empty slice if no block exists.
func ReadManagedHostsEntries(ctx *runner.StepContext) []string {
	cmd := fmt.Sprintf(
		`sed -n '/%s/,/%s/{/%s/d;/%s/d;p}' /etc/hosts`,
		escapeForSed(hostsBeginMarker),
		escapeForSed(hostsEndMarker),
		escapeForSed(hostsBeginMarker),
		escapeForSed(hostsEndMarker),
	)
	result, _ := ctx.Execute(cmd, true)
	if result == nil || result.GetStdout() == "" {
		return nil
	}
	var entries []string
	for _, line := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			entries = append(entries, line)
		}
	}
	return entries
}

func escapeForSed(s string) string {
	r := strings.NewReplacer(
		"/", "\\/",
		"#", "\\#",
	)
	return r.Replace(s)
}

// NormalizeTextContent 规范化文本以便比较（统一换行并 TrimSpace）。
func NormalizeTextContent(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

// TextContentEqual 比较两段文本在规范化后是否相等。
func TextContentEqual(a, b string) bool {
	return NormalizeTextContent(a) == NormalizeTextContent(b)
}
