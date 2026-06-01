// s010_post_snapshot.go - 终止后台 OS 性能采集并下载数据
// 读取 S-04 写入 ctx.Results 的远端采集目录路径，向所有后台采集进程发送 SIGTERM，
// 等待 3s 后依次下载各日志文件到本地归档的 runtime/bg/ 目录，最后清理远端临时目录。
package stressos

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

// s10ResolvePerfDir 优先从 ctx.Results 读取 perf_remote_dir；
// 若不存在（如 -s S-10 独立调用），则从远端 breadcrumb 文件兜底读取。
func s10ResolvePerfDir(ctx *runner.StepContext) string {
	if v, ok := ctx.Results[perfRemoteDirKey].(string); ok && v != "" {
		return v
	}
	// 兜底：读取 S-04 写入的 breadcrumb 文件
	r, err := stressExecute(ctx,
		"cat "+perfBreadcrumbFile+" 2>/dev/null || echo ''",
		false, 10*time.Second)
	if err != nil || r == nil {
		return ""
	}
	return strings.TrimSpace(r.GetStdout())
}

// StepS10StopPerfCollect 返回 S-10 步骤：终止后台 OS 性能采集并下载结果。
func StepS10StopPerfCollect() *runner.Step {
	return &runner.Step{
		ID:   "S-10",
		Name: "Stop background OS performance collection",
		Action: func(ctx *runner.StepContext) error {
			remotePerfDir := s10ResolvePerfDir(ctx)
			if remotePerfDir == "" {
				appendWarning(ctx, "S-10",
					"perf_remote_dir not found; S-04 may not have run or breadcrumb file missing")
				return nil
			}

			cmdTimeout := stressCmdTimeout(ctx)

			// ── 1. 终止所有后台采集进程 ────────────────────────────────────────
			stopScript := fmt.Sprintf(`
PIDS_FILE="%s/pids.txt"
if [ -f "$PIDS_FILE" ]; then
  PIDS=$(cat "$PIDS_FILE")
  for pid in $PIDS; do
    kill -TERM $pid 2>/dev/null || true
  done
  echo "sent SIGTERM to: $PIDS"
else
  echo "WARNING: pids file not found: $PIDS_FILE"
fi
`, remotePerfDir)

			out, err := stressRunShell(ctx, stopScript, false, 15*time.Second)
			ctx.Logger.Info("[S-10] stop result: %s err=%v", strings.TrimSpace(out), err)

			// 等待 3s 让进程完成最后一次写入并刷缓冲
			time.Sleep(3 * time.Second)

			// ── 2. 下载采集日志到本地归档 ──────────────────────────────────────
			hostDir := stressHostDir(ctx)
			bgDir := filepath.Join(hostDir, "runtime", "bg")

			// 待下载的日志文件（与 S-04 保持一致）
			logFiles := []string{
				"mpstat.log",
				"iostat.log",
				"vmstat.log",
				"sar_net.log",
				"sar_io.log",
			}

			downloaded := 0
			for _, name := range logFiles {
				remotePath := remotePerfDir + "/" + name
				localPath := filepath.Join(bgDir, name)

				r, execErr := stressExecute(ctx,
					fmt.Sprintf("test -s %s && cat %s || echo ''", remotePath, remotePath),
					false, cmdTimeout)
				content := ""
				if r != nil {
					content = r.GetStdout()
				}
				if execErr != nil {
					appendWarning(ctx, "S-10",
						fmt.Sprintf("read remote %s: %v", remotePath, execErr))
					continue
				}
				if strings.TrimSpace(content) == "" {
					ctx.Logger.Info("[S-10] %s is empty, skipping", name)
					continue
				}
				if err := writeTextFile(localPath, content); err != nil {
					appendWarning(ctx, "S-10",
						fmt.Sprintf("write %s: %v", localPath, err))
					continue
				}
				downloaded++
				ctx.Logger.Info("[S-10] downloaded: %s (%d bytes)", name, len(content))
			}

			// ── 3. 清理远端临时目录和 breadcrumb 文件 ──────────────────────────
			if _, cleanErr := stressExecute(ctx,
				fmt.Sprintf("rm -rf %s && rm -f %s", remotePerfDir, perfBreadcrumbFile),
				false, 15*time.Second); cleanErr != nil {
				appendWarning(ctx, "S-10",
					fmt.Sprintf("cleanup %s: %v", remotePerfDir, cleanErr))
			}

			ctx.Logger.Info("[S-10] perf collection stopped; downloaded=%d files to %s",
				downloaded, bgDir)
			return nil
		},
	}
}
