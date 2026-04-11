package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC000HomeCheck 检查 YASDB_HOME 目录下是否有 yasagent 或 yasdb 进程在运行
func StepC002HomeCheck() *runner.Step {
	return &runner.Step{
		ID:          "C-002",
		Name:        "Check YASDB_HOME Processes",
		Description: "Verify no yasdb/yasagent processes are running under YASDB_HOME",
		Tags:        []string{"db", "home", "precheck"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)

				// 检查 YASDB_HOME 目录下是否有 yasagent 或 yasdb 进程在运行
				hctx.Logger.Info("Checking for yasagent or yasdb processes under %s on %s...", installPath, th.Host)
				// 在路径后添加 / 以避免误匹配（如 /data/1233 不会匹配到 /data/12334）
				installPathPattern := installPath
				if !strings.HasSuffix(installPathPattern, "/") {
					installPathPattern = installPathPattern + "/"
				}
				// 检查进程的启动路径是否在 YASDB_HOME 目录下
				// 使用 ps 命令查找进程，检查进程的命令行参数是否包含 YASDB_HOME 路径
				homeProcessCmd := fmt.Sprintf(
					"ps -ef | grep -E '(yasdb|yasagent)' | grep -v grep | grep -F %s",
					commonos.ShellSingleQuote(installPathPattern),
				)
				homeProcessResult, _ := hctx.Execute(homeProcessCmd, false)
				if homeProcessResult != nil && homeProcessResult.GetExitCode() == 0 && strings.TrimSpace(homeProcessResult.GetStdout()) != "" {
					// 提取进程 PID 和详细信息
					processLines := strings.Split(strings.TrimSpace(homeProcessResult.GetStdout()), "\n")
					var pids []string
					var processDetails []string
					for _, line := range processLines {
						line = strings.TrimSpace(line)
						if line != "" {
							// 提取 PID（通常是第二列）
							fields := strings.Fields(line)
							if len(fields) >= 2 {
								pids = append(pids, fields[1])
								processDetails = append(processDetails, line)
							}
						}
					}
					if len(pids) > 0 {
						pidList := strings.Join(pids, ", ")
						return fmt.Errorf("YashanDB processes (yasdb/yasagent) are already running under %s on %s (cluster: %s, port: %d); PIDs: %s; process details: %s; please stop the database first or use clean command to remove it", installPath, th.Host, clusterName, beginPort, pidList, strings.Join(processDetails, "; "))
					}
				}
				hctx.Logger.Info("✓ No yasdb/yasagent processes found under %s on %s", installPath, th.Host)
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
