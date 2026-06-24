package mssql

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

// psEsc escapes a PS single-quoted literal (legacy alias; new code uses psEscape).
func psEsc(s string) string { return psEscape(s) }

// PublishCertToAdminShare copies the local certificate file to the partner's
// admin share via net use + Copy-Item. Single-line PS command; errors propagate.
func PublishCertToAdminShare(ctx *runner.StepContext, label, selfCert, partnerShareCert, partnerKey, user, pass string) error {
	if ctx == nil {
		return nil
	}
	if ctx.DryRun {
		return nil
	}
	partnerUNC := AdminShareUNC(partnerKey)
	destDir := partnerShareCert
	if idx := strings.LastIndexByte(partnerShareCert, '\\'); idx >= 0 {
		destDir = partnerShareCert[:idx]
	}
	cmd := fmt.Sprintf(
		`$p=%s; $u=%s; $pw=%s; $dest=%s; $src=%s; if ($pw) { net use $p /user:$u $pw 2>$null | Out-Null }; New-Item -ItemType Directory -Force -Path $dest | Out-Null; Copy-Item -LiteralPath $src -Destination %s -Force`,
		psSingleQuote(partnerUNC), psSingleQuote(user), psSingleQuote(pass),
		psSingleQuote(destDir), psSingleQuote(selfCert), psSingleQuote(partnerShareCert),
	)
	return runPSCommand(ctx, label, cmd)
}

// ImportCertFromPartner fetches a partner certificate from admin share to a
// local path, retrying until the file is present and sizes match (3 min timeout).
// The retry/wait loop runs in Go (not PowerShell) so we can interleave logging
// and timeout precisely.
func ImportCertFromPartner(ctx *runner.StepContext, label, partnerCertLocal, partnerCertRemote, partnerKey, user, pass, sqlAccount string) error {
	if ctx == nil {
		return nil
	}
	if ctx.DryRun {
		return nil
	}
	partnerUNC := AdminShareUNC(partnerKey)
	if err := fetchRemoteFileWithRetry(ctx, label, partnerCertLocal, partnerCertRemote, partnerUNC, user, pass, sqlAccount, true); err != nil {
		return err
	}
	return grantCertFileAccess(ctx, label+" icacls", partnerCertLocal, sqlAccount)
}

// FetchBackupFromPrimary fetches a backup file from the primary's admin share
// to a local path, retrying until the file is present and sizes match.
// Same Go-loop pattern as ImportCertFromPartner.
func FetchBackupFromPrimary(ctx *runner.StepContext, label, localBackup, remoteBackup, primaryHost, user, pass, sqlAccount string) error {
	if ctx == nil {
		return nil
	}
	if ctx.DryRun {
		return nil
	}
	partnerUNC := AdminShareUNC(primaryHost)
	if err := fetchRemoteFileWithRetry(ctx, label, localBackup, remoteBackup, partnerUNC, user, pass, sqlAccount, false); err != nil {
		return err
	}
	return grantBackupFileAccess(ctx, label+" icacls", localBackup, sqlAccount)
}

// fetchRemoteFileWithRetry loops Test-Path + Copy-Item until the local file
// exists and matches the remote size, or the deadline expires.
// expectedAccount is granted full control on success when non-empty.
func fetchRemoteFileWithRetry(ctx *runner.StepContext, label, localPath, remotePath, partnerUNC, user, pass, _ string, _ bool) error {
	// Prepare: remove stale local file, mkdir parent, mount admin share.
	prepareCmd := fmt.Sprintf(
		`$local=%s; $remote=%s; $unc=%s; $u=%s; $pw=%s; Remove-Item -LiteralPath $local -Force -ErrorAction SilentlyContinue; $d=Split-Path -Parent $local; New-Item -ItemType Directory -Force -Path $d | Out-Null; if ($pw) { net use $unc /user:$u $pw 2>$null | Out-Null }`,
		psSingleQuote(localPath), psSingleQuote(remotePath),
		psSingleQuote(partnerUNC), psSingleQuote(user), psSingleQuote(pass),
	)
	if err := runPSCommand(ctx, label+" prepare", prepareCmd); err != nil {
		return err
	}

	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		ok, err := remoteFileReady(ctx, label, localPath, remotePath)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		// Try copy again (partner may still be writing the file).
		copyCmd := fmt.Sprintf(
			`$local=%s; $remote=%s; if (Test-Path -LiteralPath $remote) { Copy-Item -LiteralPath $remote -Destination $local -Force }`,
			psSingleQuote(localPath), psSingleQuote(remotePath),
		)
		if err := runPSCommand(ctx, label+" copy", copyCmd); err != nil {
			return err
		}
		time.Sleep(5 * time.Second)
	}
	// Final check
	ok, err := remoteFileReady(ctx, label, localPath, remotePath)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s: partner file not available after retry: %s", label, remotePath)
	}
	return nil
}

// remoteFileReady reports whether local exists, both files have matching length.
func remoteFileReady(ctx *runner.StepContext, label, localPath, remotePath string) (bool, error) {
	cmd := fmt.Sprintf(
		`$local=%s; $remote=%s; if ((Test-Path -LiteralPath $local) -and ((Get-Item -LiteralPath $local).Length -gt 0)) { $r=(Get-Item -LiteralPath $remote).Length; $l=(Get-Item -LiteralPath $local).Length; if ($l -eq $r) { '1' } else { '0' } } else { '0' }`,
		psSingleQuote(localPath), psSingleQuote(remotePath),
	)
	out, err := runPSCommandScalar(ctx, label+" ready", cmd)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "1", nil
}

// grantCertFileAccess grants SQL service account + SYSTEM full control on cert dir + file.
func grantCertFileAccess(ctx *runner.StepContext, label, localPath, sqlAccount string) error {
	if sqlAccount == "" {
		return nil
	}
	dir := localPath
	if idx := strings.LastIndexByte(localPath, '\\'); idx >= 0 {
		dir = localPath[:idx]
	}
	cmd := fmt.Sprintf(
		`$d=%s; $f=%s; $acct=%s; icacls $d /grant ($acct+':(OI)(CI)F') 2>$null | Out-Null; icacls $f /grant ($acct+':F') 2>$null | Out-Null; icacls $f /grant 'NT AUTHORITY\SYSTEM:F' 2>$null | Out-Null`,
		psSingleQuote(dir), psSingleQuote(localPath), psSingleQuote(sqlAccount),
	)
	return runPSCommand(ctx, label, cmd)
}

// grantBackupFileAccess grants SQL service account + SYSTEM full control on backup dir + file.
func grantBackupFileAccess(ctx *runner.StepContext, label, localPath, sqlAccount string) error {
	if sqlAccount == "" {
		return nil
	}
	dir := localPath
	if idx := strings.LastIndexByte(localPath, '\\'); idx >= 0 {
		dir = localPath[:idx]
	}
	cmd := fmt.Sprintf(
		`$d=%s; $f=%s; $acct=%s; icacls $d /grant ($acct+':(OI)(CI)F') 2>$null | Out-Null; icacls $d /grant 'NT AUTHORITY\SYSTEM:(OI)(CI)F' 2>$null | Out-Null; icacls $f /grant ($acct+':F') 2>$null | Out-Null; icacls $f /grant 'NT AUTHORITY\SYSTEM:F' 2>$null | Out-Null`,
		psSingleQuote(dir), psSingleQuote(localPath), psSingleQuote(sqlAccount),
	)
	return runPSCommand(ctx, label, cmd)
}
