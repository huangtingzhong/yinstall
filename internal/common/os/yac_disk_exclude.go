package os

import (
	"path"
	"strings"
)

// ParseYACExcludeDisks 解析 --yac-exclude-disks（逗号分隔）。
func ParseYACExcludeDisks(spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, normalizeDiskPath(part))
		}
	}
	return out
}

func normalizeDiskPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, `/`))
	return path.Clean(p)
}

// IsDiskPathExcluded 判断磁盘路径是否应排除（支持精确路径、/dev 下 basename、裸别名如 data2）。
func IsDiskPathExcluded(diskPath string, excludes []string) bool {
	if len(excludes) == 0 {
		return false
	}
	diskPath = normalizeDiskPath(diskPath)
	if diskPath == "" {
		return false
	}
	diskBase := path.Base(diskPath)
	for _, ex := range excludes {
		ex = normalizeDiskPath(ex)
		if ex == "" {
			continue
		}
		if diskPath == ex {
			return true
		}
		exBase := path.Base(ex)
		// 裸别名：data2、sys1（无路径分隔）
		if !strings.Contains(ex, "/") {
			if diskBase == ex {
				return true
			}
			continue
		}
		// 完整路径：再比 basename（/dev/yfs/data2 与 data2 等等价）
		if diskBase != "" && diskBase == exBase {
			if strings.HasPrefix(diskPath, "/dev/") && strings.HasPrefix(ex, "/dev/") {
				return true
			}
		}
	}
	return false
}

// FilterDiskPaths 返回未在排除列表中的路径（保持原顺序）。
func FilterDiskPaths(paths []string, excludes []string) []string {
	if len(excludes) == 0 {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if !IsDiskPathExcluded(p, excludes) {
			out = append(out, p)
		}
	}
	return out
}
