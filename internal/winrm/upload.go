package winrm

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	mwinrm "github.com/masterzen/winrm"
	"github.com/yinstall/internal/ssh"
)

const (
	winrmMaxOpsPerShell     = 24
	winrmCmdBudget          = 8000
	winrmProgressEveryBytes = 32 * 1024 * 100 // ~3.2 MiB, aligned with SFTP upload
)

// uploadViaWinRM copies a local file to remotePath using WinRM base64 chunking (no SSH/SFTP).
func uploadViaWinRM(client *mwinrm.Client, host, localPath, remotePath string, uctx *ssh.UploadContext) error {
	stat, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	fileSize := stat.Size()
	remotePath = normalizeWinPath(remotePath)

	ssh.LogUploadStart(uctx, host, localPath, remotePath, fileSize)
	start := time.Now()

	tempName, err := tempUploadName()
	if err != nil {
		return err
	}
	tempRel := `%TEMP%\` + tempName

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer f.Close()

	if err := uploadContent(client, tempRel, f, fileSize, uctx); err != nil {
		return fmt.Errorf("winrm upload chunks: %w", err)
	}
	if err := restoreContent(client, tempRel, remotePath); err != nil {
		return fmt.Errorf("winrm restore to %s: %w", remotePath, err)
	}
	if err := cleanupContent(client, tempRel); err != nil {
		return fmt.Errorf("winrm cleanup temp: %w", err)
	}

	ssh.LogUploadDebug(uctx, host, "upload method=winrm")
	ssh.LogUploadEnd(uctx, host, localPath, remotePath, fileSize, time.Since(start))
	return nil
}

func tempUploadName() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "yinstall-" + hex.EncodeToString(b) + ".tmp", nil
}

func normalizeWinPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	return strings.ReplaceAll(p, "/", `\`)
}

func psQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func uploadContent(client *mwinrm.Client, tempRel string, r io.Reader, fileSize int64, uctx *ssh.UploadContext) error {
	var totalUploaded int64
	for {
		done, err := uploadChunks(client, tempRel, r, fileSize, uctx, &totalUploaded)
		if err != nil {
			return err
		}
		if done {
			if fileSize > 0 && totalUploaded > 0 {
				ssh.LogUploadProgress(uctx, totalUploaded, fileSize)
			}
			return nil
		}
	}
}

func uploadChunks(client *mwinrm.Client, tempRel string, reader io.Reader, fileSize int64, uctx *ssh.UploadContext, totalUploaded *int64) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	shell, err := client.CreateShell()
	if err != nil {
		return false, fmt.Errorf("create shell: %w", err)
	}
	defer shell.Close()

	chunkSize := ((winrmCmdBudget - len(tempRel)) / 4) * 3
	if chunkSize < 1024 {
		chunkSize = 1024
	}
	chunk := make([]byte, chunkSize)
	var lastLogged int64
	if totalUploaded != nil {
		lastLogged = (*totalUploaded / winrmProgressEveryBytes) * winrmProgressEveryBytes
	}

	for i := 0; i < winrmMaxOpsPerShell; i++ {
		n, readErr := reader.Read(chunk)
		if readErr != nil && readErr != io.EOF {
			return false, readErr
		}
		if n == 0 {
			return true, nil
		}
		content := base64.StdEncoding.EncodeToString(chunk[:n])
		if err := appendContent(ctx, shell, tempRel, content); err != nil {
			return false, err
		}
		if totalUploaded != nil {
			*totalUploaded += int64(n)
			if fileSize > 0 && *totalUploaded-lastLogged >= winrmProgressEveryBytes {
				ssh.LogUploadProgress(uctx, *totalUploaded, fileSize)
				lastLogged = (*totalUploaded / winrmProgressEveryBytes) * winrmProgressEveryBytes
			}
		}
		if readErr == io.EOF {
			return true, nil
		}
	}
	return false, nil
}

func appendContent(ctx context.Context, shell *mwinrm.Shell, tempRel, content string) error {
	// cmd.exe: append one base64 line to temp file (ASCII-only payload).
	cmdLine := fmt.Sprintf(`cmd /c echo %s>>"%s"`, content, tempRel)
	cmd, err := shell.ExecuteWithContext(ctx, cmdLine)
	if err != nil {
		return err
	}
	defer cmd.Close()
	cmd.Wait()
	if err := cmd.Error(); err != nil {
		return err
	}
	if cmd.ExitCode() != 0 {
		return fmt.Errorf("append chunk exit_code=%d", cmd.ExitCode())
	}
	return nil
}

func restoreContent(client *mwinrm.Client, fromRel, toPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	script := fmt.Sprintf(`
$tmp = [System.IO.Path]::GetFullPath('%s')
$dest = [System.IO.Path]::GetFullPath('%s')
if (Test-Path -LiteralPath $dest -PathType Container) { exit 1 }
$destDir = [System.IO.Path]::GetDirectoryName($dest)
if ($destDir -and -not (Test-Path -LiteralPath $destDir)) {
  New-Item -ItemType Directory -Force -Path $destDir | Out-Null
}
if (Test-Path -LiteralPath $dest) { Remove-Item -LiteralPath $dest -Force -ErrorAction SilentlyContinue }
if (Test-Path -LiteralPath $tmp) {
  $reader = [System.IO.File]::OpenText($tmp)
  $writer = [System.IO.File]::OpenWrite($dest)
  try {
    for (;;) {
      $line = $reader.ReadLine()
      if ($null -eq $line) { break }
      $bytes = [System.Convert]::FromBase64String($line)
      $writer.Write($bytes, 0, $bytes.Length)
    }
  } finally {
    $reader.Close()
    $writer.Close()
  }
} else {
  New-Item -ItemType File -Force -Path $dest | Out-Null
}
`, psQuote(fromRel), psQuote(toPath))

	_, stderr, code, err := client.RunPSWithContext(ctx, script)
	if err != nil {
		return err
	}
	if code != 0 {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = "restore failed"
		}
		return fmt.Errorf("restore exit_code=%d: %s", code, msg)
	}
	return nil
}

func cleanupContent(client *mwinrm.Client, tempRel string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	script := fmt.Sprintf(`
$tmp = [System.IO.Path]::GetFullPath('%s')
if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue }
`, psQuote(tempRel))

	_, stderr, code, err := client.RunPSWithContext(ctx, script)
	if err != nil {
		return err
	}
	if code != 0 {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = "cleanup failed"
		}
		return fmt.Errorf("cleanup exit_code=%d: %s", code, msg)
	}
	return nil
}
