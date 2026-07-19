// s009_runtime_metrics.go - 运行时指标采集
// 在压测执行后采集系统运行时指标（iostat、mpstat、top、netstat、softirq、sys_params）。
// 每类指标通过内嵌 shell 脚本执行（文件上传方式），结果写入 <host>/runtime/。
package stressos

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// stepRuntimeMetrics 返回 S-09 步骤：运行时指标采集。
func stepRuntimeMetrics() *runner.Step {
	return &runner.Step{
		Name: "Collect runtime metrics",
		Action: func(ctx *runner.StepContext) error {
			hostDir := stressHostDir(ctx)
			runtimeDir := filepath.Join(hostDir, "runtime")

			timeout := stressCmdTimeout(ctx)

			// 各类采集脚本（embed/scripts/shell/runtime/）
			scripts := []struct {
				scriptFile string
				destFile   string
			}{
				{"iostat.sh", "iostat.txt"},
				{"mpstat.sh", "mpstat.txt"},
				{"top.sh", "top.txt"},
				{"netstat.sh", "netstat.txt"},
				{"softirq.sh", "softirq.txt"},
				{"sys_params.sh", "sys_params.txt"},
			}

			for _, item := range scripts {
				content, err := readEmbedShell(item.scriptFile)
				if err != nil {
					appendWarning(ctx, fmt.Sprintf("read %s: %v", item.scriptFile, err))
					continue
				}

				out, runErr := stressRunShell(ctx, content, false, timeout)
				if runErr != nil {
					appendWarning(ctx, fmt.Sprintf("%s failed: %v", item.scriptFile, runErr))
					out += "\nERROR: " + runErr.Error()
				}

				destPath := filepath.Join(runtimeDir, item.destFile)
				if err2 := writeTextFile(destPath, out+"\n"); err2 != nil {
					appendWarning(ctx, "write "+destPath+": "+err2.Error())
				}
				ctx.Logger.Info("[S-09] collected %s -> %s", item.scriptFile, item.destFile)
			}

			ctx.Logger.Info("[S-09] runtime metrics collected in %s (timeout per script: %v)",
				runtimeDir, timeout)
			return nil
		},
	}
}
