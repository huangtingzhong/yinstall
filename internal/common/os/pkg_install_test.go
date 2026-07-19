package os

import (
	"strings"
	"testing"

	"github.com/yinstall/internal/runner"
)

func pkgInstallCtx(t *testing.T, exec *isoStubExecutor, params map[string]interface{}) *runner.StepContext {
	t.Helper()
	return isoTestCtx(t, exec, params, ol88Aarch64OSInfo())
}

func TestInstallPackages_systemSuccess(t *testing.T) {
	installCalled := false
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			if strings.Contains(cmd, "dnf -y install lz4") && !strings.Contains(cmd, "disablerepo") {
				installCalled = true
				return &isoStubResult{exit: 0}
			}
			if strings.Contains(cmd, "rpm -q lz4") || strings.Contains(cmd, "dnf list installed lz4") {
				return &isoStubResult{exit: 0, stdout: "lz4"}
			}
			return &isoStubResult{exit: 1}
		},
	}
	ctx := pkgInstallCtx(t, exec, map[string]interface{}{"os_yum_mode": ""})
	if err := InstallPackages(ctx, "lz4"); err != nil {
		t.Fatalf("InstallPackages: %v", err)
	}
	if !installCalled {
		t.Fatal("expected system repo install")
	}
}

func TestInstallPackages_localModeUsesLocalRepo(t *testing.T) {
	var cmds []string
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			cmds = append(cmds, cmd)
			switch {
			case strings.Contains(cmd, "mountpoint -q /media"):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "test -f /etc/yum.repos.d/local.repo"):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "disablerepo"):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "rpm -q lz4") || strings.Contains(cmd, "dnf list installed lz4"):
				return &isoStubResult{exit: 0, stdout: "lz4"}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := pkgInstallCtx(t, exec, map[string]interface{}{"os_yum_mode": "local"})
	if err := InstallPackages(ctx, "lz4"); err != nil {
		t.Fatalf("InstallPackages: %v", err)
	}
	foundLocal := false
	for _, c := range cmds {
		if strings.Contains(c, "disablerepo") && strings.Contains(c, "local-baseos") {
			foundLocal = true
			break
		}
	}
	if !foundLocal {
		t.Fatalf("expected local repo install, cmds=%v", cmds)
	}
}

func TestInstallPackages_fallbackWhenSystemFailsAndMediaAvailable(t *testing.T) {
	systemTried := false
	localTried := false
	installed := false
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "dnf -y install") && strings.Contains(cmd, "disablerepo"):
				localTried = true
				installed = true
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "dnf -y install"):
				systemTried = true
				return &isoStubResult{exit: 1, stderr: "Error: Unable to find a match: lz4"}
			case strings.Contains(cmd, "rpm -q"):
				if installed {
					return &isoStubResult{exit: 0, stdout: "lz4-1.0\n"}
				}
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "blkid"):
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "ls -1 /data/yashan/soft/*.iso"):
				return &isoStubResult{exit: 0, stdout: "/data/yashan/soft/OracleLinux-R8-U8-aarch64-dvd.iso\n"}
			case strings.Contains(cmd, "mount -o loop") && strings.Contains(cmd, ISOProbeMountpoint):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "cat "+ISOProbeMountpoint+"/.treeinfo"):
				return &isoStubResult{exit: 0, stdout: testTreeinfoOL88Aarch64}
			case strings.Contains(cmd, "mountpoint -q /media"):
				if strings.Contains(cmd, "2>/dev/null") {
					return &isoStubResult{exit: 1}
				}
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "mount -o loop") && strings.Contains(cmd, "/media"):
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "test -f /etc/yum.repos.d/local.repo"):
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "printf"):
				return &isoStubResult{exit: 0}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := pkgInstallCtx(t, exec, map[string]interface{}{"os_yum_mode": ""})
	if err := InstallPackages(ctx, "lz4"); err != nil {
		t.Fatalf("InstallPackages fallback: %v", err)
	}
	if !systemTried {
		t.Fatal("expected system install attempt")
	}
	if !localTried {
		t.Fatal("expected local media fallback install")
	}
}

func TestInstallPackages_fallbackUnavailableReturnsError(t *testing.T) {
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "dnf -y install lz4"):
				return &isoStubResult{exit: 1, stderr: "Error: Unable to find a match: lz4"}
			case strings.Contains(cmd, "blkid"):
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "ls -1"):
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "rpm -q lz4"):
				return &isoStubResult{exit: 1}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := pkgInstallCtx(t, exec, map[string]interface{}{"os_yum_mode": ""})
	if err := InstallPackages(ctx, "lz4"); err == nil {
		t.Fatal("expected error when fallback media unavailable")
	}
}

func TestInstallPackages_ignoreErrorsContinues(t *testing.T) {
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			if strings.Contains(cmd, "dnf -y install") {
				return &isoStubResult{exit: 1, stderr: "Error: Unable to find a match"}
			}
			if strings.Contains(cmd, "rpm -q") {
				return &isoStubResult{exit: 1}
			}
			if strings.Contains(cmd, "ls -1") {
				return &isoStubResult{exit: 1}
			}
			return &isoStubResult{exit: 0}
		},
	}
	ctx := pkgInstallCtx(t, exec, map[string]interface{}{
		"os_yum_mode":              "",
		"os_ignore_install_errors": true,
	})
	if err := InstallPackages(ctx, "lz4 missingpkg"); err != nil {
		t.Fatalf("expected nil with ignore errors, got %v", err)
	}
}

func TestInstallPackages_httpModeWritesRepoAndInstalls(t *testing.T) {
	var cmds []string
	installed := false
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			cmds = append(cmds, cmd)
			switch {
			case strings.Contains(cmd, "printf") && strings.Contains(cmd, DefaultHTTPYumRepoFile):
				if !strings.Contains(cmd, "/BaseOS/") || !strings.Contains(cmd, "/AppStream/") {
					t.Fatalf("repo content missing DVD BaseOS/AppStream paths: %s", cmd)
				}
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "dnf -y install") && strings.Contains(cmd, "yinstall-baseos"):
				installed = true
				return &isoStubResult{exit: 0}
			case strings.Contains(cmd, "rpm -q lz4"):
				if installed {
					return &isoStubResult{exit: 0, stdout: "lz4-1.0\n"}
				}
				return &isoStubResult{exit: 1}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := pkgInstallCtx(t, exec, map[string]interface{}{"os_yum_mode": "10.10.10.20"})
	if err := InstallPackages(ctx, "lz4"); err != nil {
		t.Fatalf("InstallPackages http: %v", err)
	}
	if !installed {
		t.Fatalf("expected http repo install, cmds=%v", cmds)
	}
}

func TestLocalMediaAvailable_withRemoteISO(t *testing.T) {
	exec := &isoStubExecutor{
		handler: func(cmd string) *isoStubResult {
			switch {
			case strings.Contains(cmd, "blkid"):
				return &isoStubResult{exit: 1}
			case strings.Contains(cmd, "ls -1 /data/yashan/soft/*.iso"):
				return &isoStubResult{exit: 0, stdout: "/data/yashan/soft/OracleLinux-R8-U8-aarch64-dvd.iso\n"}
			default:
				return &isoStubResult{exit: 0}
			}
		},
	}
	ctx := pkgInstallCtx(t, exec, nil)
	if !LocalMediaAvailable(ctx) {
		t.Fatal("expected ISO candidate to make media available")
	}
}
