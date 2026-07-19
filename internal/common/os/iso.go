// iso.go - ISO 挂载与本地 YUM repo 准备公共逻辑
//
// EnsureLocalISORepo 在 os_yum_mode=local 时确保：
//  1. ISO 已挂载到指定挂载点
//  2. YUM repo 文件存在（不存在则自动生成）
//
// ISO 来源查找顺序（--os-iso-device=auto 为默认）：
//  a. 依次探测 /dev/cdrom、/dev/sr0：临时挂载并读取 .treeinfo/.discinfo，版本/架构与目标 OS 一致则直接使用
//  b. 指定块设备路径：同上版本校验
//  c. 指定文件名 → FindAndDistribute；找不到或版本不一致则回退智能搜索
//  d. 在 remoteDir / $HOME / /data/yashan/soft / localDirs 中按 OS profile 选择最佳 *.iso

package os

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

// ensureOSInfo 在 OSInfo 未填充时（如 -s 跳过 B-001 的旧路径）远端探测一次。
func ensureOSInfo(ctx *runner.StepContext) {
	if ctx == nil || ctx.OSInfo != nil {
		return
	}
	ctx.LogPhase("osinfo-detect-start", "OSInfo missing, probing target OS")
	ctx.OSInfo = DetectOSInfo(ctx)
	if ctx.OSInfo != nil {
		ctx.LogPhase("osinfo-detect-done", fmt.Sprintf(
			"id=%s version=%s arch=%s pkg_mgr=%s rhel7=%v rhel8=%v",
			ctx.OSInfo.ID, ctx.OSInfo.VersionID, ctx.OSInfo.Arch,
			ctx.OSInfo.PkgManager, ctx.OSInfo.IsRHEL7, ctx.OSInfo.IsRHEL8,
		))
	} else {
		ctx.LogPhase("osinfo-detect-fail", "DetectOSInfo returned nil")
	}
}

// EnsureLocalISORepo 在 os_yum_mode=local 时确保 ISO/光驱已挂载且 repo 就绪。
// 空模式下的 auto fallback 请直接调用 PrepareLocalMediaRepo。
func EnsureLocalISORepo(ctx *runner.StepContext) error {
	if !IsLocalYumMode(GetYumMode(ctx)) {
		ctx.LogPhase("media-repo-skip", fmt.Sprintf("yum_mode=%q not local", GetYumMode(ctx)))
		return nil
	}
	return PrepareLocalMediaRepo(ctx)
}

// EnsureLocalISORepoBeforeInstall 显式 local 模式时装包前置（空模式 fallback 由 InstallPackages 处理）。
func EnsureLocalISORepoBeforeInstall(ctx *runner.StepContext) error {
	return EnsureLocalISORepo(ctx)
}

// PrepareLocalMediaRepo 挂载 auto 介质（光驱或 ISO）并确保 local yum repo 文件存在。
func PrepareLocalMediaRepo(ctx *runner.StepContext) error {
	ensureOSInfo(ctx)
	mountpoint := ctx.GetParamString("os_iso_mountpoint", "/media")
	repoFile := ctx.GetParamString("os_yum_repo_file", "/etc/yum.repos.d/local.repo")
	device := ctx.GetParamString("os_iso_device", ISODeviceAuto)
	ctx.LogPhase("media-repo-start", fmt.Sprintf("mountpoint=%s repo_file=%s device=%s", mountpoint, repoFile, device))

	source := "already_mounted"
	r, _ := ctx.Execute(fmt.Sprintf("mountpoint -q %s 2>/dev/null", mountpoint), false)
	if r != nil && r.GetExitCode() == 0 {
		ctx.LogPhase("media-mount-skip", fmt.Sprintf("mountpoint=%s already_mounted=true", mountpoint))
	} else {
		ctx.LogPhase("media-locate-start", fmt.Sprintf("mountpoint=%s action=locate_iso_source", mountpoint))

		profile := ISOProfileFromOSInfo(ctx.OSInfo)

		isoPath, err := resolveISOPath(ctx, device, profile)
		if err != nil {
			ctx.LogPhase("media-repo-fail", fmt.Sprintf("resolve ISO: %v", err))
			return err
		}
		source = isoPath
		ctx.LogPhase("media-locate-done", fmt.Sprintf("source=%s", isoPath))

		ctx.Execute(fmt.Sprintf("mkdir -p %s", mountpoint), true)
		if err := mountISOAt(ctx, isoPath, mountpoint); err != nil {
			ctx.LogPhase("media-repo-fail", fmt.Sprintf("mount: %v", err))
			return err
		}

		r, _ = ctx.Execute(fmt.Sprintf("mountpoint -q %s", mountpoint), false)
		if r == nil || r.GetExitCode() != 0 {
			ctx.LogPhase("media-repo-fail", fmt.Sprintf("mount verify failed at %s", mountpoint))
			return fmt.Errorf("ISO mount verification failed at %s", mountpoint)
		}
		ctx.LogPhase("media-mount-done", fmt.Sprintf("mountpoint=%s source=%s", mountpoint, isoPath))
		ctx.SetResult("iso_path", isoPath)
	}

	if err := ensureRepoFile(ctx, mountpoint, repoFile); err != nil {
		ctx.LogPhase("media-repo-fail", fmt.Sprintf("repo file: %v", err))
		return err
	}
	ctx.LogPhase("media-repo-done", fmt.Sprintf("mountpoint=%s source=%s repo_file=%s", mountpoint, source, repoFile))
	return nil
}

// LocalMediaAvailable 判断 auto fallback 是否可用（匹配光驱介质或可匹配的 ISO 文件）。
func LocalMediaAvailable(ctx *runner.StepContext) bool {
	ok, _ := localMediaAvailability(ctx, true)
	return ok
}

// localMediaAvailability 探测 auto 介质是否可用；log=true 时写入 media-check-start/done phase。
func localMediaAvailability(ctx *runner.StepContext, log bool) (bool, string) {
	ensureOSInfo(ctx)
	profile := ISOProfileFromOSInfo(ctx.OSInfo)
	device := ctx.GetParamString("os_iso_device", ISODeviceAuto)
	if log {
		ctx.LogPhase("media-check-start", fmt.Sprintf(
			"device=%s profile_family=%s major=%d arch=%s",
			device, profile.Family, profile.MajorVer, profile.Arch,
		))
	}
	reason := func(available bool, msg string) (bool, string) {
		if log {
			ctx.LogPhase("media-check-done", fmt.Sprintf("available=%v reason=%s", available, msg))
		}
		if available {
			return true, msg
		}
		return false, msg
	}

	if IsAutoISODevice(device) {
		if p, ok, err := resolveMatchingBlockDevice(ctx, profile, DefaultBlockDevices()); err == nil && ok {
			return reason(true, fmt.Sprintf("block_device=%s", p))
		}
	} else if commonfile.IsDevicePath(device) {
		if p, ok, err := probeBlockDevice(ctx, device, profile); err == nil && ok {
			return reason(true, fmt.Sprintf("block_device=%s", p))
		}
	}

	remoteCandidates, localBasenames, err := listISOCandidates(ctx)
	if err != nil {
		return reason(false, "list_iso_candidates_error")
	}
	type candidate struct {
		name       string
		remotePath string
	}
	var merged []candidate
	seenName := map[string]struct{}{}
	add := func(name, remotePath string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seenName[name]; ok {
			return
		}
		seenName[name] = struct{}{}
		merged = append(merged, candidate{name: name, remotePath: remotePath})
	}
	for _, p := range remoteCandidates {
		add(filepathBase(p), p)
	}
	for _, base := range localBasenames {
		add(base, "")
	}
	if len(merged) == 0 {
		return reason(false, "no_iso_candidates")
	}
	names := make([]string, len(merged))
	for i, c := range merged {
		names[i] = c.name
	}
	bestName, score, err := SelectBestISOFilename(names, profile)
	if err != nil {
		return reason(false, fmt.Sprintf("no_matching_iso err=%v", err))
	}
	return reason(true, fmt.Sprintf("iso_file=%s score=%d", bestName, score))
}

func resolveISOPath(ctx *runner.StepContext, device string, profile ISOProfile) (string, error) {
	device = strings.TrimSpace(device)

	if IsAutoISODevice(device) {
		ctx.LogPhase("iso-resolve-auto", fmt.Sprintf(
			"profile_family=%s major=%d arch=%s",
			profile.Family, profile.MajorVer, profile.Arch,
		))
		if p, ok, err := resolveMatchingBlockDevice(ctx, profile, DefaultBlockDevices()); err != nil {
			return "", err
		} else if ok {
			return p, nil
		}
		return resolveISOFileForProfile(ctx, profile, "")
	}

	if commonfile.IsDevicePath(device) {
		if p, ok, err := probeBlockDevice(ctx, device, profile); err != nil {
			return "", err
		} else if ok {
			ctx.LogPhase("iso-block-use", fmt.Sprintf("device=%s reason=matches_os_profile", device))
			return p, nil
		}
		ctx.Logger.Warn("Block device %s media does not match OS profile, searching ISO files...", device)
		return resolveISOFileForProfile(ctx, profile, "")
	}

	ctx.LogPhase("iso-search-file", fmt.Sprintf("filename=%s", device))
	isoPath, err := commonfile.FindAndDistribute(ctx, device, ctx.LocalSoftwareDirs, ctx.RemoteSoftwareDir)
	if err == nil {
		if matched, err := validateISOFileMatchesProfile(ctx, isoPath, profile); err != nil {
			return "", err
		} else if matched {
			ctx.LogPhase("iso-file-match", fmt.Sprintf("path=%s reason=matches_os_profile", isoPath))
			return isoPath, nil
		}
		ctx.Logger.Warn("Specified ISO '%s' does not match OS profile, searching alternatives...", isoPath)
	} else {
		ctx.Logger.Warn("Specified ISO file '%s' not found (%v), searching alternatives...", device, err)
	}
	return resolveISOFileForProfile(ctx, profile, isoPath)
}

func resolveMatchingBlockDevice(ctx *runner.StepContext, profile ISOProfile, devices []string) (string, bool, error) {
	for _, dev := range devices {
		if !deviceHasMedia(ctx, dev) {
			ctx.LogPhase("iso-block-no-media", fmt.Sprintf("device=%s", dev))
			continue
		}
		p, ok, err := probeBlockDevice(ctx, dev, profile)
		if err != nil {
			return "", false, err
		}
		if ok {
			return p, true, nil
		}
		ctx.Logger.Warn("Block device %s media version/arch mismatch with target OS", dev)
	}
	return "", false, nil
}

func probeBlockDevice(ctx *runner.StepContext, device string, profile ISOProfile) (string, bool, error) {
	meta, err := readISOMetadataFromDevice(ctx, device)
	if err != nil {
		ctx.Logger.Warn("Failed to read metadata from %s: %v", device, err)
		return "", false, nil
	}
	if ISOMetadataMatchesProfile(meta, profile) {
		ctx.LogPhase("iso-block-match", fmt.Sprintf(
			"device=%s version=%s arch=%s source=%s",
			device, meta.Version, meta.Arch, meta.Source,
		))
		return device, true, nil
	}
	ctx.LogPhase("iso-block-mismatch", fmt.Sprintf(
		"device=%s iso_version=%s iso_arch=%s os_major=%d os_arch=%s",
		device, meta.Version, meta.Arch, profile.MajorVer, profile.Arch,
	))
	return "", false, nil
}

func readISOMetadataFromDevice(ctx *runner.StepContext, device string) (ISOMetadata, error) {
	probe := ISOProbeMountpoint
	ctx.Execute(fmt.Sprintf("mkdir -p %s", probe), true)
	defer func() {
		ctx.Execute(fmt.Sprintf("umount %s 2>/dev/null || true", probe), true)
	}()

	mountCmd := fmt.Sprintf("mount -t iso9660 %s %s", device, probe)
	if _, err := ctx.ExecuteWithCheck(mountCmd, true); err != nil {
		return ISOMetadata{}, fmt.Errorf("probe mount failed: %w", err)
	}
	return readISOMetadataFromMount(ctx, probe), nil
}

func validateISOFileMatchesProfile(ctx *runner.StepContext, isoPath string, profile ISOProfile) (bool, error) {
	meta, err := readISOMetadataFromISOFile(ctx, isoPath)
	if err != nil {
		return false, err
	}
	return ISOMetadataMatchesProfile(meta, profile), nil
}

func readISOMetadataFromISOFile(ctx *runner.StepContext, isoPath string) (ISOMetadata, error) {
	probe := ISOProbeMountpoint
	ctx.Execute(fmt.Sprintf("mkdir -p %s", probe), true)
	defer func() {
		ctx.Execute(fmt.Sprintf("umount %s 2>/dev/null || true", probe), true)
	}()

	mountCmd := fmt.Sprintf("mount -o loop %s %s", isoPath, probe)
	if _, err := ctx.ExecuteWithCheck(mountCmd, true); err != nil {
		return ISOMetadata{}, fmt.Errorf("probe mount ISO file failed: %w", err)
	}
	return readISOMetadataFromMount(ctx, probe), nil
}

func readISOMetadataFromMount(ctx *runner.StepContext, mountpoint string) ISOMetadata {
	var parts []ISOMetadata

	if r, _ := ctx.Execute(fmt.Sprintf("cat %s/.treeinfo 2>/dev/null", mountpoint), false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
		parts = append(parts, ParseISOMetadataFromTreeinfo(r.GetStdout()))
	}
	if r, _ := ctx.Execute(fmt.Sprintf("cat %s/.discinfo 2>/dev/null", mountpoint), false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
		parts = append(parts, ParseISOMetadataFromDiscinfo(r.GetStdout()))
	}
	for _, p := range []string{
		fmt.Sprintf("%s/BaseOS/media.repo", mountpoint),
		fmt.Sprintf("%s/media.repo", mountpoint),
	} {
		if r, _ := ctx.Execute(fmt.Sprintf("cat %s 2>/dev/null", p), false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
			parts = append(parts, ParseISOMetadataFromMediaRepo(r.GetStdout()))
			break
		}
	}
	return MergeISOMetadata(parts...)
}

func resolveISOFileForProfile(ctx *runner.StepContext, profile ISOProfile, excludePath string) (string, error) {
	remoteCandidates, localBasenames, err := listISOCandidates(ctx)
	if err != nil {
		return "", err
	}

	type candidate struct {
		name       string
		remotePath string // 非空则直接使用远端路径
	}
	var merged []candidate
	seenName := map[string]struct{}{}
	add := func(name, remotePath string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seenName[name]; ok {
			return
		}
		seenName[name] = struct{}{}
		merged = append(merged, candidate{name: name, remotePath: remotePath})
	}

	for _, p := range remoteCandidates {
		if excludePath != "" && p == excludePath {
			continue
		}
		add(filepathBase(p), p)
	}
	for _, base := range localBasenames {
		add(base, "")
	}

	if len(merged) == 0 {
		return "", fmt.Errorf("no ISO files found in software directories for OS profile (family=%s major=%d arch=%s)",
			profile.Family, profile.MajorVer, profile.Arch)
	}

	names := make([]string, len(merged))
	for i, c := range merged {
		names[i] = c.name
	}

	bestName, score, err := SelectBestISOFilename(names, profile)
	if err != nil {
		return "", err
	}
	ctx.LogPhase("iso-select", fmt.Sprintf("filename=%s score=%d", bestName, score))

	var chosenPath string
	for _, c := range merged {
		if c.name == bestName {
			if c.remotePath != "" {
				chosenPath = c.remotePath
			} else {
				uploaded, err := commonfile.FindAndDistribute(ctx, bestName, ctx.LocalSoftwareDirs, ctx.RemoteSoftwareDir)
				if err != nil {
					return "", fmt.Errorf("failed to distribute selected ISO %s: %w", bestName, err)
				}
				chosenPath = uploaded
			}
			break
		}
	}
	if chosenPath == "" {
		return "", fmt.Errorf("internal error: selected ISO %s not found in candidates", bestName)
	}

	if matched, err := validateISOFileMatchesProfile(ctx, chosenPath, profile); err != nil {
		return "", err
	} else if !matched {
		return "", fmt.Errorf("selected ISO %s does not match target OS (major=%d arch=%s)", chosenPath, profile.MajorVer, profile.Arch)
	}
	return chosenPath, nil
}

func listISOCandidates(ctx *runner.StepContext) (remotePaths []string, localBasenames []string, err error) {
	seenRemote := map[string]struct{}{}
	seenLocal := map[string]struct{}{}

	for _, dir := range remoteISOSearchDirs(ctx) {
		r, _ := ctx.Execute(fmt.Sprintf("ls -1 %s/*.iso 2>/dev/null", dir), false)
		if r == nil || r.GetExitCode() != 0 {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(r.GetStdout()), "\n") {
			p := strings.TrimSpace(line)
			if p == "" {
				continue
			}
			if _, ok := seenRemote[p]; ok {
				continue
			}
			seenRemote[p] = struct{}{}
			remotePaths = append(remotePaths, p)
		}
	}

	for _, dir := range ctx.LocalSoftwareDirs {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.iso"))
		for _, m := range matches {
			base := filepathBase(m)
			if _, ok := seenLocal[base]; ok {
				continue
			}
			seenLocal[base] = struct{}{}
			localBasenames = append(localBasenames, base)
		}
	}
	return remotePaths, localBasenames, nil
}

func mountISOAt(ctx *runner.StepContext, isoPath, mountpoint string) error {
	var mountCmd string
	if commonfile.IsDevicePath(isoPath) {
		mountCmd = fmt.Sprintf("mount -t iso9660 %s %s", isoPath, mountpoint)
	} else {
		mountCmd = fmt.Sprintf("mount -o loop %s %s", isoPath, mountpoint)
	}
	ctx.LogPhase("media-mount-start", fmt.Sprintf("cmd=%s source=%s mountpoint=%s", mountCmd, isoPath, mountpoint))
	if _, err := ctx.ExecuteWithCheck(mountCmd, true); err != nil {
		return fmt.Errorf("failed to mount ISO (%s): %w", isoPath, err)
	}
	return nil
}

func deviceHasMedia(ctx *runner.StepContext, device string) bool {
	r, _ := ctx.Execute(fmt.Sprintf("blkid %s 2>/dev/null", device), false)
	return r != nil && r.GetExitCode() == 0 && strings.TrimSpace(r.GetStdout()) != ""
}

func remoteISOSearchDirs(ctx *runner.StepContext) []string {
	remoteDir := ctx.RemoteSoftwareDir

	homeDir := "/root"
	if r, _ := ctx.Execute("echo $HOME", false); r != nil && strings.TrimSpace(r.GetStdout()) != "" {
		homeDir = strings.TrimSpace(r.GetStdout())
	}

	const defaultSoftDir = "/data/yashan/soft"
	if remoteDir != "" {
		if remoteDir == homeDir || remoteDir == defaultSoftDir {
			return []string{remoteDir}
		}
		return []string{remoteDir, homeDir, defaultSoftDir}
	}

	if homeDir == defaultSoftDir {
		return []string{homeDir}
	}
	return []string{homeDir, defaultSoftDir}
}

func ensureRepoFile(ctx *runner.StepContext, mountpoint, repoFile string) error {
	r, _ := ctx.Execute(fmt.Sprintf("test -f %s", repoFile), false)
	if r != nil && r.GetExitCode() == 0 {
		ctx.LogPhase("repo-file-skip", fmt.Sprintf("repo_file=%s already_exists=true", repoFile))
		return nil
	}

	ctx.LogPhase("repo-file-create-start", fmt.Sprintf("repo_file=%s", repoFile))

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

	ctx.LogPhase("repo-file-create-done", fmt.Sprintf("repo_file=%s", repoFile))
	return nil
}
