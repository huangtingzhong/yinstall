package winrm

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	mwinrm "github.com/masterzen/winrm"
)

const downloadChunkSize = 24000

func downloadViaWinRM(client *mwinrm.Client, remotePath, localPath string) error {
	remotePath = normalizeWinPath(remotePath)
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("mkdir local dir: %w", err)
	}
	out, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer out.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	offset := int64(0)
	for {
		script := fmt.Sprintf(`
$path = '%s'
if (-not (Test-Path -LiteralPath $path)) { throw "missing: $path" }
$fs = [System.IO.File]::OpenRead($path)
try {
  $null = $fs.Seek(%d, [System.IO.SeekOrigin]::Begin)
  $buf = New-Object byte[] %d
  $n = $fs.Read($buf, 0, $buf.Length)
  if ($n -le 0) { 'EOF'; return }
  [Convert]::ToBase64String($buf, 0, $n)
} finally { $fs.Close() }
`, psQuote(remotePath), offset, downloadChunkSize)
		stdout, stderr, code, err := client.RunPSWithContext(ctx, script)
		if err != nil {
			return err
		}
		if code != 0 {
			msg := strings.TrimSpace(stderr)
			if msg == "" {
				msg = "read chunk failed"
			}
			return fmt.Errorf("download chunk exit_code=%d: %s", code, msg)
		}
		chunk := strings.TrimSpace(stdout)
		if chunk == "EOF" || chunk == "" {
			break
		}
		data, err := base64.StdEncoding.DecodeString(chunk)
		if err != nil {
			return fmt.Errorf("decode chunk: %w", err)
		}
		if _, err := out.Write(data); err != nil {
			return err
		}
		offset += int64(len(data))
		if len(data) < downloadChunkSize {
			break
		}
	}
	return nil
}
