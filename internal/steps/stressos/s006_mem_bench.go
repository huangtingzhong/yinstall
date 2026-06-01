// s006_mem_bench.go - 内存压测（sysbench memory）
// 运行标准内存带宽和可选的 numactl 绑定内存压测，结果写入 <host>/mem/。
package stressos

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/yinstall/internal/runner"
)

// StepS06MEMBench 返回 S-06 步骤：内存压测。
func StepS06MEMBench() *runner.Step {
	return &runner.Step{
		ID:       "S-06",
		Name:     "Memory benchmark (sysbench memory)",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !getBool(ctx, "stress_mem", true) {
				return fmt.Errorf("memory benchmark disabled (--mem=false)")
			}
			if !s03ToolAvailable(ctx, "sysbench") {
				return fmt.Errorf("sysbench not available")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			hostDir := stressHostDir(ctx)
			memDir := filepath.Join(hostDir, "mem")

			durationSec := getInt(ctx, "mem_time", 60)
			blockSize := getStr(ctx, "mem_block_size", "8K")
			totalSize := getStr(ctx, "mem_total_size", "50G")
			numaBind := getBool(ctx, "mem_numa_bind", false)

			benchTimeout := stressBenchTimeout(time.Duration(durationSec) * time.Second)

			runs := 1
			if numaBind && s03ToolAvailable(ctx, "numactl") {
				runs = 2
			}
			plannedWall := runs * durationSec

			ctx.Logger.Info("[S-06] Memory benchmark: block=%s total=%s duration=%ds numa=%v",
				blockSize, totalSize, durationSec, numaBind)
			stressLogPhase(ctx, "plan",
				fmt.Sprintf("runs=%d block=%s total=%s duration=%ds numa_bind=%v (~%ds wall)",
					runs, blockSize, totalSize, durationSec, numaBind, plannedWall))

			memCmd := fmt.Sprintf(
				"sysbench memory --memory-block-size=%s --memory-total-size=%s --time=%d run",
				blockSize, totalSize, durationSec)
			s06RunMemRound(ctx, memDir, benchTimeout, memCmd,
				"sysbench_mem.txt", "sysbench memory", durationSec, blockSize, totalSize)

			if numaBind && s03ToolAvailable(ctx, "numactl") {
				numaCmd := fmt.Sprintf(
					"numactl --interleave=all sysbench memory --memory-block-size=%s --memory-total-size=%s --time=%d run",
					blockSize, totalSize, durationSec)
				s06RunMemRound(ctx, memDir, benchTimeout, numaCmd,
					"sysbench_mem_numa.txt", "sysbench memory (numactl --interleave=all)",
					durationSec, blockSize, totalSize)
			} else if numaBind {
				appendWarning(ctx, "S-06", "numactl not available; skip NUMA memory benchmark")
			}

			ctx.Logger.Info("[S-06] Memory benchmark completed, results in %s", memDir)
			return nil
		},
	}
}

// s06RunMemRound 执行一轮 sysbench memory 并写入 memDir/outFile。
func s06RunMemRound(ctx *runner.StepContext, memDir string, timeout time.Duration,
	cmd, outFile, header string, durationSec int, blockSize, totalSize string) {
	stressLogPhase(ctx, "bench-start",
		fmt.Sprintf("out=%s duration=%ds block=%s total=%s timeout_cap=%ds cmd=%s",
			outFile, durationSec, blockSize, totalSize, int(timeout.Seconds()), truncateCmdForLog(cmd)))

	wallStart := time.Now()
	out, err := stressExecute(ctx, cmd, false, timeout)
	wallDur := time.Since(wallStart)

	body := ""
	exitCode := -1
	if out != nil {
		body = out.GetStdout()
		exitCode = out.GetExitCode()
	}
	summary := stressSysbenchSummary(body)
	if err != nil {
		appendWarning(ctx, "S-06", fmt.Sprintf("%s failed: %v", header, err))
		body += "\nERROR: " + err.Error()
		stressLogPhase(ctx, "bench-fail",
			fmt.Sprintf("out=%s wall=%s exit=%d err=%v", outFile, wallDur.Round(time.Millisecond), exitCode, err))
	} else {
		stressLogPhase(ctx, "bench-done",
			fmt.Sprintf("out=%s wall=%s exit=%d %s", outFile, wallDur.Round(time.Millisecond), exitCode, summary))
	}

	dest := filepath.Join(memDir, outFile)
	if err2 := writeTextFile(dest, "=== "+header+" ===\n"+body+"\n"); err2 != nil {
		appendWarning(ctx, "S-06", "write "+outFile+": "+err2.Error())
	}
}
