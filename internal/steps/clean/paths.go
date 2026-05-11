package clean

import (
	"path"
	"strings"
)

// PathLiteralPrefixForPS 将远端目录规范为用于 ps 输出匹配的固定前缀：绝对路径 + path.Clean + 末尾 '/'
//
// 配合 grep -F 使用，避免：
// - /opt/ycm 匹配到 /opt/ycm2；
// - /data/yashan/yasdb_home 匹配到 yasdb_home_3988（另一实例）；
// - /data123 与 /data1234 等「前缀重叠」：Clean 后分段匹配，且目录路径须满足 commonos.IsSafeUnixRmRfPath 白名单规则。
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
