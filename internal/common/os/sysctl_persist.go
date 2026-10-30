// sysctl_persist.go - sysctl 落盘：仅写 /etc/sysctl.conf 托管块；键存在过滤；清理旧 drop-in
// sysctl --system 会在 drop-in 之后再次加载 conf，故统一落 conf 末尾托管块以保证生效。

package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	// DefaultSysctlConfFile 统一落盘目标（托管块）
	DefaultSysctlConfFile = "/etc/sysctl.conf"

	// SysctlManagedBeginMarker / SysctlManagedEndMarker conf 托管块边界
	SysctlManagedBeginMarker = "# BEGIN yinstall managed sysctl"
	SysctlManagedEndMarker   = "# END yinstall managed sysctl"

	// 历史 drop-in 路径（仅迁移删除，不再写入）
	legacySysctlDropInFile  = "/etc/sysctl.d/yashandb.conf"
	currentSysctlDropInFile = "/etc/sysctl.d/99-yashandb-sysctl.conf"
)

// SysctlKV 有序的 sysctl 键值。
type SysctlKV struct {
	Key   string
	Value string
}

// SysctlKeyToProcPath 将 kernel.shmmax 转为 /proc/sys/kernel/shmmax。
func SysctlKeyToProcPath(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
}

// YashanSysctlDefaults 返回当前产品推荐的有序键值（含 shm 三元组）。
func YashanSysctlDefaults(shmmax, shmall, shmmni int64) []SysctlKV {
	return []SysctlKV{
		{"vm.swappiness", "0"},
		{"vm.oom-kill", "0"},
		{"vm.zone_reclaim_mode", "0"},
		{"kernel.numa_balancing", "0"},
		{"net.ipv4.ip_local_port_range", "32768 60999"},
		{"vm.max_map_count", "2000000"},
		{"net.core.somaxconn", "32768"},
		{"kernel.shmall", fmt.Sprintf("%d", shmall)},
		{"kernel.shmmni", fmt.Sprintf("%d", shmmni)},
		{"kernel.shmmax", fmt.Sprintf("%d", shmmax)},
		{"fs.aio-max-nr", "6194304"},
		{"vm.dirty_ratio", "20"},
		{"vm.dirty_background_ratio", "3"},
		{"vm.dirty_writeback_centisecs", "100"},
		{"vm.dirty_expire_centisecs", "500"},
		{"vm.min_free_kbytes", "524288"},
		{"net.core.netdev_max_backlog", "30000"},
		{"net.core.netdev_budget", "600"},
	}
}

func formatSysctlConfBlockBody(kvs []SysctlKV) string {
	var b strings.Builder
	for _, kv := range kvs {
		if strings.TrimSpace(kv.Key) == "" {
			continue
		}
		b.WriteString(kv.Key)
		b.WriteString(" = ")
		b.WriteString(kv.Value)
		b.WriteByte('\n')
	}
	return b.String()
}

// ReplaceManagedBlock 替换或追加托管块；body 为块内文本（可无尾换行）。
func ReplaceManagedBlock(content, beginMarker, endMarker, body string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	body = strings.TrimRight(body, "\n")
	block := beginMarker + "\n"
	if body != "" {
		block += body + "\n"
	}
	block += endMarker

	start := strings.Index(content, beginMarker)
	end := strings.Index(content, endMarker)
	if start >= 0 && end > start {
		end = end + len(endMarker)
		for end < len(content) && (content[end] == '\n' || content[end] == '\r') {
			end++
		}
		left := strings.TrimRight(content[:start], "\n")
		right := strings.TrimLeft(content[end:], "\n")
		out := left
		if out != "" {
			out += "\n"
		}
		out += block
		if right != "" {
			out += "\n" + right
		}
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		return out
	}
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return block + "\n"
	}
	return trimmed + "\n" + block + "\n"
}

// FilterSysctlKeysByProc 仅保留 /proc/sys 下存在的键；返回保留列表与跳过的 key。
func FilterSysctlKeysByProc(exists func(procPath string) bool, kvs []SysctlKV) (kept []SysctlKV, skipped []string) {
	kept = make([]SysctlKV, 0, len(kvs))
	for _, kv := range kvs {
		path := SysctlKeyToProcPath(kv.Key)
		if path == "" || !exists(path) {
			skipped = append(skipped, kv.Key)
			continue
		}
		kept = append(kept, kv)
	}
	return kept, skipped
}

func sysctlProcPathExists(ctx *runner.StepContext, procPath string) bool {
	if strings.TrimSpace(procPath) == "" {
		return false
	}
	r, _ := ctx.Execute(fmt.Sprintf("test -e %s && echo yes", ShellSingleQuote(procPath)), false)
	return r != nil && strings.Contains(r.GetStdout(), "yes")
}

// FilterYashanSysctlKeysOnHost 过滤本机不存在的键。
func FilterYashanSysctlKeysOnHost(ctx *runner.StepContext, kvs []SysctlKV) (kept []SysctlKV, skipped []string) {
	return FilterSysctlKeysByProc(func(p string) bool {
		return sysctlProcPathExists(ctx, p)
	}, kvs)
}

// EnsureManagedSysctlConfBlock 在 sysctl.conf 写入/更新 yinstall 托管块。
func EnsureManagedSysctlConfBlock(ctx *runner.StepContext, confPath string, kvs []SysctlKV) (changed bool, err error) {
	if confPath == "" {
		confPath = DefaultSysctlConfFile
	}
	body := formatSysctlConfBlockBody(kvs)
	raw, _ := ctx.Execute(fmt.Sprintf("cat %s 2>/dev/null || true", ShellSingleQuote(confPath)), false)
	cur := ""
	if raw != nil {
		cur = raw.GetStdout()
	}
	next := ReplaceManagedBlock(cur, SysctlManagedBeginMarker, SysctlManagedEndMarker, body)
	if TextContentEqual(cur, next) {
		return false, nil
	}
	cmd := fmt.Sprintf("cat > %s << 'EOF'\n%sEOF", confPath, next)
	if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
		return false, fmt.Errorf("write managed sysctl conf block: %w", err)
	}
	return true, nil
}

// RemoveYashanSysctlDropIns 删除历史 yashan sysctl drop-in（升级后不再保留双轨文件）。
func RemoveYashanSysctlDropIns(ctx *runner.StepContext) {
	for _, p := range []string{legacySysctlDropInFile, currentSysctlDropInFile} {
		_, _ = ctx.Execute(fmt.Sprintf("rm -f %s", ShellSingleQuote(p)), true)
	}
}
