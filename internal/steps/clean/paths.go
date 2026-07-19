package clean

import (
	"path"
	"regexp"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
)

// PathLiteralPrefixForPS 将远端目录规范为用于 ps 输出匹配的固定前缀：绝对路径 + path.Clean + 末尾 '/'
//
// 配合 grep -F 使用，避免：
// - /opt/ycm 匹配到 /opt/ycm2；
// - /data/yashan/yasdb_home 匹配到 yasdb_home_3988（另一实例）；
// - /data123 与 /data1234 等「前缀重叠」。
func PathLiteralPrefixForPS(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, `\`, `/`)
	p = path.Clean(p)
	if p == "/" || p == "." {
		return ""
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

var (
	reInstanceDataLeaf = regexp.MustCompile(`(?i)^db-\d+-\d+$`)
	reVersionLeaf      = regexp.MustCompile(`^\d+\.\d+`)
)

// PathMatchLiteralsForPS 返回供 grep -F 使用的路径字面量列表（已去重）。
// 在「尾 /」前缀之外，补充：
// - 父目录前缀（覆盖 -D .../yasdb_data/db-1-1 与 sourced 为实例子目录的情况）；
// - 实例叶子 / 日志叶子的无尾 / 形式（覆盖 cmdline 以空格结尾、无尾 /）。
// 不对裸 yasdb_home 去尾 /，以免误匹配 yasdb_home_<port>。
func PathMatchLiteralsForPS(p string) []string {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, `/`))
	if p == "" {
		return nil
	}
	clean := path.Clean(p)
	if clean == "/" || clean == "." {
		return nil
	}

	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	if pref := PathLiteralPrefixForPS(clean); pref != "" {
		add(pref)
	}

	base := path.Base(clean)
	// 仅当当前叶子是实例子目录/版本目录时补充父前缀，避免 .../yasdb_data 的父 /data/yashan/ 误伤同机其它实例
	if reInstanceDataLeaf.MatchString(base) || reVersionLeaf.MatchString(base) {
		parent := path.Dir(clean)
		if parent != "/" && parent != "." && parent != clean {
			if pref := PathLiteralPrefixForPS(parent); pref != "" {
				add(pref)
			}
		}
	}

	switch {
	case reInstanceDataLeaf.MatchString(base),
		strings.EqualFold(base, "log"),
		strings.HasPrefix(strings.ToLower(base), "log_"):
		add(clean)
	case reVersionLeaf.MatchString(base):
		// 版本目录：.../23.5.2.101/bin/yasdb 已由尾 / 前缀覆盖
	}

	return out
}

// PathsCompatibleLiterals 字面量路径是否兼容：相等，或 a 在 b 下，或 b 在 a 下。
func PathsCompatibleLiterals(a, b string) bool {
	a = path.Clean(strings.ReplaceAll(strings.TrimSpace(a), `\`, `/`))
	b = path.Clean(strings.ReplaceAll(strings.TrimSpace(b), `\`, `/`))
	if a == "" || b == "" {
		return false
	}
	return a == b || commonos.DeletePathUnder(a, b) || commonos.DeletePathUnder(b, a)
}
