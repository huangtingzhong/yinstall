// r024_db_processes.go - 数据库进程与端口采集
// 采集 yasdb/yasagent 进程、监听端口、CPU 绑核（taskset/cgroup）等运行时信息，
// 写入 db/processes.json 和 db/cpu-affinity.json。
package collect

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// stepDbProcesses 返回 R-024 步骤：采集数据库进程与端口信息。
func stepDbProcesses() *runner.Step {
	return &runner.Step{
		Name: "Collect DB processes and ports",
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "db")
			clusterName := getCollectClusterName(ctx)

			cmds := []struct {
				cmd  string
				dest string
			}{
				{"ps aux | grep -E 'yasdb|yasagent|yasboot' | grep -v grep || true", filepath.Join(dir, "ps-yasdb.txt")},
				{"pgrep -a yasdb 2>/dev/null || true", filepath.Join(dir, "pgrep-yasdb.txt")},
				{"pgrep -a yasagent 2>/dev/null || true", filepath.Join(dir, "pgrep-yasagent.txt")},
				{"ss -tlnp 2>/dev/null | grep -E 'yasdb|1688|1689' || netstat -tlnp 2>/dev/null | grep -E 'yasdb|1688|1689' || true", filepath.Join(dir, "ports.txt")},
			}

			if clusterName != "" {
				cmds = append(cmds, struct {
					cmd  string
					dest string
				}{
					cmd:  "pgrep -a -f " + clusterName + " 2>/dev/null | grep -E 'yasdb|yasagent' || true",
					dest: filepath.Join(dir, "pgrep-cluster.txt"),
				})
			}

			collectLogPhase(ctx, "plan",
				fmt.Sprintf("cluster=%q runAndSave_cmds=%d +affinity+cgroup dir=db", clusterName, len(cmds)))

			for _, c := range cmds {
				if err := runAndSave(ctx, c.cmd, c.dest); err != nil {
					appendWarning(ctx, err.Error())
				}
			}

			// CPU 绑核（taskset）
			_ = runAndSave(ctx,
				"for pid in $(pgrep yasdb 2>/dev/null); do echo \"pid=$pid taskset=$(taskset -cp $pid 2>/dev/null | awk '{print $NF}')\"; done || true",
				filepath.Join(dir, "cpu-affinity.txt"))

			// cgroup 信息
			_ = runAndSave(ctx,
				"cat /sys/fs/cgroup/cpu/yasdb*/cpu.shares 2>/dev/null || true",
				filepath.Join(dir, "cgroup-cpu.txt"))

			ctx.Logger.Info("[R-024] DB processes and ports collected to %s", dir)
			return nil
		},
	}
}
