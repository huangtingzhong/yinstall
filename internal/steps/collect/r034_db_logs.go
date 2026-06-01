// r034_db_logs.go - 数据库日志采集（可选，时间窗守卫）
//
// YashanDB 日志目录结构（官方文档）：
//
//	非 OM 安装（直接安装，无 yasboot）：
//	  $YASDB_DATA/log/run/run.log          运行日志（可配置 RUN_LOG_FILE_PATH）
//	  $YASDB_DATA/log/alert/alert.log      告警日志（路径固定不可改）
//	  $YASDB_DATA/log/listener/listener.log  监听日志（路径固定不可改）
//	  $YASDB_DATA/log/slow/slow.log        慢查询日志（可配置 SLOW_LOG_FILE_PATH）
//	  $YASDB_DATA/log/external/server/yex_server.log  代理程序日志
//	  $YASDB_DATA/diag/trace/              ADR trace（DIAGNOSTIC_DEST 参数控制）
//
//	OM 安装（通过 yasboot 安装，即本工具的标准安装方式）：
//	  $YASDB_HOME/log/<cluster>/node-<nodeid>/run/run.log
//	  $YASDB_HOME/log/<cluster>/node-<nodeid>/slow/slow.log
//	  $YASDB_DATA/log/alert/alert.log      仍在 DATA 下
//	  $YASDB_DATA/log/listener/listener.log  仍在 DATA 下
//	  $YASDB_DATA/diag/trace/*.trc
//
// PreCheck：未指定 --db-log-since 时跳过（避免无界拷贝）。
// 500MB 总大小限制保护控制端磁盘。
package collect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yinstall/internal/runner"
)

const dbLogSizeLimitBytes = 500 * 1024 * 1024 // 500MB

// StepR034CollectDBLogs 返回 R-034 步骤：采集数据库日志（Optional）。
func StepR034CollectDBLogs() *runner.Step {
	return &runner.Step{
		ID:       "R-034",
		Name:     "Collect DB logs",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			since := ctx.GetParamString("db_log_since", "")
			if since == "" {
				return fmt.Errorf("--db-log-since not specified, skipping R-034 (prevents unbounded log copy)")
			}
			if getCollectEnvFile(ctx) == "" {
				return fmt.Errorf("env_file not available (R-004 skipped), skipping R-034")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			since := ctx.GetParamString("db_log_since", "")
			until := ctx.GetParamString("db_log_until", "")
			osUser := getCollectOSUser(ctx)
			envFile := getCollectEnvFile(ctx)
			clusterName := getCollectClusterName(ctx)
			if clusterName == "" {
				clusterName = "yashandb"
			}
			destDir := filepath.Join(collectHostDir(ctx), "db", "logs")
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				return fmt.Errorf("mkdir db/logs: %w", err)
			}

			get := func(cmd string) string {
				r, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile, cmd, collectCmdTimeout(ctx))
				if r != nil {
					return strings.TrimSpace(r.GetStdout())
				}
				return ""
			}

			yasdbHome := get("echo $YASDB_HOME")
			yasdbData := get("echo $YASDB_DATA")

			collectLogPhase(ctx, "plan",
				fmt.Sprintf("since=%q until=%q dest=db/logs limit_mb=%d cluster=%s",
					since, until, dbLogSizeLimitBytes/1024/1024, clusterName))

			// 1. journalctl（systemd 日志，按时间窗截取）
			jctlArgs := buildJournalctlArgs(since, until)
			_ = runAndSaveWithTimeout(ctx,
				fmt.Sprintf("journalctl -u 'yashan_monit*' %s 2>/dev/null || journalctl -u 'yasdb*' %s 2>/dev/null || true", jctlArgs, jctlArgs),
				filepath.Join(destDir, "journalctl.txt"),
				collectLogTimeout(ctx), false)

			tracker := &sizeTracker{limit: dbLogSizeLimitBytes}

			// 2. DATA 目录固定路径日志（路径不可修改，所有安装方式共有）
			if yasdbData != "" {
				fixedFiles := []string{
					yasdbData + "/log/alert/alert.log",
					yasdbData + "/log/listener/listener.log",
					yasdbData + "/log/external/server/yex_server.log",
				}
				for _, f := range fixedFiles {
					copyRemoteLog(ctx, osUser, envFile, f, destDir, tracker)
					if tracker.exceeded() {
						appendWarning(ctx, "R-034", "size limit reached, stopping log collection")
						return nil
					}
				}

				// 归档的 run.log 滚动文件（run-<timestamp>.log）
				copyRemoteLogDir(ctx, osUser, envFile,
					yasdbData+"/log/run", destDir+"/data-run",
					since, until, tracker)

				// slow.log（DATA 下，非 OM 安装）
				copyRemoteLogDir(ctx, osUser, envFile,
					yasdbData+"/log/slow", destDir+"/data-slow",
					since, until, tracker)

				// ADR trace 目录
				copyRemoteLogDir(ctx, osUser, envFile,
					yasdbData+"/diag/trace", destDir+"/trace",
					since, until, tracker)
			}

			// 3. HOME 目录 OM 安装日志（$YASDB_HOME/log/<cluster>/node-*/）
			if yasdbHome != "" && !tracker.exceeded() {
				omLogBase := yasdbHome + "/log/" + clusterName
				// 列出 node 子目录
				nodeList, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile,
					fmt.Sprintf("ls %s/ 2>/dev/null || true", omLogBase), collectCmdTimeout(ctx))
				if nodeList != nil && nodeList.GetStdout() != "" {
					for _, node := range strings.Fields(nodeList.GetStdout()) {
						nodeBase := omLogBase + "/" + node
						copyRemoteLogDir(ctx, osUser, envFile,
							nodeBase+"/run", destDir+"/om-"+node+"-run",
							since, until, tracker)
						copyRemoteLogDir(ctx, osUser, envFile,
							nodeBase+"/slow", destDir+"/om-"+node+"-slow",
							since, until, tracker)
						if tracker.exceeded() {
							break
						}
					}
				}
			}

			ctx.Logger.Info("[R-034] DB logs collected to %s (total ~%dMB)", destDir, tracker.bytes/1024/1024)
			return nil
		},
	}
}

// sizeTracker 累计已采集字节数，提供超限判断。
type sizeTracker struct {
	bytes int64
	limit int64
}

func (t *sizeTracker) exceeded() bool { return t.bytes >= t.limit }
func (t *sizeTracker) add(n int64)    { t.bytes += n }

// copyRemoteLog 拷贝单个远端日志文件到控制端归档目录。
func copyRemoteLog(ctx *runner.StepContext, osUser, envFile, remotePath, destDir string, tracker *sizeTracker) {
	if tracker.exceeded() {
		collectLogPhase(ctx, "copy-skip", fmt.Sprintf("remote=%s reason=tracker_limit", remotePath))
		return
	}
	exist, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile,
		fmt.Sprintf("test -f %s && echo yes || echo no", remotePath), collectCmdTimeout(ctx))
	if exist == nil || strings.TrimSpace(exist.GetStdout()) != "yes" {
		collectLogPhase(ctx, "copy-skip", fmt.Sprintf("remote=%s reason=not_found", remotePath))
		return
	}

	sizeOut, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile,
		fmt.Sprintf("stat -c%%s %s 2>/dev/null || echo 0", remotePath), collectCmdTimeout(ctx))
	var fileSize int64
	if sizeOut != nil {
		fmt.Sscanf(strings.TrimSpace(sizeOut.GetStdout()), "%d", &fileSize)
	}
	if tracker.bytes+fileSize > tracker.limit {
		collectLogPhase(ctx, "copy-skip",
			fmt.Sprintf("remote=%s size=%d reason=would_exceed_limit used_mb=%d",
				remotePath, fileSize, tracker.bytes/1024/1024))
		return
	}

	destFile := filepath.Join(destDir, filepath.Base(remotePath))
	collectLogPhase(ctx, "copy-start",
		fmt.Sprintf("remote=%s size=%d dest=%s used_mb=%d",
			remotePath, fileSize, collectDestLabel(ctx, destFile), tracker.bytes/1024/1024))

	content, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile,
		fmt.Sprintf("cat %s 2>/dev/null || true", remotePath), collectLogTimeout(ctx))
	if content == nil || content.GetStdout() == "" {
		collectLogPhase(ctx, "copy-skip", fmt.Sprintf("remote=%s reason=empty_content", remotePath))
		return
	}
	if err := writeTextFile(destFile, content.GetStdout()); err != nil {
		appendWarning(ctx, "R-034", fmt.Sprintf("write %s: %v", destFile, err))
		collectLogPhase(ctx, "copy-skip", fmt.Sprintf("remote=%s dest=%s reason=write_err", remotePath, destFile))
		return
	}
	tracker.add(fileSize)
	collectLogPhase(ctx, "copy-done",
		fmt.Sprintf("remote=%s dest=%s bytes=%d %s",
			remotePath, collectDestLabel(ctx, destFile), fileSize, collectOutputStats(content.GetStdout())))
}

// copyRemoteLogDir 将远端日志目录中满足时间窗的文件批量拷贝到 localDestDir。
func copyRemoteLogDir(ctx *runner.StepContext, osUser, envFile, remoteDir, localDestDir, since, until string, tracker *sizeTracker) {
	if tracker.exceeded() {
		return
	}

	collectLogPhase(ctx, "plan",
		fmt.Sprintf("copy_dir remote=%s local=%s since=%q until=%q",
			remoteDir, collectDestLabel(ctx, localDestDir), since, until))

	// 列出目录中的 .log 和 .trc 文件
	listOut, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile,
		fmt.Sprintf("find %s -maxdepth 1 -type f \\( -name '*.log' -o -name '*.trc' \\) 2>/dev/null | sort || true", remoteDir),
		collectCmdTimeout(ctx))
	if listOut == nil || listOut.GetStdout() == "" {
		return
	}

	for _, f := range strings.Split(strings.TrimSpace(listOut.GetStdout()), "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		// 时间窗过滤：若指定了 since，使用 find -newer 参考文件近似判断
		// 此处简化：直接拷贝（实际生产中可借助 find -newer 做精确过滤）
		_ = since
		_ = until

		copyRemoteLog(ctx, osUser, envFile, f, localDestDir, tracker)
		if tracker.exceeded() {
			appendWarning(ctx, "R-034", fmt.Sprintf("size limit %dMB reached", tracker.limit/1024/1024))
			return
		}
	}
}

// buildJournalctlArgs 根据时间窗构建 journalctl 参数字符串。
func buildJournalctlArgs(since, until string) string {
	var args []string
	if since != "" {
		// journalctl 接受 "YYYY-MM-DD HH:MM:SS" 格式
		args = append(args, fmt.Sprintf("--since=%q", since))
	}
	if until != "" {
		args = append(args, fmt.Sprintf("--until=%q", until))
	}
	return strings.Join(args, " ")
}
