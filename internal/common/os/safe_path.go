package os

import (
	"path"
	"strings"
)

// unixRmRfRoots 允许执行 rm -rf / rm -f 的路径根（远程 Linux）。采用白名单，拒绝 /etc、/usr（除 /usr/local）、/bin 等系统目录，
// 避免配置错误或恶意参数导致扩大删除范围。
var unixRmRfRoots = []string{
	"/data",
	"/home",
	"/opt",
	"/root",
	"/tmp",
	"/var",
	"/usr/local",
	"/srv",
	"/mnt",
	"/media",
}

// IsSafeUnixRmRfPath 判断绝对路径是否允许用于 rm -rf（删除目录树）或 rm -f（删除文件）。
// 规则：必须为绝对路径、path.Clean 后非根目录、路径段不含 ".."、至少两级路径（如 /data/yashan），且必须落在 unixRmRfRoots 白名单下。
// 控制端可能是 Windows；路径语义为远端 Unix，故使用 path 而非 filepath。
func IsSafeUnixRmRfPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return false
	}
	p = strings.ReplaceAll(p, `\`, `/`)
	// 拒绝含 ".." 的原始路径；path.Clean 会静默消解，可能被用于绕过「看似在 /data/yashan 下」实则指向其他目录。
	if strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") || strings.HasPrefix(p, "../") {
		return false
	}
	cleaned := path.Clean(p)
	if cleaned == "/" || cleaned == "." || cleaned == "" {
		return false
	}
	if !strings.HasPrefix(cleaned, "/") {
		return false
	}
	trim := strings.TrimPrefix(cleaned, "/")
	if trim == "" {
		return false
	}
	parts := strings.Split(trim, "/")
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	if len(parts) < 2 {
		return false
	}
	for _, root := range unixRmRfRoots {
		if cleaned == root || strings.HasPrefix(cleaned, root+"/") {
			return true
		}
	}
	return false
}

// IsSafeUnixBlockDevicePath 判断路径是否可作为 dd 等操作的块设备路径（of=...）。
// 仅允许 /dev/ 下路径，禁止 ".." 与常见 shell 元字符，避免命令注入或误写非设备文件。
func IsSafeUnixBlockDevicePath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	p = strings.ReplaceAll(p, `\`, `/`)
	cleaned := path.Clean(p)
	if cleaned == "/dev" || cleaned == "/dev/" {
		return false
	}
	if !strings.HasPrefix(cleaned, "/dev/") {
		return false
	}
	if strings.Contains(cleaned, "..") {
		return false
	}
	// 禁止 shell 展开/注入与空白
	if strings.ContainsAny(cleaned, " \t\n$;|&`<>()*") {
		return false
	}
	return true
}
