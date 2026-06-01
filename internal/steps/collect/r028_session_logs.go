// r028_session_logs.go - 会话日志归档
// 将本次 yinstall 运行的控制端 session/debug 日志复制到归档目录 logs/ 子目录。
// 独立 collect 与安装流程末尾挂钩 collect 共用同一 Logger，无需额外开关。
package collect

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// StepR028SessionLogs 返回 R-028 步骤：复制会话日志到归档（Optional：拷贝失败记 warning）。
func StepR028SessionLogs() *runner.Step {
	return &runner.Step{
		ID:       "R-028",
		Name:     "Archive session logs",
		Optional: true,
		Action: func(ctx *runner.StepContext) error {
			rootDir := collectRootDir(ctx)
			logsDir := filepath.Join(rootDir, "logs")
			if err := os.MkdirAll(logsDir, 0o755); err != nil {
				return fmt.Errorf("mkdir logs dir: %w", err)
			}

			sessionLog := ctx.Logger.SessionLogPath()
			debugLog := ctx.Logger.DebugLogPath()

			for _, src := range []string{sessionLog, debugLog} {
				if src == "" {
					continue
				}
				dest := filepath.Join(logsDir, filepath.Base(src))
				if err := copyLocalFile(src, dest); err != nil {
					appendWarning(ctx, "R-028", fmt.Sprintf("copy log %s: %v", src, err))
				} else {
					ctx.Logger.Info("[R-028] session log archived: %s -> %s", src, dest)
				}
			}
			return nil
		},
	}
}

// copyLocalFile 在控制端本地复制文件。
func copyLocalFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
