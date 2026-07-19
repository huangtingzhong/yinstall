// s004_pre_snapshot.go - 启动后台 OS 性能数据并行采集
// 在压测开始前，以并行模式在目标机后台启动多类 OS 实时性能采集命令：
//
//	iostat（磁盘 IO 扩展统计）、mpstat（逐核 CPU）、vmstat（内存/交换/IO/系统）、
//	sar（网络设备）、dstat（可选，综合仪表盘）。
//
// 设计要点：
//   - 全部采集进程通过 nohup + & 并行启动，SSH session 结束后继续运行。
//   - 每条采集命令加 timeout <MAX_SECS> 安全上限，防止 S-10 未执行时孤立进程无限占用。
//   - 将所有子进程 PID 与远端数据目录路径写入 ctx.Results，供 S-10 读取。
//   - 启动脚本本身通过 stressRunShell 施加短暂 SSH session 超时（30s）。
package stressos

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

// perfRemoteDirKey 用于在 ctx.Results 中传递远端采集目录路径（S-04 → S-10）。
const perfRemoteDirKey = "perf_remote_dir"

// perfBreadcrumbFile 在远端写入的固定路径，用于跨调用（-s S-10）的兜底读取。
const perfBreadcrumbFile = "/tmp/.yinstall_stress_perf_dir"

// stepPreSnapshot 返回 S-04 步骤：后台启动 OS 性能并行采集。
func stepPreSnapshot() *runner.Step {
	return &runner.Step{
		Name: "Start background OS performance collection",
		Action: func(ctx *runner.StepContext) error {
			ts := fmt.Sprintf("%d", time.Now().UnixNano())
			remotePerfDir := "/tmp/.stress_perf_" + ts

			// 采集持续时间上限（秒）= stress-cmd-timeout * 10，最小 3600s
			// 用于 timeout 命令包裹每条采集进程，防止孤立进程长期占用。
			maxSecs := getInt(ctx, "stress_cmd_timeout", 600) * 10
			if maxSecs < 3600 {
				maxSecs = 3600
			}

			// 启动脚本：并行启动所有采集进程，以 & 并发，nohup 防 SIGHUP，timeout 加安全上限。
			script := s04BuildStartScript(remotePerfDir, maxSecs)

			// 启动超时 30s（只是创建进程的脚本，快速返回）
			out, err := stressRunShell(ctx, script, false, 30*time.Second)
			if err != nil {
				appendWarning(ctx, fmt.Sprintf("start perf collection may have partial failures: %v", err))
			}
			ctx.Logger.Info("[S-04] perf collection started: dir=%s output=%s",
				remotePerfDir, strings.TrimSpace(out))

			// 写入 Results 供 S-10 读取
			ctx.Results[perfRemoteDirKey] = remotePerfDir

			return nil
		},
	}
}

// s04BuildStartScript 构造并行启动采集进程的 shell 脚本。
// 每条命令均以 nohup timeout N cmd >> file 2>&1 & 方式启动，脚本立即返回。
func s04BuildStartScript(remotePerfDir string, maxSecs int) string {
	d := remotePerfDir
	m := maxSecs

	// 各采集命令（每条独立进程，并行运行）
	collectors := []struct {
		name string
		cmd  string
	}{
		// CPU 占用（每 2s 一次，所有核心）
		{"mpstat.log", fmt.Sprintf("mpstat -P ALL 2 2>/dev/null || echo 'mpstat not available'")},
		// 磁盘 IO 扩展统计（每 2s 一次）
		{"iostat.log", fmt.Sprintf("iostat -xk 2 2>/dev/null || echo 'iostat not available'")},
		// 内存/交换/IO/系统/CPU 综合（每 2s 含时间戳）
		{"vmstat.log", fmt.Sprintf("vmstat -tw 2 2>/dev/null || vmstat 2")},
		// 网络设备流量统计（每 2s 一次，需 sysstat）
		{"sar_net.log", fmt.Sprintf("sar -n DEV 2 2>/dev/null || echo 'sar not available'")},
		// 块设备 IO 等待（每 2s 一次，需 sysstat）
		{"sar_io.log", fmt.Sprintf("sar -d 2 2>/dev/null || echo 'sar -d not available'")},
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("mkdir -p %s\nPIDS=''\n", d))

	for _, c := range collectors {
		logFile := filepath.ToSlash(filepath.Join(d, c.name))
		// nohup timeout N cmd 方式：既防 SIGHUP，又有安全上限
		sb.WriteString(fmt.Sprintf(
			"nohup timeout %d bash -c %s >> %s 2>&1 &\nPIDS=\"$PIDS $!\"\n",
			m, shellQuoteArg(c.cmd), logFile))
	}

	// 写 PID 文件，供 S-10 杀进程
	sb.WriteString(fmt.Sprintf("echo \"$PIDS\" > %s/pids.txt\n", d))
	// 标记采集已启动（S-10 检查此文件确认采集正常启动）
	sb.WriteString(fmt.Sprintf("echo started > %s/status.txt\n", d))
	// breadcrumb：写到固定文件，供跨独立调用（-s S-10）的 S-10 兜底读取
	sb.WriteString(fmt.Sprintf("echo %s > %s\n", d, perfBreadcrumbFile))
	sb.WriteString(fmt.Sprintf("echo 'perf_dir=%s pids='$PIDS\n", d))

	return sb.String()
}

// shellQuoteArg 对 bash -c 的参数加单引号（内部单引号转义）。
func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
