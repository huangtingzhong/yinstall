// s005_cpu_bench.go - CPU 压测（sysbench cpu）
// 全机：单线程、threads=nproc、threads=2*nproc；
// 单 NUMA：numactl --cpunodebind=N --membind=N，threads=该节点 CPU 数及 2 倍。
package stressos

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

// stepCpuBench 返回 S-05 步骤：CPU 压测。
func stepCpuBench() *runner.Step {
	return &runner.Step{
		Name:     "CPU benchmark (sysbench cpu)",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !getBool(ctx, "stress_cpu", true) {
				return fmt.Errorf("CPU benchmark disabled (--cpu=false)")
			}
			if !s03ToolAvailable(ctx, "sysbench") {
				return fmt.Errorf("sysbench not available; install with --install-deps or place sysbench-1.0.20.tar.gz in --local-software-dirs")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			cpuDir := filepath.Join(stressHostDir(ctx), "cpu")

			durationSec := getInt(ctx, "cpu_time", 60)
			maxPrime := getInt(ctx, "cpu_max_prime", 200000)
			numaNode := getInt(ctx, "cpu_numa_node", 0)
			numaBind := getBool(ctx, "cpu_numa_bind", true)

			nproc := s05CPUCount(ctx)
			nproc2x := nproc * 2

			ctx.Logger.Info("[S-05] CPU benchmark: max_prime=%d duration=%ds threads=1,%d,%d (2*nproc)",
				maxPrime, durationSec, nproc, nproc2x)
			stressLogPhase(ctx, "plan",
				fmt.Sprintf("5 runs planned: single=1 nproc=%d 2*nproc=%d numa_bind=%v numa_node=%d (~%ds wall)",
					nproc, nproc2x, numaBind, numaNode, s05CPUPlannedWallSec(durationSec, numaBind)))

			benchTimeout := stressBenchTimeout(time.Duration(durationSec) * time.Second)

			s05RunSysbenchCPU(ctx, cpuDir, benchTimeout, maxPrime, durationSec, 1, -1,
				"sysbench_single.txt", "sysbench cpu single-thread (threads=1)")
			s05RunSysbenchCPU(ctx, cpuDir, benchTimeout, maxPrime, durationSec, nproc, -1,
				"sysbench_multi.txt", fmt.Sprintf("sysbench cpu multi-thread (threads=nproc=%d)", nproc))
			s05RunSysbenchCPU(ctx, cpuDir, benchTimeout, maxPrime, durationSec, nproc2x, -1,
				"sysbench_multi_2x.txt", fmt.Sprintf("sysbench cpu multi-thread (threads=2*nproc=%d)", nproc2x))

			if numaBind {
				s05RunNUMACPUBench(ctx, cpuDir, benchTimeout, maxPrime, durationSec, numaNode)
			} else {
				ctx.Logger.Info("[S-05] NUMA CPU benchmark skipped (--cpu-numa-bind=false)")
			}

			ctx.Logger.Info("[S-05] CPU benchmark completed, results in %s", cpuDir)
			return nil
		},
	}
}

// s05RunNUMACPUBench 在单个 NUMA 节点上执行 sysbench cpu（threads=节点 CPU 数及 2 倍）。
func s05RunNUMACPUBench(ctx *runner.StepContext, cpuDir string, timeout time.Duration,
	maxPrime, durationSec, numaNode int) {
	if !s03ToolAvailable(ctx, "numactl") {
		appendWarning(ctx, "numactl not available; skip NUMA node CPU benchmark")
		return
	}

	hw, ok := s05FetchNUMATopology(ctx)
	if !ok || strings.TrimSpace(hw) == "" {
		appendWarning(ctx, "cannot read numactl --hardware; skip NUMA node CPU benchmark")
		return
	}
	if err := writeTextFile(filepath.Join(cpuDir, "numa_hardware.txt"),
		"=== numactl --hardware ===\n"+hw+"\n"); err != nil {
		appendWarning(ctx, "write numa_hardware.txt: "+err.Error())
	}

	nodeCPUs := s05ParseNUMANodeCPUCount(hw, numaNode)
	if nodeCPUs <= 0 {
		appendWarning(ctx, fmt.Sprintf("NUMA node %d has no CPUs in topology; skip NUMA CPU benchmark", numaNode))
		return
	}
	node2x := nodeCPUs * 2
	nodes := s05ParseNUMANodeCount(hw)
	ctx.Logger.Info("[S-05] NUMA CPU benchmark: node=%d node_cpus=%d nodes_total=%d bind=cpunodebind+membind",
		numaNode, nodeCPUs, nodes)
	stressLogPhase(ctx, "numa-start",
		fmt.Sprintf("node=%d node_cpus=%d 2x=%d runs=2", numaNode, nodeCPUs, node2x))

	prefix := fmt.Sprintf("numactl --cpunodebind=%d --membind=%d", numaNode, numaNode)
	s05RunSysbenchCPU(ctx, cpuDir, timeout, maxPrime, durationSec, nodeCPUs, numaNode,
		fmt.Sprintf("sysbench_numa_node%d.txt", numaNode),
		fmt.Sprintf("%s sysbench cpu (threads=node%d_cpus=%d)", prefix, numaNode, nodeCPUs))
	s05RunSysbenchCPU(ctx, cpuDir, timeout, maxPrime, durationSec, node2x, numaNode,
		fmt.Sprintf("sysbench_numa_node%d_2x.txt", numaNode),
		fmt.Sprintf("%s sysbench cpu (threads=2*node%d_cpus=%d)", prefix, numaNode, node2x))
}

// s05RunSysbenchCPU 执行一次 sysbench cpu 并写入 cpuDir/outFile。
// numaNode < 0 表示不绑 NUMA；否则使用 numactl --cpunodebind --membind。
func s05RunSysbenchCPU(ctx *runner.StepContext, cpuDir string, timeout time.Duration,
	maxPrime, durationSec, threads, numaNode int, outFile, header string) {
	inner := fmt.Sprintf(
		"sysbench cpu --cpu-max-prime=%d --time=%d --threads=%d run",
		maxPrime, durationSec, threads)
	cmd := inner
	if numaNode >= 0 {
		cmd = fmt.Sprintf("numactl --cpunodebind=%d --membind=%d %s", numaNode, numaNode, inner)
	}

	stressLogPhase(ctx, "bench-start",
		fmt.Sprintf("out=%s threads=%d time=%ds max_prime=%d timeout_cap=%ds numa_node=%d",
			outFile, threads, durationSec, maxPrime, int(timeout.Seconds()), numaNode))

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
		appendWarning(ctx, fmt.Sprintf("%s failed: %v", header, err))
		body += "\nERROR: " + err.Error()
		stressLogPhase(ctx, "bench-fail",
			fmt.Sprintf("out=%s wall=%s exit=%d err=%v", outFile, wallDur.Round(time.Millisecond), exitCode, err))
	} else {
		stressLogPhase(ctx, "bench-done",
			fmt.Sprintf("out=%s wall=%s exit=%d %s", outFile, wallDur.Round(time.Millisecond), exitCode, summary))
	}

	dest := filepath.Join(cpuDir, outFile)
	if err2 := writeTextFile(dest, "=== "+header+" ===\n"+body+"\n"); err2 != nil {
		appendWarning(ctx, "write "+outFile+": "+err2.Error())
	}
}

// s05CPUPlannedWallSec 估算 S-05 总墙钟时间（秒）：全机 3 轮 + 可选 NUMA 2 轮。
func s05CPUPlannedWallSec(durationSec int, numaBind bool) int {
	n := 3
	if numaBind {
		n += 2
	}
	return n * durationSec
}

// s05FetchNUMATopology 在目标机执行 numactl --hardware 并返回输出。
func s05FetchNUMATopology(ctx *runner.StepContext) (string, bool) {
	if ctx == nil || ctx.Executor == nil {
		return "", false
	}
	r, err := stressExecute(ctx, "numactl --hardware 2>/dev/null", false, 15*time.Second)
	if err != nil || r == nil || r.GetExitCode() != 0 {
		return "", false
	}
	return r.GetStdout(), true
}

// s05ParseNUMANodeCount 从 numactl --hardware 解析 NUMA 节点个数。
func s05ParseNUMANodeCount(hardware string) int {
	for _, line := range strings.Split(hardware, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "available:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if n, err := strconv.Atoi(fields[1]); err == nil && n > 0 {
				return n
			}
		}
	}
	return 1
}

// s05ParseNUMANodeCPUCount 返回指定 NUMA 节点上的 CPU 个数（解析 node N cpus: 行）。
func s05ParseNUMANodeCPUCount(hardware string, node int) int {
	prefix := fmt.Sprintf("node %d cpus:", node)
	for _, line := range strings.Split(hardware, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		fields := strings.Fields(line)
		// node 0 cpus: 0 1 2 3
		if len(fields) <= 3 {
			return 0
		}
		return len(fields) - 3
	}
	return 0
}

// s05CPUCount 返回目标机 nproc（逻辑 CPU 数）；失败时回落 4。
func s05CPUCount(ctx *runner.StepContext) int {
	if ctx == nil || ctx.Executor == nil {
		return 4
	}
	r, _ := stressExecute(ctx, "nproc", false, 5*time.Second)
	if r != nil && r.GetExitCode() == 0 {
		n, err := strconv.Atoi(strings.TrimSpace(r.GetStdout()))
		if err == nil && n > 0 {
			return n
		}
	}
	return 4
}
