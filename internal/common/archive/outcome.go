package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Outcome 描述采集/安装存档目录及 R-029 等步骤产出的压缩包路径。
type Outcome struct {
	Dir           string
	ArchivePath   string
	ArchiveFormat string
	SizeBytes     int64 // <0 表示未知
}

// ResolveOutcome 从 sharedResults 与 manifest.json 解析目录与打包文件路径。
func ResolveOutcome(outDir string, sharedResults map[string]interface{}) Outcome {
	o := Outcome{Dir: filepath.Clean(outDir), SizeBytes: -1}
	if sharedResults != nil {
		if arc, ok := sharedResults["archive_path"].(string); ok && strings.TrimSpace(arc) != "" {
			o.ArchivePath = filepath.Clean(arc)
		}
		if fmtName, ok := sharedResults["archive_format"].(string); ok {
			o.ArchiveFormat = fmtName
		}
	}
	if o.ArchivePath == "" && outDir != "" {
		path, format := archivePathFromManifest(outDir)
		o.ArchivePath = path
		if o.ArchiveFormat == "" {
			o.ArchiveFormat = format
		}
	}
	if o.ArchivePath != "" {
		if o.ArchiveFormat == "" {
			o.ArchiveFormat = formatFromPath(o.ArchivePath)
		}
		if st, err := os.Stat(o.ArchivePath); err == nil && !st.IsDir() {
			o.SizeBytes = st.Size()
		}
	}
	return o
}

func archivePathFromManifest(outDir string) (path, format string) {
	b, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		return "", ""
	}
	var m struct {
		ArchivePath   string `json:"archive_path"`
		ArchiveFormat string `json:"archive_format"`
	}
	if json.Unmarshal(b, &m) != nil {
		return "", ""
	}
	return filepath.Clean(m.ArchivePath), m.ArchiveFormat
}

func formatFromPath(archivePath string) string {
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz"):
		return FormatTarGz
	case strings.HasSuffix(archivePath, ".zip"):
		return FormatZip
	default:
		return ""
	}
}

// HumanBytes 将字节数格式化为可读大小（B / KiB / MiB / GiB）。
func HumanBytes(n int64) string {
	if n < 0 {
		return ""
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	suffix := []string{"KiB", "MiB", "GiB", "TiB"}
	if exp >= len(suffix) {
		exp = len(suffix) - 1
	}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), suffix[exp])
}

// PrintTerminalSummary 向终端打印目录与打包文件（供 collect / 安装挂钩共用）。
func PrintTerminalSummary(dirLabel, archiveLabel string, outDir string, sharedResults map[string]interface{}) {
	o := ResolveOutcome(outDir, sharedResults)
	fmt.Printf("%s: %s\n", dirLabel, o.Dir)
	if o.ArchivePath == "" {
		return
	}
	line := fmt.Sprintf("%s: %s", archiveLabel, o.ArchivePath)
	if o.ArchiveFormat != "" {
		line += fmt.Sprintf(" (%s", o.ArchiveFormat)
		if o.SizeBytes >= 0 {
			line += ", " + HumanBytes(o.SizeBytes)
		}
		line += ")"
	} else if o.SizeBytes >= 0 {
		line += fmt.Sprintf(" (%s)", HumanBytes(o.SizeBytes))
	}
	fmt.Println(line)
}

// LogSummary 写入 session/debug Info 行（与 PrintTerminalSummary 信息一致）。
func LogSummary(logger interface {
	Info(format string, args ...interface{})
}, outDir string, sharedResults map[string]interface{}) {
	o := ResolveOutcome(outDir, sharedResults)
	logger.Info("Archive directory: %s", o.Dir)
	if o.ArchivePath == "" {
		return
	}
	if o.SizeBytes >= 0 && o.ArchiveFormat != "" {
		logger.Info("Packaged archive: %s format=%s size=%s", o.ArchivePath, o.ArchiveFormat, HumanBytes(o.SizeBytes))
		return
	}
	if o.SizeBytes >= 0 {
		logger.Info("Packaged archive: %s size=%s", o.ArchivePath, HumanBytes(o.SizeBytes))
		return
	}
	logger.Info("Packaged archive: %s", o.ArchivePath)
}
