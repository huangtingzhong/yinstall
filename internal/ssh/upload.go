package ssh

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"github.com/yinstall/internal/logging"
	"golang.org/x/crypto/ssh"
)

const (
	defaultSFTPBufferSize     = 32 * 1024
	defaultProgressEveryBytes = defaultSFTPBufferSize * 100 // ~3.2 MiB, aligned with oracleinstall
)

// UploadContext 上传日志上下文（进度仅写 debug；起止写 Logger.Info，不进终端）。
type UploadContext struct {
	Logger *logging.Logger
	StepID string
	Host   string
}

// Upload 先 SFTP 上传，失败则降级 SCP；记录起止 Info 与周期 debug 进度。
func (e *SSHExecutor) Upload(localPath, remotePath string, uctx *UploadContext) error {
	return uploadWithSFTPFallback(e.client, e.config.Host, localPath, remotePath, uctx)
}

// Upload 本机分块复制，日志格式与远端 SFTP 一致（method=local）。
func (e *LocalExecutor) Upload(localPath, remotePath string, uctx *UploadContext) error {
	if localPath == remotePath {
		return nil
	}
	stat, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	fileSize := stat.Size()
	host := e.Host()

	logUploadStart(uctx, host, localPath, remotePath, fileSize)
	start := time.Now()

	if err := uploadViaLocalCopy(localPath, remotePath, fileSize, uctx); err != nil {
		return err
	}
	logUploadDebug(uctx, host, "upload method=local")
	logUploadEnd(uctx, host, localPath, remotePath, fileSize, time.Since(start))
	return nil
}

func uploadWithSFTPFallback(client *ssh.Client, host, localPath, remotePath string, uctx *UploadContext) error {
	stat, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	fileSize := stat.Size()

	logUploadStart(uctx, host, localPath, remotePath, fileSize)
	start := time.Now()

	err = uploadViaSFTP(client, localPath, remotePath, fileSize, uctx)
	if err != nil {
		logUploadWarn(uctx, host, "SFTP upload failed, falling back to SCP: %v", err)
		if errSCP := uploadViaSCP(client, localPath, remotePath); errSCP != nil {
			return fmt.Errorf("SFTP failed: %w; SCP failed: %v", err, errSCP)
		}
		logUploadDebug(uctx, host, "upload method=scp (fallback)")
	} else {
		logUploadDebug(uctx, host, "upload method=sftp")
	}

	logUploadEnd(uctx, host, localPath, remotePath, fileSize, time.Since(start))
	return nil
}

func uploadViaSFTP(client *ssh.Client, localPath, remotePath string, fileSize int64, uctx *UploadContext) error {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("create SFTP client: %w", err)
	}
	defer sftpClient.Close()

	if dir := path.Dir(remotePath); dir != "" && dir != "." && dir != "/" {
		if err := sftpClient.MkdirAll(dir); err != nil {
			return fmt.Errorf("mkdir remote dir %s: %w", dir, err)
		}
	}

	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer localFile.Close()

	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote file %s: %w", remotePath, err)
	}
	defer remoteFile.Close()

	if _, err := copyWithUploadProgress(remoteFile, localFile, fileSize, uctx); err != nil {
		return fmt.Errorf("copy to remote file: %w", err)
	}
	return nil
}

func uploadViaLocalCopy(localPath, destPath string, fileSize int64, uctx *UploadContext) error {
	if dir := filepath.Dir(destPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create dest file %s: %w", destPath, err)
	}
	defer dst.Close()

	if _, err := copyWithUploadProgress(dst, src, fileSize, uctx); err != nil {
		return fmt.Errorf("local copy: %w", err)
	}
	return nil
}

// copyWithUploadProgress 分块复制并周期性写入 debug 进度（SFTP / 本机共用）。
func copyWithUploadProgress(w io.Writer, r io.Reader, fileSize int64, uctx *UploadContext) (int64, error) {
	buf := make([]byte, defaultSFTPBufferSize)
	var uploaded int64
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return uploaded, writeErr
			}
			uploaded += int64(n)
			if fileSize > 0 && uploaded%defaultProgressEveryBytes == 0 {
				logUploadProgress(uctx, uploaded, fileSize)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return uploaded, readErr
		}
	}
	if fileSize > 0 && uploaded > 0 && uploaded%defaultProgressEveryBytes != 0 {
		logUploadProgress(uctx, uploaded, fileSize)
	}
	return uploaded, nil
}

func uploadViaSCP(client *ssh.Client, localPath, remotePath string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	w, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	copyErrCh := make(chan error, 1)
	go func() {
		defer w.Close()
		if _, err := fmt.Fprintf(w, "C0644 %d %s\n", stat.Size(), stat.Name()); err != nil {
			copyErrCh <- err
			return
		}
		if _, err := io.Copy(w, file); err != nil {
			copyErrCh <- err
			return
		}
		if _, err := fmt.Fprint(w, "\x00"); err != nil {
			copyErrCh <- err
			return
		}
		copyErrCh <- nil
	}()

	if err := session.Run(fmt.Sprintf("scp -t %s", remotePath)); err != nil {
		return err
	}
	return <-copyErrCh
}

func logUploadStart(uctx *UploadContext, host, localPath, remotePath string, size int64) {
	if uctx == nil || uctx.Logger == nil {
		return
	}
	h := host
	if uctx.Host != "" {
		h = uctx.Host
	}
	uctx.Logger.Info("Uploading %s -> %s:%s (%s)",
		localPath, h, remotePath, formatBytes(size))
}

func logUploadEnd(uctx *UploadContext, host, localPath, remotePath string, size int64, elapsed time.Duration) {
	if uctx == nil || uctx.Logger == nil {
		return
	}
	h := host
	if uctx.Host != "" {
		h = uctx.Host
	}
	rate := ""
	if elapsed > 0 && size > 0 {
		rate = fmt.Sprintf(", avg %s/s", formatBytes(int64(float64(size)/elapsed.Seconds())))
	}
	uctx.Logger.Info("Upload completed %s -> %s:%s (%s) in %s%s",
		localPath, h, remotePath, formatBytes(size), elapsed.Round(time.Millisecond), rate)
}

func logUploadWarn(uctx *UploadContext, host, format string, args ...interface{}) {
	if uctx == nil || uctx.Logger == nil {
		return
	}
	uctx.Logger.Warn(format, args...)
	_ = host
}

func logUploadDebug(uctx *UploadContext, host, msg string) {
	if uctx == nil || uctx.Logger == nil {
		return
	}
	h := host
	if uctx.Host != "" {
		h = uctx.Host
	}
	uctx.Logger.Debug(logging.LogEntry{
		Host:    h,
		StepID:  uctx.StepID,
		Level:   "debug",
		Message: msg,
	})
}

func logUploadProgress(uctx *UploadContext, uploaded, fileSize int64) {
	if uctx == nil || uctx.Logger == nil || fileSize <= 0 {
		return
	}
	h := uctx.Host
	pct := float64(uploaded) * 100 / float64(fileSize)
	uctx.Logger.Debug(logging.LogEntry{
		Host:   h,
		StepID: uctx.StepID,
		Level:  "debug",
		Message: fmt.Sprintf("upload progress: %.1f%% (%s/%s)",
			pct, formatBytes(uploaded), formatBytes(fileSize)),
	})
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// LogUploadStart records upload start (shared by SFTP/WinRM).
func LogUploadStart(uctx *UploadContext, host, localPath, remotePath string, size int64) {
	logUploadStart(uctx, host, localPath, remotePath, size)
}

// LogUploadEnd records upload completion.
func LogUploadEnd(uctx *UploadContext, host, localPath, remotePath string, size int64, elapsed time.Duration) {
	logUploadEnd(uctx, host, localPath, remotePath, size, elapsed)
}

// LogUploadDebug writes upload method/details to debug log.
func LogUploadDebug(uctx *UploadContext, host, msg string) {
	logUploadDebug(uctx, host, msg)
}

// LogUploadProgress writes periodic upload progress to debug log.
func LogUploadProgress(uctx *UploadContext, uploaded, fileSize int64) {
	logUploadProgress(uctx, uploaded, fileSize)
}
