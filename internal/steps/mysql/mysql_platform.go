package mysql

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

func quotedBin(home, name string) string {
	return commonos.ShellSingleQuote(home + "/bin/" + name)
}

// WaitForMysqlReady polls until the instance accepts connections.
// Pass rootPassword when the instance already has a root password (e.g. after clone);
// omit it for fresh init before M-015 (uses mysqladmin ping, ignores ~/.my.cnf).
func WaitForMysqlReady(ctx *runner.StepContext, layout Layout, timeout time.Duration, rootPassword ...string) error {
	socket := layout.Other + "/mysql.sock"
	password := ""
	if len(rootPassword) > 0 {
		password = rootPassword[0]
	}
	start := time.Now()
	deadline := start.Add(timeout)
	lastLog := start
	for time.Now().Before(deadline) {
		var cmd string
		if ctx.GetTargetPlatform() == PlatformWindows {
			homeWin := filepathToSlash(layout.Home)
			cmd = fmt.Sprintf(`powershell -NoProfile -Command "& '%s/bin/mysqladmin.exe' ping --host=127.0.0.1 --port=%d --silent"`, homeWin, layout.Port)
		} else if password != "" {
			cmd = fmt.Sprintf("MYSQL_PWD=%s %s --no-defaults -S %s -uroot -e 'SELECT 1' >/dev/null 2>&1",
				commonos.ShellSingleQuote(password), quotedBin(layout.Home, "mysql"), commonos.ShellSingleQuote(socket))
		} else {
			cmd = fmt.Sprintf("%s --defaults-file=/dev/null ping -S %s --silent",
				quotedBin(layout.Home, "mysqladmin"), commonos.ShellSingleQuote(socket))
		}
		res, _ := ctx.Execute(cmd, false)
		if res != nil && res.GetExitCode() == 0 {
			return nil
		}
		if time.Since(lastLog) >= 30*time.Second {
			ctx.Logger.Info("waiting for mysql ready (port=%d, elapsed=%s)", layout.Port, time.Since(start).Round(time.Second))
			lastLog = time.Now()
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("mysql not ready within %s (port=%d)", timeout, layout.Port)
}

const (
	PlatformLinux   = "linux"
	PlatformDarwin  = "darwin"
	PlatformWindows = "windows"
)

// DetectTargetPlatform probes the target OS (linux/darwin/windows).
func DetectTargetPlatform(ctx *runner.StepContext) string {
	if ctx.GetParamBool("local_mode", false) {
		return localPlatform()
	}

	// Windows OpenSSH often has cmd as shell
	res, _ := ctx.Execute(`cmd /c echo windows_probe`, false)
	if res != nil && strings.Contains(res.GetStdout(), "windows_probe") {
		return PlatformWindows
	}
	res, _ = ctx.Execute(`powershell -NoProfile -Command "Write-Output windows_probe"`, false)
	if res != nil && strings.Contains(res.GetStdout(), "windows_probe") {
		return PlatformWindows
	}

	res, _ = ctx.Execute("uname -s", false)
	if res != nil {
		osName := strings.ToLower(strings.TrimSpace(res.GetStdout()))
		switch {
		case strings.Contains(osName, "darwin"):
			return PlatformDarwin
		case strings.Contains(osName, "linux"):
			return PlatformLinux
		case strings.Contains(osName, "mingw"), strings.Contains(osName, "cygwin"):
			return PlatformWindows
		}
	}
	return PlatformLinux
}

func localPlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return PlatformDarwin
	case "windows":
		return PlatformWindows
	default:
		return PlatformLinux
	}
}

// UseSudo reports whether privileged commands should run with sudo on this host.
func UseSudo(ctx *runner.StepContext) bool {
	return ctx.GetParamBool("sudo", false) && !ctx.GetParamBool("local_mode", false)
}

// StoreTargetPlatform writes platform into shared results for this host.
func StoreTargetPlatform(ctx *runner.StepContext, platform string) {
	host := ""
	if ctx.Executor != nil {
		host = ctx.Executor.Host()
	}
	ctx.SetResult("target_platform", platform)
	if host != "" {
		ctx.SetResult(host+"_target_platform", platform)
	}
	ctx.TargetPlatform = platform
}
