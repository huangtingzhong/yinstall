package os

import (
	"fmt"
	"path"
	"strings"
	"unicode"
)

// isWindowsAbsPath reports whether p is a Windows drive path (e.g. D:/mysql/app).
func isWindowsAbsPath(p string) bool {
	if len(p) < 3 || p[1] != ':' || p[2] != '/' {
		return false
	}
	return unicode.IsLetter(rune(p[0]))
}

func validateWindowsDeletePath(raw string) error {
	if len(raw) == 3 { // D:/
		return fmt.Errorf("refusing to delete drive root")
	}
	rest := strings.TrimPrefix(raw, raw[:3])
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return fmt.Errorf("refusing to delete drive root")
	}
	parts := strings.Split(rest, "/")
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("invalid path segment in %q", raw)
		}
	}
	if len(parts) < 2 {
		return fmt.Errorf("path must be at least two levels deep (e.g. D:/mysql/app), got %q", raw)
	}
	return nil
}

// ValidateDeletePath 校验待删除路径是否适合用于字面量 rm/rm -rf（ShellSingleQuote 后执行）。
// 不做安装根目录白名单；删除范围由调用方在 rm 前 test -d/-f 确认目标存在，以及业务层路径约束（如必须在 db_data_path 下）保证。
//
// 拒绝：空路径、根目录、相对路径、含 ".." 、shell/通配元字符、仅单段路径（如 /data、/tmp，避免误删整盘挂载点）。
// Windows 绝对路径（如 D:/mysql/app/mysql）同样接受，至少两级目录深度。
func ValidateDeletePath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("empty path")
	}
	raw := strings.ReplaceAll(p, `\`, `/`)
	if strings.Contains(raw, "..") {
		return fmt.Errorf("path must not contain ..")
	}
	if strings.ContainsAny(raw, "*?[{}$`|;&<>() \t\n\\'\"!#~") {
		return fmt.Errorf("path must not contain shell or glob metacharacters")
	}
	if isWindowsAbsPath(raw) {
		return validateWindowsDeletePath(raw)
	}
	cleaned := path.Clean(raw)
	if cleaned == "/" || cleaned == "." || cleaned == "" {
		return fmt.Errorf("refusing to delete filesystem root")
	}
	if !strings.HasPrefix(cleaned, "/") {
		return fmt.Errorf("path must be absolute")
	}
	trim := strings.TrimPrefix(cleaned, "/")
	if trim == "" {
		return fmt.Errorf("invalid path")
	}
	parts := strings.Split(trim, "/")
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("invalid path segment in %q", cleaned)
		}
	}
	if len(parts) < 2 {
		return fmt.Errorf("path must be at least two levels deep (e.g. /data/yashan), got %q", cleaned)
	}
	return nil
}

// CleanDeletePath 校验并返回 path.Clean 后的绝对路径。
func CleanDeletePath(p string) (string, error) {
	if err := ValidateDeletePath(p); err != nil {
		return "", err
	}
	raw := strings.ReplaceAll(strings.TrimSpace(p), `\`, `/`)
	if isWindowsAbsPath(raw) {
		return raw, nil
	}
	return path.Clean(raw), nil
}

// DeletePathUnder 判断 child 是否等于 parent 或位于 parent 目录树内。
// 使用 parent+"/" 边界，避免 /data1 误匹配 /data12。
func DeletePathUnder(child, parent string) bool {
	childRaw := strings.ReplaceAll(strings.TrimSpace(child), `\`, `/`)
	parentRaw := strings.ReplaceAll(strings.TrimSpace(parent), `\`, `/`)
	if isWindowsAbsPath(childRaw) && isWindowsAbsPath(parentRaw) {
		if strings.EqualFold(childRaw, parentRaw) {
			return true
		}
		parentPrefix := parentRaw
		if !strings.HasSuffix(parentPrefix, "/") {
			parentPrefix += "/"
		}
		return strings.HasPrefix(strings.ToLower(childRaw), strings.ToLower(parentPrefix))
	}
	child = path.Clean(childRaw)
	parent = path.Clean(parentRaw)
	if child == "" || parent == "" || parent == "/" {
		return false
	}
	if child == parent {
		return true
	}
	parentPrefix := parent
	if !strings.HasSuffix(parentPrefix, "/") {
		parentPrefix += "/"
	}
	return strings.HasPrefix(child, parentPrefix)
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
