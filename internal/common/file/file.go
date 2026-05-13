// 说明：本包内 FindLatest*、findLatestInstantclient*、FindZstdSourceTarball 的「远端+本地存在目录合并、再按版本号选全局最新」
// 仅适用于 DB / YCM / YMP 安装包、instantclient、zstd 源码等可版本排序的介质。
//
// ISO（local-iso / YUM 源盘）的查找与挂载仍走 internal/common/os/iso.go（resolveISOPath / autoFindISO）：
// 保持「按目录顺序命中第一个 *.iso」策略，不参与上述合并选最新，也不做 ISO 版本比较。
package file

import (
	"fmt"
	"os"
	"path"          // remote (Linux) path operations - always uses '/'
	"path/filepath" // local (OS-native) path operations
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

// remoteSoftwareDirExists 远端软件目录是否存在（不存在则跳过扫描，避免无意义 ls）。
func remoteSoftwareDirExists(ctx *runner.StepContext, dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	return DirExists(ctx, dir)
}

// localSoftwareDirExists 控制端本地软件目录是否存在。
func localSoftwareDirExists(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// FindAndDistribute 查找文件并分发到远程，返回远程文件路径。
//
// 输入分两种情况：
//  1. 路径（含目录分隔符）：精确查找，先远程后本地，不做 baseName 拼目录搜索
//  2. 纯文件名：在远程目标目录 → 远程 $HOME → 本地目录列表 依次搜索
//
// 跨平台说明（控制端可能是 Windows/Linux/macOS，目标端始终 Linux）：
//   - 本地文件查找使用 filepath（OS 原生路径分隔符）
//   - 远程路径拼接统一使用 path（始终 '/'），避免 Windows 下生成反斜杠路径
//   - filepath.IsAbs 在 Windows 上识别 C:\... 等盘符路径，在 Unix 上识别 /...
//   - strings.HasPrefix(filename, "/") 用于判断可能的远程 Linux 绝对路径
func FindAndDistribute(
	ctx *runner.StepContext,
	filename string,
	localDirs []string,
	remoteDir string,
) (string, error) {
	if strings.HasPrefix(filename, "/dev/") {
		return filename, nil
	}

	normalized := filepath.ToSlash(filename)
	baseName := path.Base(normalized)
	hasDir := (normalized != baseName)

	remoteHomeDir := "/root"
	r, _ := ctx.Execute("echo $HOME", false)
	if r != nil && strings.TrimSpace(r.GetStdout()) != "" {
		remoteHomeDir = strings.TrimSpace(r.GetStdout())
	}

	var localPath string

	if hasDir {
		if strings.HasPrefix(filename, "/") {
			r, _ := ctx.Execute(fmt.Sprintf("test -f '%s' && echo 'exists'", filename), false)
			if r != nil && strings.Contains(r.GetStdout(), "exists") {
				return filename, nil
			}
		}

		if filepath.IsAbs(filename) {
			if _, err := os.Stat(filename); err == nil {
				localPath = filename
			}
		}

		if localPath == "" && !filepath.IsAbs(filename) && !strings.HasPrefix(filename, "/") {
			for _, dir := range localDirs {
				candidate := filepath.Join(dir, filename)
				if _, err := os.Stat(candidate); err == nil {
					localPath = candidate
					break
				}
			}
		}

		if localPath == "" {
			return "", fmt.Errorf("file '%s' not found on remote (exact path) or locally", filename)
		}
	} else {
		if remoteDir != "" {
			remotePath := path.Join(remoteDir, baseName)
			r, _ := ctx.Execute(fmt.Sprintf("test -f '%s' && echo 'exists'", remotePath), false)
			if r != nil && strings.Contains(r.GetStdout(), "exists") {
				return remotePath, nil
			}
		}

		remoteHomePath := path.Join(remoteHomeDir, baseName)
		r, _ := ctx.Execute(fmt.Sprintf("test -f '%s' && echo 'exists'", remoteHomePath), false)
		if r != nil && strings.Contains(r.GetStdout(), "exists") {
			return remoteHomePath, nil
		}

		for _, dir := range localDirs {
			candidate := filepath.Join(dir, baseName)
			if _, err := os.Stat(candidate); err == nil {
				localPath = candidate
				break
			}
		}

		if localPath == "" {
			return "", fmt.Errorf("file '%s' not found in remote dirs ['%s', '%s'] or local dirs %v",
				filename, remoteDir, remoteHomeDir, localDirs)
		}
	}

	uploadPath := path.Join(remoteHomeDir, baseName)

	if err := ctx.Executor.Upload(localPath, uploadPath); err != nil {
		return "", fmt.Errorf("failed to upload '%s' to '%s': %w", localPath, uploadPath, err)
	}

	r, _ = ctx.Execute(fmt.Sprintf("test -f '%s' && echo 'exists'", uploadPath), false)
	if r == nil || !strings.Contains(r.GetStdout(), "exists") {
		return "", fmt.Errorf("file upload verification failed for '%s'", uploadPath)
	}

	return uploadPath, nil
}

// FileExists 检查远程文件是否存在
func FileExists(ctx *runner.StepContext, path string) bool {
	result, _ := ctx.Execute(fmt.Sprintf("test -f '%s' && echo 'exists'", path), false)
	return result != nil && strings.Contains(result.GetStdout(), "exists")
}

// DirExists 检查远程目录是否存在
func DirExists(ctx *runner.StepContext, path string) bool {
	result, _ := ctx.Execute(fmt.Sprintf("test -d '%s' && echo 'exists'", path), false)
	return result != nil && strings.Contains(result.GetStdout(), "exists")
}

// EnsureDir 确保远程目录存在
func EnsureDir(ctx *runner.StepContext, path string, sudo bool) error {
	result, err := ctx.Execute(fmt.Sprintf("mkdir -p '%s'", path), sudo)
	if err != nil {
		return err
	}
	if result != nil && result.GetExitCode() != 0 {
		return fmt.Errorf("failed to create directory '%s': %s", path, result.GetStderr())
	}
	return nil
}

// IsDevicePath 判断是否为设备路径
func IsDevicePath(path string) bool {
	return strings.HasPrefix(path, "/dev/")
}

// IsISOFile 判断是否为 ISO 文件
func IsISOFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".iso")
}

// remoteSearchDirs 返回自动发现时需要扫描的远端目录列表（去重）。
// 顺序为：[remoteDir（若为空则默认 /data/yashan/soft）, SSH 用户 $HOME]。
// 即使用户通过 --remote-software-dir 指定了远端目录，仍会额外扫描 $HOME 下的包（与历史行为一致）。
func remoteSearchDirs(ctx *runner.StepContext, remoteDir string) []string {
	homeDir := "/root"
	if r, _ := ctx.Execute("echo $HOME", false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
		homeDir = strings.TrimSpace(r.GetStdout())
	}

	if remoteDir == "" {
		remoteDir = "/data/yashan/soft"
	}

	seen := make(map[string]bool)
	var dirs []string
	for _, d := range []string{remoteDir, homeDir} {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// FindLatestDBPackage 自动查找最新版本的数据库软件包
// 软件包格式（linux 段常见两种写法，均支持）：
//   - x86: yashandb-23.4.7.100-linux-x86_64.tar.gz 或 ...-linux-x86-64.tar.gz
//   - arm: yashandb-23.4.7.100-linux-aarch64.tar.gz 或 ...-linux-aarch-64.tar.gz
//
// 在远端 software 目录（仅存在者）与本地 software 目录（仅存在者）中收集全部候选，按版本号取全局最新：
//   - 若选中远端文件 → 返回远端绝对路径，可直接使用
//   - 若选中本地文件 → 返回本地绝对路径，由 FindAndDistribute 上传
func FindLatestDBPackage(
	ctx *runner.StepContext,
	localDirs []string,
	remoteDir string,
) (string, error) {
	dbLinuxArchRE := `(?:x86_64|x86-64)`
	result, _ := ctx.Execute("uname -m", false)
	if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
		remoteArch := strings.TrimSpace(result.GetStdout())
		if remoteArch == "aarch64" || remoteArch == "arm64" {
			dbLinuxArchRE = `(?:aarch64|aarch-64)`
		}
	}

	pattern := fmt.Sprintf(`yashandb-(\d+\.\d+\.\d+\.\d+)-linux-%s\.tar\.gz`, dbLinuxArchRE)
	re := regexp.MustCompile(pattern)

	var remotePackages []string
	for _, dir := range remoteSearchDirs(ctx, remoteDir) {
		if !remoteSoftwareDirExists(ctx, dir) {
			continue
		}
		result, _ := ctx.Execute(fmt.Sprintf("ls -1 %s/yashandb-*-linux-*.tar.gz 2>/dev/null || true", dir), false)
		if result != nil && result.GetStdout() != "" {
			for _, f := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
				f = strings.TrimSpace(f)
				if f != "" && re.MatchString(path.Base(f)) {
					remotePackages = append(remotePackages, f)
				}
			}
		}
	}

	var localPackages []string
	for _, dir := range localDirs {
		if !localSoftwareDirExists(dir) {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(dir, "yashandb-*-linux-*.tar.gz"))
		if err == nil {
			for _, m := range matches {
				if re.MatchString(filepath.Base(m)) {
					localPackages = append(localPackages, m)
				}
			}
		}
	}

	all := append(append([]string{}, remotePackages...), localPackages...)
	if len(all) == 0 {
		remoteDirs := remoteSearchDirs(ctx, remoteDir)
		return "", fmt.Errorf("no yashandb package found (linux-x86_64|linux-x86-64 or linux-aarch64|linux-aarch-64) in remote dirs %v or local dirs %v", remoteDirs, localDirs)
	}

	latest := findLatestVersion(all, re)
	return latest, nil
}

// FindLatestYCMPackage 自动查找最新版本的 YCM 软件包
// 软件包格式（linux 段常见两种写法，均支持）：
//   - x86: yashandb-cloud-manager-23.5.3.2-linux-x86_64.tar.gz 或 ...-linux-x86-64.tar.gz
//   - arm: yashandb-cloud-manager-23.5.3.2-linux-aarch64.tar.gz 或 ...-linux-aarch-64.tar.gz
//
// 远端与本地（仅存在目录）合并候选后取版本最新；远端路径直接使用，本地绝对路径由 FindAndDistribute 上传。
func FindLatestYCMPackage(
	ctx *runner.StepContext,
	localDirs []string,
	remoteDir string,
) (string, error) {
	ycmLinuxArchRE := `(?:x86_64|x86-64)`
	result, _ := ctx.Execute("uname -m", false)
	if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
		remoteArch := strings.TrimSpace(result.GetStdout())
		if remoteArch == "aarch64" || remoteArch == "arm64" {
			ycmLinuxArchRE = `(?:aarch64|aarch-64)`
		}
	}

	pattern := fmt.Sprintf(`yashandb-cloud-manager-(\d+\.\d+\.\d+\.\d+)-linux-%s\.tar\.gz`, ycmLinuxArchRE)
	re := regexp.MustCompile(pattern)

	var remotePackages []string
	for _, dir := range remoteSearchDirs(ctx, remoteDir) {
		if !remoteSoftwareDirExists(ctx, dir) {
			continue
		}
		result, _ := ctx.Execute(fmt.Sprintf("ls -1 %s/yashandb-cloud-manager-*-linux-*.tar.gz 2>/dev/null || true", dir), false)
		if result != nil && result.GetStdout() != "" {
			for _, f := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
				f = strings.TrimSpace(f)
				if f != "" && re.MatchString(path.Base(f)) {
					remotePackages = append(remotePackages, f)
				}
			}
		}
	}

	var localPackages []string
	for _, dir := range localDirs {
		if !localSoftwareDirExists(dir) {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(dir, "yashandb-cloud-manager-*-linux-*.tar.gz"))
		if err == nil {
			for _, m := range matches {
				if re.MatchString(filepath.Base(m)) {
					localPackages = append(localPackages, m)
				}
			}
		}
	}

	all := append(append([]string{}, remotePackages...), localPackages...)
	if len(all) == 0 {
		remoteDirs := remoteSearchDirs(ctx, remoteDir)
		return "", fmt.Errorf("no yashandb-cloud-manager package found (linux-x86_64|linux-x86-64 or linux-aarch64|linux-aarch-64) in remote dirs %v or local dirs %v", remoteDirs, localDirs)
	}

	return findLatestVersion(all, re), nil
}

// FindLatestYMPPackage 自动查找最新版本的 YMP 软件包
// 软件包格式（linux 段常见两种写法，均支持）：
//   - x86: yashan-migrate-platform-23.5.3.2-linux-x86_64.zip 或 ...-linux-x86-64.zip
//   - arm: yashan-migrate-platform-23.5.3.2-linux-aarch64.zip 或 ...-linux-aarch-64.zip
//
// 远端与本地（仅存在目录）合并候选后取版本最新；远端路径直接使用，本地绝对路径由 FindAndDistribute 上传。
func FindLatestYMPPackage(
	ctx *runner.StepContext,
	localDirs []string,
	remoteDir string,
) (string, error) {
	// 与文件名中 linux-<arch> 段对齐：x86 允许 x86_64 / x86-64；aarch64 允许 aarch64 / aarch-64
	ympLinuxArchRE := `(?:x86_64|x86-64)`
	result, _ := ctx.Execute("uname -m", false)
	if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
		remoteArch := strings.TrimSpace(result.GetStdout())
		if remoteArch == "aarch64" || remoteArch == "arm64" {
			ympLinuxArchRE = `(?:aarch64|aarch-64)`
		}
	}

	pattern := fmt.Sprintf(`yashan-migrate-platform-(\d+\.\d+\.\d+\.\d+)-linux-%s\.zip`, ympLinuxArchRE)
	re := regexp.MustCompile(pattern)

	var remotePackages []string
	for _, dir := range remoteSearchDirs(ctx, remoteDir) {
		if !remoteSoftwareDirExists(ctx, dir) {
			continue
		}
		// 用较宽 glob 再 regex 过滤，避免远端 shell 对花括号展开差异
		result, _ := ctx.Execute(fmt.Sprintf("ls -1 %s/yashan-migrate-platform-*-linux-*.zip 2>/dev/null || true", dir), false)
		if result != nil && result.GetStdout() != "" {
			for _, f := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
				f = strings.TrimSpace(f)
				if f != "" && re.MatchString(path.Base(f)) {
					remotePackages = append(remotePackages, f)
				}
			}
		}
	}

	var localPackages []string
	for _, dir := range localDirs {
		if !localSoftwareDirExists(dir) {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(dir, "yashan-migrate-platform-*-linux-*.zip"))
		if err == nil {
			for _, m := range matches {
				if re.MatchString(filepath.Base(m)) {
					localPackages = append(localPackages, m)
				}
			}
		}
	}

	all := append(append([]string{}, remotePackages...), localPackages...)
	if len(all) == 0 {
		remoteDirs := remoteSearchDirs(ctx, remoteDir)
		return "", fmt.Errorf("no yashan-migrate-platform package found (linux-x86_64|linux-x86-64 or linux-aarch64|linux-aarch-64) in remote dirs %v or local dirs %v", remoteDirs, localDirs)
	}

	return findLatestVersion(all, re), nil
}

// FindZstdSourceTarball 在本地/远端软件目录中查找 zstd 源码包（.tar.gz），供 RHEL7 等无 libzstd RPM 时编译安装。
// 优先 zstd-1.5.7.tar.gz；否则取 zstd-x.y.z.tar.gz 中版本号最高者。explicit 非空时作为固定路径或文件名（与 FindAndDistribute 规则一致）。
func FindZstdSourceTarball(
	ctx *runner.StepContext,
	localDirs []string,
	remoteDir string,
	explicit string,
) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		norm := filepath.ToSlash(explicit)
		base := path.Base(norm)
		if !strings.HasPrefix(base, "zstd-") || !strings.HasSuffix(base, ".tar.gz") {
			return "", fmt.Errorf("os_zstd_source_tarball must be a zstd-*.tar.gz path or filename, got: %s", explicit)
		}
		if strings.Contains(norm, "/") {
			return norm, nil
		}
		return base, nil
	}

	pattern := `zstd-(\d+\.\d+\.\d+)\.tar\.gz`
	re := regexp.MustCompile(`^` + pattern + `$`)
	reVer := regexp.MustCompile(pattern)

	// 优先 zstd-1.5.7.tar.gz：远端与本地（仅存在目录）均扫描，远端命中优先返回远端绝对路径。
	shellQuote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	for _, dir := range remoteSearchDirs(ctx, remoteDir) {
		if !remoteSoftwareDirExists(ctx, dir) {
			continue
		}
		pref := path.Join(dir, "zstd-1.5.7.tar.gz")
		r, _ := ctx.Execute(fmt.Sprintf("test -f %s && echo ok", shellQuote(pref)), false)
		if r != nil && strings.Contains(strings.TrimSpace(r.GetStdout()), "ok") {
			return pref, nil
		}
	}
	for _, dir := range localDirs {
		if !localSoftwareDirExists(dir) {
			continue
		}
		pref := filepath.Join(dir, "zstd-1.5.7.tar.gz")
		if _, err := os.Stat(pref); err == nil {
			return pref, nil
		}
	}

	var remotePackages []string
	for _, dir := range remoteSearchDirs(ctx, remoteDir) {
		if !remoteSoftwareDirExists(ctx, dir) {
			continue
		}
		result, _ := ctx.Execute(fmt.Sprintf("ls -1 %s/zstd-*.tar.gz 2>/dev/null || true", dir), false)
		if result != nil && result.GetStdout() != "" {
			for _, f := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
				f = strings.TrimSpace(f)
				if f != "" && re.MatchString(path.Base(f)) {
					remotePackages = append(remotePackages, f)
				}
			}
		}
	}

	var localPackages []string
	for _, dir := range localDirs {
		if !localSoftwareDirExists(dir) {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(dir, "zstd-*.tar.gz"))
		if err != nil {
			continue
		}
		for _, m := range matches {
			if re.MatchString(filepath.Base(m)) {
				localPackages = append(localPackages, m)
			}
		}
	}

	all := append(append([]string{}, remotePackages...), localPackages...)
	if len(all) == 0 {
		remoteDirs := remoteSearchDirs(ctx, remoteDir)
		return "", fmt.Errorf("no zstd source tarball (zstd-x.y.z.tar.gz, prefer zstd-1.5.7.tar.gz) in remote dirs %v or local dirs %v", remoteDirs, localDirs)
	}
	return findLatestVersion(all, reVer), nil
}

// FindLatestInstantclientBasicPackage 自动查找最新版本的 Oracle instantclient-basic 软件包。
// 详见 findLatestInstantclientPackage。
func FindLatestInstantclientBasicPackage(
	ctx *runner.StepContext,
	localDirs []string,
	remoteDir string,
) (string, error) {
	return findLatestInstantclientPackage(ctx, localDirs, remoteDir, "basic")
}

// FindLatestInstantclientSQLPlusPackage 自动查找最新版本的 Oracle instantclient-sqlplus 软件包。
// 详见 findLatestInstantclientPackage。
func FindLatestInstantclientSQLPlusPackage(
	ctx *runner.StepContext,
	localDirs []string,
	remoteDir string,
) (string, error) {
	return findLatestInstantclientPackage(ctx, localDirs, remoteDir, "sqlplus")
}

// findLatestInstantclientPackage 自动查找最新版本的 Oracle instantclient 软件包。
//
// component 为 "basic" 或 "sqlplus"，对应文件名：
//
//	instantclient-<component>-linux.arm64-19.10.0.0.0dbru-2.zip   ← arm64，有发布号
//	instantclient-<component>-linux.x86_64-19.10.0.0.0dbru-2.zip  ← x86_64，有发布号
//	instantclient-<component>-linux.x64-19.29.0.0.0dbru.zip       ← x64（x86_64 别名），无发布号
//
// 架构映射（来自 uname -m）：
//   - aarch64 / arm64  →  arm64        （glob 和 regex 均用 "arm64"）
//   - x86_64           →  x64|x86_64  （glob 用 "x*" 通配，regex 用 "(?:x86_64|x64)"）
//
// 版本排序：先比较点分版本号，再比较发布号（缺省视为 0）。
//
// 返回：远端为完整远端路径；本地为完整本地路径（由 FindAndDistribute 上传）。
func findLatestInstantclientPackage(
	ctx *runner.StepContext,
	localDirs []string,
	remoteDir string,
	component string,
) (string, error) {
	icGlob := "x*"
	icReArch := `(?:x86_64|x64)`
	result, _ := ctx.Execute("uname -m", false)
	if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
		if remoteArch := strings.TrimSpace(result.GetStdout()); remoteArch == "aarch64" || remoteArch == "arm64" {
			icGlob = "arm64"
			icReArch = "arm64"
		}
	}

	patternStr := fmt.Sprintf(`instantclient-%s-linux\.(?:%s)-(\d+(?:\.\d+)+)[a-z]*(?:-(\d+))?\.zip`, component, icReArch)
	re := regexp.MustCompile(patternStr)

	lsGlob := fmt.Sprintf("instantclient-%s-linux.%s-*.zip", component, icGlob)

	var remotePackages []string
	for _, dir := range remoteSearchDirs(ctx, remoteDir) {
		if !remoteSoftwareDirExists(ctx, dir) {
			continue
		}
		r, _ := ctx.Execute(
			fmt.Sprintf("ls -1 %s/%s 2>/dev/null || true", dir, lsGlob), false)
		if r != nil && r.GetStdout() != "" {
			for _, f := range strings.Split(strings.TrimSpace(r.GetStdout()), "\n") {
				f = strings.TrimSpace(f)
				if f != "" && re.MatchString(path.Base(f)) {
					remotePackages = append(remotePackages, f)
				}
			}
		}
	}

	var localPackages []string
	for _, dir := range localDirs {
		if !localSoftwareDirExists(dir) {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(dir, lsGlob))
		if err == nil {
			for _, m := range matches {
				if re.MatchString(filepath.Base(m)) {
					localPackages = append(localPackages, m)
				}
			}
		}
	}

	all := append(append([]string{}, remotePackages...), localPackages...)
	if len(all) == 0 {
		remoteDirs := remoteSearchDirs(ctx, remoteDir)
		return "", fmt.Errorf(
			"no instantclient-%s package found (arch glob=%s) in remote dirs %v or local dirs %v",
			component, icGlob, remoteDirs, localDirs)
	}

	return findLatestInstantclientVersion(all, re), nil
}

// findLatestInstantclientVersion 从文件列表中找到版本号+发布号最大的 instantclient 包
// re 需含两个捕获组：group1 = 版本号数字部分，group2 = 发布号（可选，空时视为 0）
func findLatestInstantclientVersion(files []string, re *regexp.Regexp) string {
	if len(files) == 0 {
		return ""
	}
	if len(files) == 1 {
		return files[0]
	}

	type versionFile struct {
		file    string
		version []int // 版本各段 + 末尾发布号（缺省 0）
	}

	var versionFiles []versionFile
	for _, f := range files {
		baseName := path.Base(filepath.ToSlash(f))
		m := re.FindStringSubmatch(baseName)
		if len(m) >= 2 {
			parts := strings.Split(m[1], ".")
			ver := make([]int, len(parts))
			for i, p := range parts {
				ver[i], _ = strconv.Atoi(p)
			}
			// group2 = 发布号，缺省（空字符串）时视为 0
			rel := 0
			if len(m) >= 3 && m[2] != "" {
				rel, _ = strconv.Atoi(m[2])
			}
			ver = append(ver, rel)
			versionFiles = append(versionFiles, versionFile{file: f, version: ver})
		}
	}

	if len(versionFiles) == 0 {
		return files[0]
	}

	sort.Slice(versionFiles, func(i, j int) bool {
		vi, vj := versionFiles[i].version, versionFiles[j].version
		for k := 0; k < len(vi) && k < len(vj); k++ {
			if vi[k] != vj[k] {
				return vi[k] > vj[k]
			}
		}
		return len(vi) > len(vj)
	})

	return versionFiles[0].file
}

// findLatestVersion 从文件列表中找到版本号最大的文件
// 输入的文件名可能是远程 Linux 路径或本地 OS 路径，统一先 ToSlash 再用 path.Base 提取文件名
func findLatestVersion(files []string, re *regexp.Regexp) string {
	if len(files) == 0 {
		return ""
	}
	if len(files) == 1 {
		return files[0]
	}

	type versionFile struct {
		file    string
		version []int
	}

	var versionFiles []versionFile
	for _, f := range files {
		baseName := path.Base(filepath.ToSlash(f))
		matches := re.FindStringSubmatch(baseName)
		if len(matches) > 1 {
			versionStr := matches[1]
			parts := strings.Split(versionStr, ".")
			version := make([]int, len(parts))
			for i, p := range parts {
				v, _ := strconv.Atoi(p)
				version[i] = v
			}
			versionFiles = append(versionFiles, versionFile{file: f, version: version})
		}
	}

	if len(versionFiles) == 0 {
		return files[0]
	}

	sort.Slice(versionFiles, func(i, j int) bool {
		vi, vj := versionFiles[i].version, versionFiles[j].version
		for k := 0; k < len(vi) && k < len(vj); k++ {
			if vi[k] != vj[k] {
				return vi[k] > vj[k]
			}
		}
		return len(vi) > len(vj)
	})

	return versionFiles[0].file
}
