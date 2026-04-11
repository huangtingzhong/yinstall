// iso.go - ISO 挂载与本地 YUM repo 准备公共逻辑
//
// EnsureLocalISORepo 在 os_yum_mode=local-iso 时确保：
//  1. ISO 已挂载到指定挂载点
//  2. YUM repo 文件存在（不存在则自动生成）
//
// ISO 来源查找顺序：
//  a. os_iso_device 是块设备路径 (/dev/...) → 检查是否有介质
//       有介质 → 直接挂载
//       无介质 → 回退到 ISO 文件自动搜索
//  b. os_iso_device 是文件名/路径 → FindAndDistribute（远端 → 本地/上传）
//       找不到指定文件 → 回退到 ISO 文件自动搜索
//  c. 自动搜索：在 remoteDir / $HOME / /data/yashan/soft 及 localDirs 中
//     查找第一个 *.iso 文件

package os

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

// EnsureLocalISORepo 确保 local-iso 模式下 ISO 已挂载且 repo 文件就绪。
// 当 os_yum_mode != "local-iso" 时立即返回 nil（无操作）。
func EnsureLocalISORepo(ctx *runner.StepContext) error {
	if ctx.GetParamString("os_yum_mode", "none") != "local-iso" {
		return nil
	}

	device := ctx.GetParamString("os_iso_device", "/dev/cdrom")
	mountpoint := ctx.GetParamString("os_iso_mountpoint", "/media")
	repoFile := ctx.GetParamString("os_yum_repo_file", "/etc/yum.repos.d/local.repo")

	// ── 1. 检查是否已挂载 ──────────────────────────────────────────────────
	r, _ := ctx.Execute(fmt.Sprintf("mountpoint -q %s 2>/dev/null", mountpoint), false)
	if r != nil && r.GetExitCode() == 0 {
		ctx.Logger.Info("ISO already mounted at %s", mountpoint)
	} else {
		ctx.Logger.Info("ISO not mounted at %s, locating ISO source...", mountpoint)

		isoPath, err := resolveISOPath(ctx, device)
		if err != nil {
			return err
		}

		// ── 3. 创建挂载点并挂载 ───────────────────────────────────────────
		ctx.Execute(fmt.Sprintf("mkdir -p %s", mountpoint), true)

		var mountCmd string
		if commonfile.IsDevicePath(isoPath) {
			mountCmd = fmt.Sprintf("mount -t iso9660 %s %s", isoPath, mountpoint)
		} else {
			mountCmd = fmt.Sprintf("mount -o loop %s %s", isoPath, mountpoint)
		}
		ctx.Logger.Info("Mounting: %s", mountCmd)
		if _, err := ctx.ExecuteWithCheck(mountCmd, true); err != nil {
			return fmt.Errorf("failed to mount ISO (%s): %w", isoPath, err)
		}

		// 验证挂载成功
		r, _ = ctx.Execute(fmt.Sprintf("mountpoint -q %s", mountpoint), false)
		if r == nil || r.GetExitCode() != 0 {
			return fmt.Errorf("ISO mount verification failed at %s", mountpoint)
		}
		ctx.Logger.Info("ISO mounted successfully at %s", mountpoint)
	}

	// ── 4. 确保 repo 文件存在 ──────────────────────────────────────────────
	return ensureRepoFile(ctx, mountpoint, repoFile)
}

// resolveISOPath 按优先级确定最终使用的 ISO 路径（设备或文件）。
//
// 优先级：
//  1. 块设备路径且有介质
//  2. 指定文件名 → FindAndDistribute
//  3. 自动搜索 *.iso（远端 → 本地）
func resolveISOPath(ctx *runner.StepContext, device string) (string, error) {
	// ── 情况 A：块设备 ─────────────────────────────────────────────────────
	if commonfile.IsDevicePath(device) {
		if deviceHasMedia(ctx, device) {
			ctx.Logger.Info("Block device %s has media, using directly", device)
			return device, nil
		}
		ctx.Logger.Warn("Block device %s has no media, falling back to ISO file search...", device)
		// 直接进入自动搜索
		return autoFindISO(ctx)
	}

	// ── 情况 B：指定文件名/路径 ────────────────────────────────────────────
	ctx.Logger.Info("Searching for specified ISO file: %s", device)
	isoPath, err := commonfile.FindAndDistribute(
		ctx,
		device,
		ctx.LocalSoftwareDirs,
		ctx.RemoteSoftwareDir,
	)
	if err == nil {
		ctx.Logger.Info("ISO file located at: %s", isoPath)
		return isoPath, nil
	}
	ctx.Logger.Warn("Specified ISO file '%s' not found (%v), falling back to auto-search...", device, err)

	// ── 情况 C：自动搜索 *.iso ────────────────────────────────────────────
	return autoFindISO(ctx)
}

// deviceHasMedia 检查块设备是否有可读介质（使用 blkid 检测）
func deviceHasMedia(ctx *runner.StepContext, device string) bool {
	r, _ := ctx.Execute(fmt.Sprintf("blkid %s 2>/dev/null", device), false)
	return r != nil && r.GetExitCode() == 0 && strings.TrimSpace(r.GetStdout()) != ""
}

// autoFindISO 自动在远端和本地软件目录中搜索第一个 *.iso 文件。
// 搜索顺序：remoteDir → $HOME → /data/yashan/soft → localDirs
func autoFindISO(ctx *runner.StepContext) (string, error) {
	ctx.Logger.Info("Auto-searching for *.iso files...")

	// 远端搜索目录（复用 remoteSearchDirs 逻辑）
	remoteDirs := remoteISOSearchDirs(ctx)
	for _, dir := range remoteDirs {
		r, _ := ctx.Execute(
			fmt.Sprintf("ls -1 %s/*.iso 2>/dev/null | head -1", dir), false)
		if r != nil && r.GetExitCode() == 0 {
			found := strings.TrimSpace(r.GetStdout())
			if found != "" {
				ctx.Logger.Info("Found ISO on remote: %s", found)
				return found, nil
			}
		}
	}

	// 本地搜索目录
	for _, dir := range ctx.LocalSoftwareDirs {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.iso"))
		if len(matches) > 0 {
			isoPath, err := commonfile.FindAndDistribute(
				ctx,
				filepath.Base(matches[0]),
				ctx.LocalSoftwareDirs,
				ctx.RemoteSoftwareDir,
			)
			if err == nil {
				ctx.Logger.Info("Found ISO locally, uploaded to: %s", isoPath)
				return isoPath, nil
			}
		}
	}

	return "", fmt.Errorf(
		"no ISO file found in remote dirs %v or local dirs %v; "+
			"use --os-iso-device=<path-or-filename.iso> to specify the ISO explicitly",
		remoteDirs, ctx.LocalSoftwareDirs)
}

// remoteISOSearchDirs 返回远端 ISO 搜索目录列表（与 remoteSearchDirs 逻辑保持一致）
func remoteISOSearchDirs(ctx *runner.StepContext) []string {
	remoteDir := ctx.RemoteSoftwareDir

	homeDir := "/root"
	if r, _ := ctx.Execute("echo $HOME", false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
		homeDir = strings.TrimSpace(r.GetStdout())
	}

	const defaultSoftDir = "/data/yashan/soft"
	if remoteDir != "" {
		// 用户指定了目录，只搜该目录
		if remoteDir == homeDir || remoteDir == defaultSoftDir {
			return []string{remoteDir}
		}
		return []string{remoteDir}
	}

	// 未指定：搜索 $HOME 和默认目录（去重）
	if homeDir == defaultSoftDir {
		return []string{homeDir}
	}
	return []string{homeDir, defaultSoftDir}
}

// ensureRepoFile 确保 YUM repo 文件存在，不存在则根据 OS 类型自动生成
func ensureRepoFile(ctx *runner.StepContext, mountpoint, repoFile string) error {
	r, _ := ctx.Execute(fmt.Sprintf("test -f %s", repoFile), false)
	if r != nil && r.GetExitCode() == 0 {
		ctx.Logger.Info("YUM repo file already exists: %s", repoFile)
		return nil
	}

	ctx.Logger.Info("YUM repo file not found at %s, creating...", repoFile)

	var repoContent string
	if IsRHEL8(ctx.OSInfo) {
		repoContent = fmt.Sprintf(
			"[local-baseos]\nname=DVD for RHEL - BaseOS\nbaseurl=file://%s/BaseOS\nenabled=1\ngpgcheck=0\n\n"+
				"[local-appstream]\nname=DVD for RHEL - AppStream\nbaseurl=file://%s/AppStream\nenabled=1\ngpgcheck=0\n",
			mountpoint, mountpoint)
	} else {
		repoContent = fmt.Sprintf(
			"[local]\nname=Enterprise Linux DVD\nbaseurl=file://%s\ngpgcheck=0\nenabled=1\n",
			mountpoint)
	}

	ctx.Execute(fmt.Sprintf("mkdir -p %s", path.Dir(repoFile)), true)

	escaped := strings.ReplaceAll(repoContent, "'", `'\''`)
	cmd := fmt.Sprintf("printf '%%s' '%s' > %s", escaped, repoFile)
	if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
		return fmt.Errorf("failed to write repo file %s: %w", repoFile, err)
	}

	ctx.Logger.Info("YUM repo file created: %s", repoFile)
	return nil
}
