package os

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
)

type isoStubResult struct {
	stdout string
	stderr string
	exit   int
}

func (r *isoStubResult) GetStdout() string          { return r.stdout }
func (r *isoStubResult) GetStderr() string          { return r.stderr }
func (r *isoStubResult) GetExitCode() int           { return r.exit }
func (r *isoStubResult) GetDuration() time.Duration { return 0 }

type isoStubExecutor struct {
	handler func(cmd string) *isoStubResult
	uploads []string
}

func (e *isoStubExecutor) Execute(cmd string, _ bool) (runner.ExecResult, error) {
	if e.handler == nil {
		return &isoStubResult{exit: 0}, nil
	}
	res := e.handler(cmd)
	if res == nil {
		return &isoStubResult{exit: 0}, nil
	}
	return res, nil
}

func (e *isoStubExecutor) Host() string { return "10.0.0.1" }
func (e *isoStubExecutor) Close() error { return nil }

func (e *isoStubExecutor) Upload(local, remote string, _ *ssh.UploadContext) error {
	e.uploads = append(e.uploads, local+" -> "+remote)
	return nil
}

func isoTestLogger(t *testing.T) *logging.Logger {
	t.Helper()
	l, err := logging.NewLogger("test", t.TempDir(), "v", "a", "c")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func isoTestCtx(t *testing.T, exec *isoStubExecutor, params map[string]interface{}, osInfo *runner.OSInfo) *runner.StepContext {
	t.Helper()
	if params == nil {
		params = map[string]interface{}{}
	}
	return &runner.StepContext{
		Executor:          exec,
		Logger:            isoTestLogger(t),
		Params:            params,
		Results:           map[string]interface{}{},
		OSInfo:            osInfo,
		LocalSoftwareDirs: nil,
		RemoteSoftwareDir: "/data/yashan/soft",
	}
}

func ol88Aarch64OSInfo() *runner.OSInfo {
	return &runner.OSInfo{
		Name: "Oracle Linux Server", VersionID: "8.8", ID: "ol",
		Arch: "aarch64", IsRHEL8: true, PkgManager: "dnf",
	}
}

const testTreeinfoOL88Aarch64 = `[general]
version = 8.8
arch = aarch64
`

func TestEnsureLocalISORepo_skipsWhenNotLocalISO(t *testing.T) {
	ctx := isoTestCtx(t, &isoStubExecutor{}, map[string]interface{}{
		"os_yum_mode": "none",
	}, ol88Aarch64OSInfo())
	if err := EnsureLocalISORepo(ctx); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestEnsureLocalISORepo_skipsWhenAlreadyMountedAndRepoExists(t *testing.T) {
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "mountpoint -q /media"):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "test -f /etc/yum.repos.d/local.repo"):
				return &isoStubResult{exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, map[string]interface{}{
		"os_yum_mode": "local-iso",
	}, ol88Aarch64OSInfo())
	if err := EnsureLocalISORepo(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureLocalISORepo_autoUsesMatchingBlockDevice(t *testing.T) {
	mounted := false
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "mountpoint -q /media"):
				if mounted {
					return &isoStubResult{exit: 0}
				}
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "blkid /dev/cdrom"):
				return &isoStubResult{stdout: "/dev/cdrom: UUID=\"abc\"", exit: 0}
			case strings.Contains(cmd, "blkid /dev/sr0"):
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "mount -t iso9660 /dev/cdrom "+ISOProbeMountpoint):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "cat "+ISOProbeMountpoint+"/.treeinfo"):
				return &isoStubResult{stdout: testTreeinfoOL88Aarch64, exit: 0}
			case strings.Contains(cmd, "umount "+ISOProbeMountpoint):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "mount -t iso9660 /dev/cdrom /media"):
				mounted = true
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "test -f /etc/yum.repos.d/local.repo"):
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "printf"):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "mkdir -p"):
				return &isoStubResult{exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, map[string]interface{}{
		"os_yum_mode":       "local-iso",
		"os_iso_device":     "auto",
		"os_iso_mountpoint": "/media",
		"os_yum_repo_file":  "/etc/yum.repos.d/local.repo",
	}, ol88Aarch64OSInfo())

	if err := EnsureLocalISORepo(ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.Results["iso_path"] != "/dev/cdrom" {
		t.Fatalf("iso_path=%v want /dev/cdrom", ctx.Results["iso_path"])
	}
}

func TestResolveISOPath_autoFallsBackToRemoteISO(t *testing.T) {
	remoteISO := "/data/yashan/soft/OracleLinux-R8-U8-aarch64-dvd.iso"
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.HasPrefix(cmd, "blkid "):
				return &isoStubResult{exit: 1}
			case cmd == "echo $HOME":
				return &isoStubResult{stdout: "/root", exit: 0}
			case strings.Contains(cmd, "ls -1 /data/yashan/soft/*.iso"):
				return &isoStubResult{stdout: remoteISO, exit: 0}
			case strings.Contains(cmd, "mount -o loop "+remoteISO+" "+ISOProbeMountpoint):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "cat "+ISOProbeMountpoint+"/.treeinfo"):
				return &isoStubResult{stdout: testTreeinfoOL88Aarch64, exit: 0}
			case strings.Contains(cmd, "umount "+ISOProbeMountpoint):
				return &isoStubResult{exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	profile := ISOProfileFromOSInfo(ctx.OSInfo)

	got, err := resolveISOPath(ctx, "auto", profile)
	if err != nil {
		t.Fatal(err)
	}
	if got != remoteISO {
		t.Fatalf("got %q want remote ISO path", got)
	}
}

func TestResolveISOPath_blockDeviceMismatchFallsBack(t *testing.T) {
	remoteISO := "/data/yashan/soft/OracleLinux-R8-U8-aarch64-dvd.iso"
	lastLoopISO := ""
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "blkid /dev/sr0"):
				return &isoStubResult{stdout: "UUID=1", exit: 0}
			case strings.Contains(cmd, "mount -t iso9660 /dev/sr0 "+ISOProbeMountpoint):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "mount -o loop"):
				if i := strings.Index(cmd, "mount -o loop "); i >= 0 {
					rest := cmd[i+len("mount -o loop "):]
					lastLoopISO = strings.Fields(rest)[0]
				}
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "cat "+ISOProbeMountpoint+"/.treeinfo"):
				if lastLoopISO == remoteISO {
					return &isoStubResult{stdout: testTreeinfoOL88Aarch64, exit: 0}
				}
				return &isoStubResult{stdout: "[general]\nversion = 8.8\narch = x86_64\n", exit: 0}
			case strings.Contains(cmd, "umount"):
				return &isoStubResult{exit: 0}
			case cmd == "echo $HOME":
				return &isoStubResult{stdout: "/root", exit: 0}
			case strings.Contains(cmd, "ls -1 /data/yashan/soft/*.iso"):
				return &isoStubResult{stdout: remoteISO, exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	got, err := resolveISOPath(ctx, "/dev/sr0", ISOProfileFromOSInfo(ctx.OSInfo))
	if err != nil {
		t.Fatal(err)
	}
	if got != remoteISO {
		t.Fatalf("got %q want fallback remote ISO", got)
	}
}

func TestResolveISOPath_noCandidatesError(t *testing.T) {
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.HasPrefix(cmd, "blkid"):
				return &isoStubResult{exit: 1}
			case cmd == "echo $HOME":
				return &isoStubResult{stdout: "/root", exit: 0}
			case strings.Contains(cmd, "ls -1"):
				return &isoStubResult{exit: 1}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	_, err := resolveISOPath(ctx, "auto", ISOProfileFromOSInfo(ctx.OSInfo))
	if err == nil || !strings.Contains(err.Error(), "no ISO files found") {
		t.Fatalf("expected no ISO files error, got %v", err)
	}
}

func TestRemoteISOSearchDirs(t *testing.T) {
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			if cmd == "echo $HOME" {
				return &isoStubResult{stdout: "/home/yashan", exit: 0}
			}
			return &isoStubResult{exit: 0}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	ctx.RemoteSoftwareDir = "/opt/soft"

	dirs := remoteISOSearchDirs(ctx)
	if len(dirs) != 3 || dirs[0] != "/opt/soft" || dirs[1] != "/home/yashan" || dirs[2] != "/data/yashan/soft" {
		t.Fatalf("unexpected dirs: %v", dirs)
	}
}

func TestDeviceHasMedia(t *testing.T) {
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			if strings.Contains(cmd, "blkid /dev/cdrom") {
				return &isoStubResult{stdout: "UUID=x", exit: 0}
			}
			return &isoStubResult{exit: 1}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	if !deviceHasMedia(ctx, "/dev/cdrom") {
		t.Fatal("expected media on cdrom")
	}
	if deviceHasMedia(ctx, "/dev/sr0") {
		t.Fatal("expected no media on sr0")
	}
}

func TestReadISOMetadataFromMount_mergesSources(t *testing.T) {
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, ".treeinfo"):
				return &isoStubResult{stdout: testTreeinfoOL88Aarch64, exit: 0}
			case strings.Contains(cmd, ".discinfo"):
				return &isoStubResult{stdout: "Oracle Linux Server release 8.8 (aarch64)\n", exit: 0}
			case strings.Contains(cmd, "media.repo"):
				return &isoStubResult{stdout: "version=8.8\n", exit: 0}
			default:
				return &isoStubResult{exit: 1}
			}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	meta := readISOMetadataFromMount(ctx, "/mnt/iso")
	if meta.Major != 8 || meta.Minor != 8 || meta.Arch != "aarch64" {
		t.Fatalf("unexpected merged meta: %+v", meta)
	}
}

func TestEnsureRepoFile_rhel8Creates(t *testing.T) {
	var written string
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "test -f /etc/yum.repos.d/local.repo"):
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "printf"):
				written = cmd
				return &isoStubResult{exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	if err := ensureRepoFile(ctx, "/media", "/etc/yum.repos.d/local.repo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(written, "local-baseos") || !strings.Contains(written, "file:///media/BaseOS") {
		t.Fatalf("unexpected repo content cmd: %s", written)
	}
}

func TestEnsureRepoFile_rhel7Creates(t *testing.T) {
	var written string
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "test -f"):
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "printf"):
				written = cmd
				return &isoStubResult{exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	osInfo := &runner.OSInfo{ID: "rhel", VersionID: "7.9", IsRHEL7: true, PkgManager: "yum"}
	ctx := isoTestCtx(t, exec, nil, osInfo)
	if err := ensureRepoFile(ctx, "/media", "/etc/yum.repos.d/local.repo"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(written, "[local]") || strings.Contains(written, "local-baseos") {
		t.Fatalf("expected rhel7 single repo, got: %s", written)
	}
}

func TestListISOCandidates_localAndRemote(t *testing.T) {
	localDir := t.TempDir()
	localISO := filepath.Join(localDir, "OracleLinux-R8-U8-aarch64-dvd.iso")
	if err := os.WriteFile(localISO, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case cmd == "echo $HOME":
				return &isoStubResult{stdout: "/root", exit: 0}
			case strings.Contains(cmd, "ls -1 /data/yashan/soft/*.iso"):
				return &isoStubResult{stdout: "/data/yashan/soft/CentOS-7-x86_64-DVD.iso", exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	ctx.LocalSoftwareDirs = []string{localDir}

	remote, local, err := listISOCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(remote) != 1 || !strings.Contains(remote[0], "CentOS-7") {
		t.Fatalf("remote=%v", remote)
	}
	if len(local) != 1 || local[0] != "OracleLinux-R8-U8-aarch64-dvd.iso" {
		t.Fatalf("local=%v", local)
	}
}

func TestResolveISOFileForProfile_usesLocalFindAndDistribute(t *testing.T) {
	localDir := t.TempDir()
	isoName := "OracleLinux-R8-U8-aarch64-dvd.iso"
	localISO := filepath.Join(localDir, isoName)
	if err := os.WriteFile(localISO, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantRemote := "/data/yashan/soft/" + isoName
	uploaded := false

	inner := &isoStubExecutor{}
	inner.handler = func(cmd string) *isoStubResult {
		switch {
		case cmd == "echo $HOME":
			return &isoStubResult{stdout: "/root", exit: 0}
		case strings.Contains(cmd, "ls -1"):
			return &isoStubResult{exit: 1}
		case strings.Contains(cmd, "test -d"):
			return &isoStubResult{stdout: "exists", exit: 0}
		case strings.Contains(cmd, "test -f") && strings.Contains(cmd, wantRemote):
			if uploaded {
				return &isoStubResult{stdout: "exists", exit: 0}
			}
			return &isoStubResult{exit: 1}
		case strings.Contains(cmd, "test -f"):
			return &isoStubResult{exit: 1}
		case strings.Contains(cmd, "mount -o loop"):
			return &isoStubResult{exit: 0}
		case strings.Contains(cmd, ".treeinfo"):
			return &isoStubResult{stdout: testTreeinfoOL88Aarch64, exit: 0}
		case strings.Contains(cmd, "umount"), strings.Contains(cmd, "mkdir -p"):
			return &isoStubResult{exit: 0}
		default:
			return &isoStubResult{exit: 0}
		}
	}

	ctx := isoTestCtx(t, inner, nil, ol88Aarch64OSInfo())
	ctx.LocalSoftwareDirs = []string{localDir}
	ctx.Executor = &uploadTrackingExecutor{
		inner:    inner,
		onUpload: func() { uploaded = true },
	}

	got, err := resolveISOFileForProfile(ctx, ISOProfileFromOSInfo(ctx.OSInfo), "")
	if err != nil {
		t.Fatal(err)
	}
	if got != wantRemote {
		t.Fatalf("got %q want %q", got, wantRemote)
	}
	wrapped := ctx.Executor.(*uploadTrackingExecutor)
	if len(wrapped.uploads) != 1 {
		t.Fatalf("expected one upload, got %v", wrapped.uploads)
	}
}

type uploadTrackingExecutor struct {
	inner    *isoStubExecutor
	onUpload func()
	uploads  []string
}

func (e *uploadTrackingExecutor) Execute(cmd string, sudo bool) (runner.ExecResult, error) {
	return e.inner.Execute(cmd, sudo)
}
func (e *uploadTrackingExecutor) Host() string { return e.inner.Host() }
func (e *uploadTrackingExecutor) Close() error { return e.inner.Close() }
func (e *uploadTrackingExecutor) Upload(local, remote string, uc *ssh.UploadContext) error {
	if err := e.inner.Upload(local, remote, uc); err != nil {
		return err
	}
	if e.onUpload != nil {
		e.onUpload()
	}
	e.uploads = append(e.uploads, local+" -> "+remote)
	return nil
}

func TestMountISOAt_commands(t *testing.T) {
	var lastCmd string
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			lastCmd = cmd
			return &isoStubResult{exit: 0}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())

	if err := mountISOAt(ctx, "/dev/sr0", "/media"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lastCmd, "mount -t iso9660 /dev/sr0 /media") {
		t.Fatalf("device mount cmd: %s", lastCmd)
	}

	if err := mountISOAt(ctx, "/data/soft/test.iso", "/media"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lastCmd, "mount -o loop /data/soft/test.iso /media") {
		t.Fatalf("file mount cmd: %s", lastCmd)
	}
}

func TestEnsureLocalISORepoBeforeInstall_delegates(t *testing.T) {
	called := false
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			if strings.Contains(cmd, "mountpoint -q /media") {
				called = true
				return &isoStubResult{exit: 0}
			}
			if strings.Contains(cmd, "test -f") {
				return &isoStubResult{exit: 0}
			}
			return &isoStubResult{exit: 0}
		},
	}
	ctx := isoTestCtx(t, exec, map[string]interface{}{"os_yum_mode": "local-iso"}, ol88Aarch64OSInfo())
	if err := EnsureLocalISORepoBeforeInstall(ctx); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected EnsureLocalISORepoBeforeInstall to invoke mount check")
	}
}

func TestResolveISOPath_specifiedRemoteFileMatches(t *testing.T) {
	remoteISO := "/data/yashan/soft/OracleLinux-R8-U8-aarch64-dvd.iso"
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "test -f") && strings.Contains(cmd, remoteISO):
				return &isoStubResult{stdout: "exists", exit: 0}
			case strings.Contains(cmd, "mount -o loop "+remoteISO):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, ".treeinfo"):
				return &isoStubResult{stdout: testTreeinfoOL88Aarch64, exit: 0}
			case strings.Contains(cmd, "umount"):
				return &isoStubResult{exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	got, err := resolveISOPath(ctx, remoteISO, ISOProfileFromOSInfo(ctx.OSInfo))
	if err != nil {
		t.Fatal(err)
	}
	if got != remoteISO {
		t.Fatalf("got %q", got)
	}
}

func TestResolveMatchingBlockDevice_prefersFirstMatch(t *testing.T) {
	order := []string{}
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "blkid /dev/cdrom"):
				order = append(order, "cdrom")
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "blkid /dev/sr0"):
				order = append(order, "sr0-blkid")
				return &isoStubResult{stdout: "UUID=1", exit: 0}
			case strings.Contains(cmd, "mount -t iso9660 /dev/sr0"):
				order = append(order, "sr0-mount")
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, ".treeinfo"):
				return &isoStubResult{stdout: testTreeinfoOL88Aarch64, exit: 0}
			case strings.Contains(cmd, "umount"):
				return &isoStubResult{exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	path, ok, err := resolveMatchingBlockDevice(ctx, ISOProfileFromOSInfo(ctx.OSInfo), DefaultBlockDevices())
	if err != nil || !ok || path != "/dev/sr0" {
		t.Fatalf("path=%q ok=%v err=%v order=%v", path, ok, err, order)
	}
}

func TestEnsureLocalISORepo_mountFailure(t *testing.T) {
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "mountpoint -q /media"):
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "blkid"):
				return &isoStubResult{stdout: "UUID=x", exit: 0}
			case strings.Contains(cmd, "mount -t iso9660 /dev/cdrom "+ISOProbeMountpoint):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, ".treeinfo"):
				return &isoStubResult{stdout: testTreeinfoOL88Aarch64, exit: 0}
			case strings.Contains(cmd, "mount -t iso9660 /dev/cdrom /media"):
				return &isoStubResult{stderr: "mount failed", exit: 1}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, map[string]interface{}{
		"os_yum_mode":       "local-iso",
		"os_iso_device":     "auto",
		"os_iso_mountpoint": "/media",
	}, ol88Aarch64OSInfo())
	err := EnsureLocalISORepo(ctx)
	if err == nil || !strings.Contains(err.Error(), "failed to mount ISO") {
		t.Fatalf("expected mount failure, got %v", err)
	}
}

func TestFilepathBase(t *testing.T) {
	if filepathBase("/data/yashan/soft/a.iso") != "a.iso" {
		t.Fatal("unix path base failed")
	}
	if filepathBase(`C:\soft\b.iso`) != "b.iso" {
		t.Fatal("windows path base failed")
	}
}

func TestValidateISOFileMatchesProfile(t *testing.T) {
	isoPath := "/data/yashan/soft/test.iso"
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "mount -o loop "+isoPath):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, ".treeinfo"):
				return &isoStubResult{stdout: testTreeinfoOL88Aarch64, exit: 0}
			case strings.Contains(cmd, "umount"):
				return &isoStubResult{exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	ok, err := validateISOFileMatchesProfile(ctx, isoPath, ISOProfileFromOSInfo(ctx.OSInfo))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestResolveISOPath_specifiedFileMismatchSearchesAlternatives(t *testing.T) {
	wrongISO := "/data/yashan/soft/OracleLinux-R8-U8-x86_64-dvd.iso"
	goodISO := "/data/yashan/soft/OracleLinux-R8-U8-aarch64-dvd.iso"
	lastLoopISO := ""
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "test -f") && strings.Contains(cmd, wrongISO):
				return &isoStubResult{stdout: "exists", exit: 0}
			case strings.Contains(cmd, "test -f") && strings.Contains(cmd, goodISO):
				return &isoStubResult{stdout: "exists", exit: 0}
			case strings.Contains(cmd, "mount -o loop"):
				if i := strings.Index(cmd, "mount -o loop "); i >= 0 {
					rest := cmd[i+len("mount -o loop "):]
					lastLoopISO = strings.Fields(rest)[0]
				}
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "cat "+ISOProbeMountpoint+"/.treeinfo"):
				if lastLoopISO == goodISO {
					return &isoStubResult{stdout: testTreeinfoOL88Aarch64, exit: 0}
				}
				return &isoStubResult{stdout: "[general]\nversion = 8.8\narch = x86_64\n", exit: 0}
			case cmd == "echo $HOME":
				return &isoStubResult{stdout: "/root", exit: 0}
			case strings.Contains(cmd, "ls -1 /data/yashan/soft/*.iso"):
				return &isoStubResult{stdout: wrongISO + "\n" + goodISO, exit: 0}
			case strings.Contains(cmd, "umount"):
				return &isoStubResult{exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := isoTestCtx(t, exec, nil, ol88Aarch64OSInfo())
	got, err := resolveISOPath(ctx, wrongISO, ISOProfileFromOSInfo(ctx.OSInfo))
	if err != nil {
		t.Fatal(err)
	}
	if got != goodISO {
		t.Fatalf("got %q want %q", got, goodISO)
	}
}
