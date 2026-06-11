package file

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func isWindowsTarget(ctx *runner.StepContext) bool {
	return ctx != nil && ctx.GetTargetPlatform() == "windows"
}

func psQuotePath(p string) string {
	return strings.ReplaceAll(p, `'`, `''`)
}

func toSlashPath(p string) string {
	return strings.ReplaceAll(p, `\`, `/`)
}

// RemoteWriteTextFile writes UTF-8 text to a remote file (Windows via base64; Unix via heredoc).
func RemoteWriteTextFile(ctx *runner.StepContext, remotePath, content string, sudo bool) error {
	if isWindowsTarget(ctx) {
		q := psQuotePath(toSlashPath(remotePath))
		b64 := base64.StdEncoding.EncodeToString([]byte(content))
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "[IO.File]::WriteAllText('%s', [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s')))"`, q, b64)
		res, err := ctx.ExecuteWithCheck(cmd, false)
		if err != nil {
			return err
		}
		if res != nil && res.GetExitCode() != 0 {
			return fmt.Errorf("failed to write %s: %s", remotePath, res.GetStderr())
		}
		return nil
	}
	cmd := fmt.Sprintf("cat > %s << 'EOF'\n%sEOF", shellSingleQuote(remotePath), content)
	_, err := ctx.ExecuteWithCheck(cmd, sudo)
	return err
}

// RemoteHomeDir returns the remote user's home directory.
func RemoteHomeDir(ctx *runner.StepContext) string {
	if isWindowsTarget(ctx) {
		res, _ := ctx.Execute(`powershell -NoProfile -Command "Write-Output $env:USERPROFILE"`, false)
		if res != nil && strings.TrimSpace(res.GetStdout()) != "" {
			return strings.TrimSpace(res.GetStdout())
		}
		return `C:\Users\Administrator`
	}
	res, _ := ctx.Execute("echo $HOME", false)
	if res != nil && strings.TrimSpace(res.GetStdout()) != "" {
		return strings.TrimSpace(res.GetStdout())
	}
	return "/root"
}

// RemoteFileExists checks whether a remote file exists.
func RemoteFileExists(ctx *runner.StepContext, p string) bool {
	if isWindowsTarget(ctx) {
		q := psQuotePath(toSlashPath(p))
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "if (Test-Path -LiteralPath '%s' -PathType Leaf) { 'exists' }"`, q)
		res, _ := ctx.Execute(cmd, false)
		return res != nil && strings.Contains(res.GetStdout(), "exists")
	}
	res, _ := ctx.Execute(fmt.Sprintf("test -f %s && echo 'exists'", shellSingleQuote(p)), false)
	return res != nil && strings.Contains(res.GetStdout(), "exists")
}

// RemoteDirExists checks whether a remote directory exists.
func RemoteDirExists(ctx *runner.StepContext, p string) bool {
	if isWindowsTarget(ctx) {
		q := psQuotePath(toSlashPath(p))
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "if (Test-Path -LiteralPath '%s' -PathType Container) { 'exists' }"`, q)
		res, _ := ctx.Execute(cmd, false)
		return res != nil && strings.Contains(res.GetStdout(), "exists")
	}
	res, _ := ctx.Execute(fmt.Sprintf("test -d %s && echo 'exists'", shellSingleQuote(p)), false)
	return res != nil && strings.Contains(res.GetStdout(), "exists")
}

// RemoteEnsureDir creates a remote directory tree if missing.
func RemoteEnsureDir(ctx *runner.StepContext, p string, sudo bool) error {
	if isWindowsTarget(ctx) {
		q := psQuotePath(toSlashPath(p))
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "New-Item -ItemType Directory -Force -Path '%s' | Out-Null"`, q)
		res, err := ctx.Execute(cmd, false)
		if err != nil {
			return err
		}
		if res != nil && res.GetExitCode() != 0 {
			return fmt.Errorf("failed to create directory '%s': %s", p, res.GetStderr())
		}
		return nil
	}
	res, err := ctx.Execute(fmt.Sprintf("mkdir -p %s", shellSingleQuote(p)), sudo)
	if err != nil {
		return err
	}
	if res != nil && res.GetExitCode() != 0 {
		return fmt.Errorf("failed to create directory '%s': %s", p, res.GetStderr())
	}
	return nil
}

// RemoteRemovePath removes a remote file or directory tree.
func RemoteRemovePath(ctx *runner.StepContext, p string, sudo bool) error {
	if isWindowsTarget(ctx) {
		q := psQuotePath(toSlashPath(p))
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "if (Test-Path -LiteralPath '%s') { Remove-Item -LiteralPath '%s' -Recurse -Force -ErrorAction Stop }"`, q, q)
		res, err := ctx.Execute(cmd, false)
		if err != nil {
			return err
		}
		if res != nil && res.GetExitCode() != 0 {
			return fmt.Errorf("failed to remove '%s': %s", p, res.GetStderr())
		}
		return nil
	}
	res, err := ctx.Execute(fmt.Sprintf("rm -rf %s", shellSingleQuote(p)), sudo)
	if err != nil {
		return err
	}
	if res != nil && res.GetExitCode() != 0 {
		return fmt.Errorf("failed to remove '%s': %s", p, res.GetStderr())
	}
	return nil
}

// RemoteFileSize returns remote file size in bytes, or -1 if unavailable.
func RemoteFileSize(ctx *runner.StepContext, p string) int64 {
	if isWindowsTarget(ctx) {
		q := psQuotePath(toSlashPath(p))
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "(Get-Item -LiteralPath '%s').Length"`, q)
		res, _ := ctx.Execute(cmd, false)
		if res == nil || res.GetExitCode() != 0 {
			return -1
		}
		var size int64
		if _, err := fmt.Sscanf(strings.TrimSpace(res.GetStdout()), "%d", &size); err != nil {
			return -1
		}
		return size
	}
	res, _ := ctx.Execute(fmt.Sprintf("stat -c %%s %s 2>/dev/null", shellSingleQuote(p)), false)
	if res == nil || res.GetExitCode() != 0 {
		return -1
	}
	var size int64
	if _, err := fmt.Sscanf(strings.TrimSpace(res.GetStdout()), "%d", &size); err != nil {
		return -1
	}
	return size
}
