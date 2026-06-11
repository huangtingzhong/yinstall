package ssh

import (
	"strings"
)

const (
	TargetOSLinux   = "linux"
	TargetOSWindows = "windows"
	TargetOSDarwin  = "darwin"
)

// wrapSSHCommand builds the remote command for SSH session.Run.
// Windows OpenSSH runs commands in the user's default shell (PowerShell); Unix uses bash -c.
func wrapSSHCommand(cfg Config, command string, sudo bool) string {
	if cfg.TargetOS == TargetOSWindows || isWindowsSSHUser(cfg.User) {
		return command
	}
	escapedCmd := strings.ReplaceAll(command, "'", "'\"'\"'")
	if sudo && cfg.User != "root" {
		return "sudo -n bash -c '" + escapedCmd + "'"
	}
	return "bash -c '" + escapedCmd + "'"
}

func isWindowsSSHUser(user string) bool {
	u := strings.ToLower(strings.TrimSpace(user))
	return u == "administrator" || u == "system"
}
