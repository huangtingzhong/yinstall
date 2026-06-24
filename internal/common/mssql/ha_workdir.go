package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	// HAWorkSubdir under SQL default backup directory (shared HA work root).
	HAWorkSubdir = "yinstall_ha"
	// HACertSub is cert export subdirectory under work dir.
	HACertSub = "certs"
)

// DiscoverHAWorkDirSQL returns backup directory via xp_instance_regread.
func DiscoverHAWorkDirSQL() string {
	return DiscoverMirrorWorkDirSQL()
}

func haWorkDirResultKey(hostKey string) string {
	return "ha_work_dir_" + HAHostKey(hostKey)
}

// HAWorkDirResultKey returns Results key for a host-specific HA work directory.
func HAWorkDirResultKey(hostKey string) string {
	return haWorkDirResultKey(hostKey)
}

// SetHAWorkDir stores per-host work dir in Results (ha_* and mirror_* keys for compatibility).
func SetHAWorkDir(ctx *runner.StepContext, hostKey, workDir string) {
	if ctx == nil {
		return
	}
	workDir = strings.TrimRight(strings.TrimSpace(workDir), `\`)
	hostKey = HAHostKey(hostKey)
	if hostKey == "" {
		return
	}
	ctx.SetResult(haWorkDirResultKey(hostKey), workDir)
	ctx.SetResult(mirrorWorkDirResultKey(hostKey), workDir)
}

// HAWorkDirForHost returns HA work root for a specific host (from Results or registry).
func HAWorkDirForHost(ctx *runner.StepContext, hostKey string) string {
	hostKey = strings.TrimSpace(hostKey)
	if hostKey != "" {
		if v, ok := ctx.Results[haWorkDirResultKey(hostKey)].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimRight(strings.TrimSpace(v), `\`)
		}
		if v, ok := ctx.Results[mirrorWorkDirResultKey(hostKey)].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimRight(strings.TrimSpace(v), `\`)
		}
	}
	if ctx != nil && ctx.Results != nil && hostKey != "" {
		if entry, ok := ctx.Results[RegistryEntryResultKey(hostKey)].(InstanceRegistryEntry); ok {
			layout := LayoutFromRegistryEntry(entry)
			if layout.BackupDir != "" {
				return joinWinPath(layout.BackupDir, HAWorkSubdir)
			}
		}
	}
	self := ""
	if ctx != nil && ctx.Executor != nil {
		self = strings.TrimSpace(ctx.Executor.Host())
	}
	if hostKey == "" || strings.EqualFold(hostKey, self) {
		return haWorkDirLocal(ctx)
	}
	return DefaultHAWorkDirFallback()
}

// HAWorkDir returns HA work root on the current executor host.
func HAWorkDir(ctx *runner.StepContext) string {
	if ctx != nil && ctx.Executor != nil {
		return HAWorkDirForHost(ctx, ctx.Executor.Host())
	}
	return haWorkDirLocal(ctx)
}

func haWorkDirLocal(ctx *runner.StepContext) string {
	if ctx != nil {
		if v := strings.TrimSpace(ctx.GetParamString("mirror_work_dir", "")); v != "" {
			return strings.TrimRight(v, `\`)
		}
		if ctx.Executor != nil {
			hostKey := HAHostKey(ctx.Executor.Host())
			if v, ok := ctx.Results[haWorkDirResultKey(hostKey)].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimRight(strings.TrimSpace(v), `\`)
			}
			if entry, ok := RegistryEntryFromContext(ctx); ok {
				layout := LayoutFromRegistryEntry(entry)
				if layout.BackupDir != "" {
					return joinWinPath(layout.BackupDir, HAWorkSubdir)
				}
			}
		}
	}
	return DefaultHAWorkDirFallback()
}

// HACertDirForHost returns cert dir under layoutHost's backup work dir.
func HACertDirForHost(ctx *runner.StepContext, layoutHost string) string {
	return joinWinPath(HAWorkDirForHost(ctx, layoutHost), HACertSub)
}

// HACertDir returns cert export directory on the current host.
func HACertDir(ctx *runner.StepContext) string {
	layoutHost := ""
	if ctx != nil && ctx.Executor != nil {
		layoutHost = ctx.Executor.Host()
	}
	return HACertDirForHost(ctx, layoutHost)
}

// HACertFileForHost returns cert file path on layoutHost (certHostKey in filename).
func HACertFileForHost(ctx *runner.StepContext, layoutHost, certHostKey string) string {
	return haCertFilePath(ctx, layoutHost, certHostKey)
}

// HACertFile returns local cert file path (current host layout, certHostKey in filename).
func HACertFile(ctx *runner.StepContext, certHostKey string) string {
	layoutHost := ""
	if ctx != nil && ctx.Executor != nil {
		layoutHost = ctx.Executor.Host()
	}
	return haCertFilePath(ctx, layoutHost, certHostKey)
}

func haCertFilePath(ctx *runner.StepContext, layoutHost, certHostKey string) string {
	key := strings.ReplaceAll(strings.TrimSpace(certHostKey), ":", "_")
	key = strings.ReplaceAll(key, `\`, "_")
	key = strings.ReplaceAll(key, ".", "_")
	return joinWinPath(HACertDirForHost(ctx, layoutHost), key+".cer")
}

// AdminShareHACertPath returns UNC path to certHostKey's cert on shareHost admin share.
func AdminShareHACertPath(ctx *runner.StepContext, shareHost, certHostKey string) string {
	rel := strings.TrimPrefix(haCertFilePath(ctx, shareHost, certHostKey), `C:\`)
	return AdminShareUNC(shareHost) + `\` + rel
}

// ParseHAWorkDirFromSqlcmd extracts backup dir from DiscoverHAWorkDirSQL output.
func ParseHAWorkDirFromSqlcmd(stdout string) (string, error) {
	base := strings.TrimSpace(stdout)
	for _, line := range strings.Split(base, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.EqualFold(line, "backup_dir") && !IsSqlcmdMetaLine(line) {
			return line, nil
		}
	}
	return "", fmt.Errorf("cannot discover SQL backup directory from sqlcmd output")
}

// DefaultHAWorkDirFallback returns fallback work dir when discovery fails.
func DefaultHAWorkDirFallback() string {
	return joinWinPath(`C:\Program Files\Microsoft SQL Server\MSSQL13.MSSQLSERVER\MSSQL\Backup`, HAWorkSubdir)
}
