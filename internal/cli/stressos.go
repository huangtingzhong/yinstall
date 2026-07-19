// stressos.go - yinstall stressos 子命令
// 实现对目标主机通过 SSH 执行 OS 压测（CPU/MEM/IO/NET），将结果归档到本地目录。
// 命令结构与 collect.go 完全对齐；两阶段编排复用 runner_host.go。
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/common/archive"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
	stresssteps "github.com/yinstall/internal/steps/stressos"
)

// stressExecAdapter 实现 runner.Executor，额外暴露 ExecuteCtx 支持 SSH session 超时（方案D）。
// 仅供 stressos 子命令使用，与 collectExecAdapter 结构完全一致，独立定义避免跨包耦合。
type stressExecAdapter struct {
	e ssh.Executor
}

func (a *stressExecAdapter) Execute(cmd string, sudo bool) (runner.ExecResult, error) {
	return a.e.Execute(cmd, sudo)
}

// ExecuteCtx 通过类型断言调用底层 SSH 执行器的 ExecuteContext，施加 context 超时。
func (a *stressExecAdapter) ExecuteCtx(ctx context.Context, cmd string, sudo bool) (runner.ExecResult, error) {
	type ctxExec interface {
		ExecuteContext(context.Context, string, bool) (*ssh.ExecResult, error)
	}
	if ce, ok := a.e.(ctxExec); ok {
		return ce.ExecuteContext(ctx, cmd, sudo)
	}
	return a.e.Execute(cmd, sudo)
}

func (a *stressExecAdapter) Host() string { return a.e.Host() }
func (a *stressExecAdapter) Close() error { return a.e.Close() }
func (a *stressExecAdapter) Upload(localPath, remotePath string, uploadCtx *ssh.UploadContext) error {
	return a.e.Upload(localPath, remotePath, uploadCtx)
}

// SSHExecutor 暴露底层传输，供 runner 挂接实时 debug 输出回调。
func (a *stressExecAdapter) SSHExecutor() ssh.Executor {
	if a == nil {
		return nil
	}
	return a.e
}

// stressExecFactory 是 ExecutorAdapterFactory，供 RunPerHostStepsEx 使用。
func stressExecFactory(e ssh.Executor) runner.Executor {
	return &stressExecAdapter{e: e}
}

// ─── stressos 专属 flag 变量 ─────────────────────────────────────────────────

var (
	stressInstallDeps   bool   // --install-deps
	stressDepsOnly      bool   // --deps-only
	stressDoCPU         bool   // --cpu
	stressDoMEM         bool   // --mem
	stressDoIO          bool   // --io
	stressDoNET         bool   // --net
	stressCmdTimeoutSec int    // --stress-cmd-timeout
	stressCPUTimeSec    int    // --cpu-time
	stressCPUMaxPrime   int    // --cpu-max-prime
	stressCPUNumaBind   bool   // --cpu-numa-bind
	stressCPUNumaNode   int    // --cpu-numa-node
	stressMEMTimeSec    int    // --mem-time
	stressMEMBlockSize  string // --mem-block-size
	stressMEMTotalSize  string // --mem-total-size
	stressMEMNumaBind   bool   // --mem-numa-bind
	stressIODir         string // --io-dir
	stressIOSize        string // --io-size
	stressIODirect      int    // --io-direct (0=buffered, 1=O_DIRECT)
	stressIOTimeSec     int    // --io-time
	stressKeepIOFiles   bool   // --keep-io-files
	// fio 引擎与 IO 模式参数
	stressIOEngine         string // --io-engine 全局默认
	stressIOEngineData     string // --io-engine-data 数据文件场景（randrw/randread）
	stressIOEngineLogwrite string // --io-engine-logwrite redo/logwrite 场景
	stressIOEngineRandrw   string // --io-engine-randrw 覆盖数据文件默认
	stressIOEngineRandread string // --io-engine-randread 覆盖数据文件默认
	stressIOBSRandrw       string // --io-bs-randrw  block size for randrw scenario
	stressIOBSRandread     string // --io-bs-randread block size for randread scenario
	stressIOBSLogwrite     string // --io-bs-logwrite block size for logwrite scenario
	stressIOIodepth        int    // --io-iodepth  (0=auto: 2*nproc for randrw/randread, 1 for logwrite)
	stressIONumjobs        int    // --io-numjobs  (0=auto: 2*nproc for randrw/randread, 1 for logwrite)
	stressIORwmixRead      int    // --io-rwmix-read  read ratio % for randrw (default 70)
	stressIOFsync          int    // --io-fsync  fsync interval for logwrite (default 1)
	// fio 块设备模式（危险）：直接对 /dev/* 做 IO 压测
	stressIODevice       string // --io-device (e.g. /dev/nvme0n1)
	stressIODeviceMode   string // --io-device-mode (readonly|readwrite)
	stressDangerIODevice bool   // --danger-io-device (required to enable io-device)
	stressPingTarget     string // --ping-target
	stressRulesFile      string // --rules-file（预留，供后续扩展自定义压测规则）
	stressNoPack         bool   // --no-pack
)

// stressosCmd 是 stressos 子命令的 cobra.Command 实例。
var stressosCmd = newStressOSCommand()

// newStressOSCommand 构造并返回 stressos 子命令。
func newStressOSCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stressos",
		Short: "Run OS stress tests (CPU/MEM/IO/NET) on target hosts",
		Long: `Run OS stress tests on target hosts via SSH and archive results locally.

Tests include CPU (sysbench), Memory (sysbench memory), IO (fio 3 scenarios),
and Network (ping latency, optional). Results are saved under --output
(default: ./output/stress/<timestamp>/).

Use --install-deps to automatically install required tools.
Use --deps-only to only install dependencies without running tests.`,
		RunE:         runStressOS,
		SilenceUsage: true,
	}

	// 基础 flag（--output/-o 为全局参数，见 root.go）
	cmd.Flags().BoolVar(&stressNoPack, "no-pack", false,
		"Do not create an archive after stress tests (default: try tar.gz, then zip)")
	cmd.Flags().BoolVar(&stressInstallDeps, "install-deps", true,
		"Automatically install required tools (sysbench, fio, sysstat, numactl, iperf3)")
	cmd.Flags().BoolVar(&stressDepsOnly, "deps-only", false,
		"Only install dependencies, skip all stress tests")

	// 压测开关
	cmd.Flags().BoolVar(&stressDoCPU, "cpu", true, "Run CPU benchmark (sysbench cpu)")
	cmd.Flags().BoolVar(&stressDoMEM, "mem", true, "Run memory benchmark (sysbench memory)")
	cmd.Flags().BoolVar(&stressDoIO, "io", true, "Run IO benchmark (fio)")
	cmd.Flags().BoolVar(&stressDoNET, "net", false,
		"Run network benchmark (single-host ping; >=2 targets: YAC mesh ping + iperf3 server=first -t, clients=rest)")

	// 超时
	cmd.Flags().IntVar(&stressCmdTimeoutSec, "stress-cmd-timeout", 600,
		"Default max seconds per stress command (0 = no limit)")

	// CPU 参数
	cmd.Flags().IntVar(&stressCPUTimeSec, "cpu-time", 60, "CPU benchmark duration in seconds")
	cmd.Flags().IntVar(&stressCPUMaxPrime, "cpu-max-prime", 200000,
		"sysbench cpu --cpu-max-prime value")
	cmd.Flags().BoolVar(&stressCPUNumaBind, "cpu-numa-bind", true,
		"Run sysbench cpu bound to a single NUMA node (numactl --cpunodebind/--membind)")
	cmd.Flags().IntVar(&stressCPUNumaNode, "cpu-numa-node", 0,
		"NUMA node index for --cpu-numa-bind (default 0)")

	// MEM 参数
	cmd.Flags().IntVar(&stressMEMTimeSec, "mem-time", 60, "Memory benchmark duration in seconds")
	cmd.Flags().StringVar(&stressMEMBlockSize, "mem-block-size", "8K",
		"Memory block size for sysbench memory")
	cmd.Flags().StringVar(&stressMEMTotalSize, "mem-total-size", "50G",
		"Total transfer size for sysbench memory (used when --mem-time=0)")
	cmd.Flags().BoolVar(&stressMEMNumaBind, "mem-numa-bind", false,
		"Run extra numactl --interleave=all memory test")

	// IO 基础参数
	cmd.Flags().StringVar(&stressIODir, "io-dir", "/data/yashan",
		"Remote directory for fio test files")
	cmd.Flags().StringVar(&stressIOSize, "io-size", "10G",
		"Test file size per fio job")
	cmd.Flags().IntVar(&stressIODirect, "io-direct", 1,
		"fio direct I/O: 1=O_DIRECT (bypass page cache, recommended for DB), 0=buffered")
	cmd.Flags().IntVar(&stressIOTimeSec, "io-time", 120,
		"IO benchmark duration in seconds per fio job (0 = size-limited)")
	cmd.Flags().BoolVar(&stressKeepIOFiles, "keep-io-files", false,
		"Keep fio test files on remote after IO benchmark (default: clean up)")
	// IO 引擎与模式参数（数据库场景调优）
	cmd.Flags().StringVar(&stressIOEngine, "io-engine", "libaio",
		"fio ioengine default for all scenarios when per-scenario engine is not set")
	cmd.Flags().StringVar(&stressIOEngineData, "io-engine-data", "",
		"fio ioengine for DB data file scenarios (randrw/randread); empty uses --io-engine")
	cmd.Flags().StringVar(&stressIOEngineLogwrite, "io-engine-logwrite", "",
		"fio ioengine for redo/logwrite scenario; empty uses --io-engine")
	cmd.Flags().StringVar(&stressIOEngineRandrw, "io-engine-randrw", "",
		"fio ioengine for randrw only; empty uses --io-engine-data or --io-engine")
	cmd.Flags().StringVar(&stressIOEngineRandread, "io-engine-randread", "",
		"fio ioengine for randread only; empty uses --io-engine-data or --io-engine")
	cmd.Flags().StringVar(&stressIOBSRandrw, "io-bs-randrw", "8k",
		"Block size for random read/write scenario (DB data file typical: 8k)")
	cmd.Flags().StringVar(&stressIOBSRandread, "io-bs-randread", "8k",
		"Block size for random read scenario")
	cmd.Flags().StringVar(&stressIOBSLogwrite, "io-bs-logwrite", "4k",
		"Block size for log write scenario (DB redo/WAL log typical: 4k)")
	cmd.Flags().IntVar(&stressIOIodepth, "io-iodepth", 0,
		"fio iodepth for randrw/randread (0=auto: 2*nproc; logwrite always uses 1)")
	cmd.Flags().IntVar(&stressIONumjobs, "io-numjobs", 0,
		"fio numjobs for randrw/randread (0=auto: 2*nproc; logwrite always uses 1)")
	cmd.Flags().IntVar(&stressIORwmixRead, "io-rwmix-read", 70,
		"Read ratio %% for randrw scenario (default 70, meaning 70%% read / 30%% write)")
	cmd.Flags().IntVar(&stressIOFsync, "io-fsync", 1,
		"fio fsync interval for logwrite scenario (1=fsync after every write, simulates DB redo log)")
	// 块设备模式（默认关闭）：注意 readwrite 会破坏设备数据
	cmd.Flags().StringVar(&stressIODevice, "io-device", "",
		"Run fio directly on a block device (DANGEROUS, e.g. /dev/nvme0n1). Mutually exclusive with --io-dir")
	cmd.Flags().StringVar(&stressIODeviceMode, "io-device-mode", "readwrite",
		"Block device mode: readwrite (default, runs randrw+randread+logwrite, DESTROYS DATA) or readonly (randread only, safe)")
	cmd.Flags().BoolVar(&stressDangerIODevice, "danger-io-device", false,
		"Enable --io-device mode (required). readwrite destroys all data on the device")

	// NET 参数
	cmd.Flags().StringVar(&stressPingTarget, "ping-target", "",
		"Ping target for --net (default: first -t; local mode uses default gateway; empty gateway skips net)")

	// 扩展
	cmd.Flags().StringVar(&stressRulesFile, "rules-file", "",
		"Extra stress rules YAML file (reserved for future custom stress rules)")

	// YUM/ISO 与 yinstall os 共用 --os-yum-mode、--os-iso-device 等
	registerOSYumISOFlags(cmd, registerOSFlagsConfig{forDB: false})

	return cmd
}

// runStressOS 是 stressos 子命令的主处理函数（3-phase 编排）。
func runStressOS(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()

	// --list-steps / -l
	if flags.ListSteps {
		printStressStepCatalog()
		return nil
	}

	// 未指定 --targets 时，默认本地执行（与 os/db 一致）。
	if len(flags.Targets) == 0 {
		flags.Local = true
		flags.Targets = []string{"localhost"}
	} else {
		flags.Local = false
	}

	// 确定输出目录（默认 ./output/stress/<timestamp>，无写权限时回退到系统临时目录）
	outDir, outFallback, err := archive.ResolveOutputDir(flags.Output, "stress")
	if err != nil {
		return err
	}
	if err := archive.EnsureOutputDir(outDir); err != nil {
		return err
	}
	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("stress-%s", time.Now().Format(archive.TimestampFormat))
	}

	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	logger.Info("Starting yinstall stressos (RunID: %s)", rid)
	logger.Info("Targets: %v", flags.Targets)
	logger.Info("Output directory: %s", outDir)
	if outFallback {
		logger.Warn("Could not create ./output under current directory (permission denied); using temp dir: %s", outDir)
	}
	logger.Info("Tests enabled: cpu=%v mem=%v io=%v net=%v",
		stressDoCPU, stressDoMEM, stressDoIO, stressDoNET)
	if stressCmdTimeoutSec > 0 {
		logger.Info("Stress cmd timeout: %ds", stressCmdTimeoutSec)
	}

	netPlan := resolveStressNetPlan(cmd, flags, stressDoNET, stressPingTarget)

	// 构造步骤参数
	params := buildStressOSParams(outDir, stressNoPack, flags.UseSudo)
	applyStressNetPlanToParams(params, netPlan, logger)

	// 过滤步骤（-s / -e 支持）
	allSteps := stresssteps.GetAllSteps()
	steps := filterSteps(allSteps, flags)
	if len(steps) == 0 {
		logger.Info("No steps to execute after filtering")
		return nil
	}

	logger.Info("Steps to execute: %d", len(steps))
	for _, s := range steps {
		logger.Info("  [%s] %s", s.ID, s.Name)
	}

	// 拆出三类步骤（同 collect.go 模式）：
	//   1. connectivityStep（S-01）：Phase 1 专用
	//   2. finalizeStep（S-11）：后置步骤，Phase 3 单独驱动
	//   3. mainSteps：其余 per-host 步骤（Phase 2 主循环）
	var connectivityStep *runner.Step
	var finalizeStep *runner.Step
	var mainSteps []*runner.Step
	for _, s := range steps {
		switch s.Name {
		case "Check Connectivity":
			connectivityStep = s
		case "Finalize stress report":
			finalizeStep = s
		default:
			mainSteps = append(mainSteps, s)
		}
	}
	totalSteps := len(steps)

	// Phase 1：连通性检查
	connResult, err := RunConnectivityPhase(connectivityStep, flags.Targets, flags, params, logger, 0, totalSteps, nil)
	if err != nil {
		return err
	}
	hostInfos := connResult.HostInfos

	defer func() {
		for _, info := range hostInfos {
			info.Executor.Close()
		}
	}()

	if len(hostInfos) == 0 {
		return fmt.Errorf("no reachable hosts after connectivity check")
	}

	// Phase 2：逐主机步骤（mainSteps）
	sharedResults := map[string]interface{}{
		"output_dir": outDir,
	}
	phaseResult := RunPerHostStepsEx(mainSteps, hostInfos, params, flags, logger,
		connResult.NextStepIndex, totalSteps, sharedResults, stressExecFactory, nil)

	// YAC：S-08 已完成节点间 ping；iperf3 首节点服务端，其余各节点依次作客户端。
	if netPlan.Enabled && netPlan.Mode == stressNetModeYAC && len(hostInfos) >= 2 {
		targetHosts := make([]runner.TargetHost, 0, len(hostInfos))
		for _, info := range hostInfos {
			targetHosts = append(targetHosts, runner.TargetHost{
				Host:     info.Host,
				Executor: stressExecFactory(info.Executor),
			})
		}
		srvInfo := findHostInfoByTarget(hostInfos, netPlan.IperfServer)
		if srvInfo == nil {
			srvInfo = hostInfos[0]
		}
		clientHosts := netPlan.IperfClients
		if len(clientHosts) == 0 {
			clientHosts = []string{hostInfos[1].Host}
		}
		iperfIdx := connResult.NextStepIndex + len(mainSteps)
		iperfStepID := stresssteps.StepIDByName("Network benchmark (ping latency)")
		srvCtx := &runner.StepContext{
			Executor:      stressExecFactory(srvInfo.Executor),
			Logger:        logger,
			Params:        params,
			Results:       sharedResults,
			TargetHosts:   targetHosts,
			CurrentStepID: iperfStepID,
			StepIndex:     iperfIdx,
			TotalSteps:    totalSteps,
		}
		var clientCtxs []*runner.StepContext
		for _, ch := range clientHosts {
			cliInfo := findHostInfoByTarget(hostInfos, ch)
			if cliInfo == nil {
				logger.Warn("YAC iperf3: skip client %s (not in connected hosts)", ch)
				continue
			}
			clientCtxs = append(clientCtxs, &runner.StepContext{
				Executor:      stressExecFactory(cliInfo.Executor),
				Logger:        logger,
				Params:        params,
				Results:       sharedResults,
				TargetHosts:   targetHosts,
				CurrentStepID: iperfStepID,
				StepIndex:     iperfIdx,
				TotalSteps:    totalSteps,
			})
		}
		if len(clientCtxs) == 0 {
			logger.Error("YAC iperf3: no client hosts available")
		} else if err := stresssteps.RunIperf3YAC(srvCtx, clientCtxs); err != nil {
			logger.Error("YAC iperf3 benchmark failed: %v", err)
			if phaseResult.LastError == nil {
				phaseResult.LastError = err
			}
		}
	}

	// Phase 3：后置步骤（S-11 Finalize）
	// 在所有 per-host 步骤完成后执行，传入完整 TargetHosts 以便 summary 列出所有主机。
	if finalizeStep != nil {
		targetHosts := make([]runner.TargetHost, 0, len(hostInfos))
		for _, info := range hostInfos {
			targetHosts = append(targetHosts, runner.TargetHost{
				Host:     info.Host,
				Executor: stressExecFactory(info.Executor),
			})
		}
		postIdx := connResult.NextStepIndex + len(mainSteps)

		// 聚合各主机的 errors/warnings（从 sharedResults 无法读取 per-host results；
		// Phase 3 依赖 RunPerHostStepsEx 中将 sharedResults 共享给各主机步骤写入，
		// 因此这里直接从 sharedResults 读取汇总后的 stress_errors/stress_warnings）
		finalCtx := &runner.StepContext{
			Executor:    stressExecFactory(hostInfos[0].Executor),
			Logger:      logger,
			Params:      params,
			Results:     sharedResults,
			TargetHosts: targetHosts,
			StepIndex:   postIdx,
			TotalSteps:  totalSteps,
		}
		result := runner.RunStep(finalizeStep, finalCtx)
		if !result.Success && !result.Skipped {
			logger.Error("Finalize step S-11 failed: %v", result.Error)
		}
	}

	if phaseResult.LastError != nil {
		logger.Error("stressos completed with errors")
		logger.Info("Partial results saved to: %s", outDir)
		logger.Info("Check debug logs at: %s", logger.DebugLogPath())
		return phaseResult.LastError
	}

	logger.Info("stressos completed successfully")
	archive.LogSummary(logger, outDir, sharedResults)
	archive.PrintTerminalSummary("Stress results", "Packaged file", outDir, sharedResults)
	return nil
}

// buildStressOSParams 构造 stressos 步骤所需的 ctx.Params map。
func buildStressOSParams(outDir string, noPack, useSudo bool) map[string]interface{} {
	doCPU := stressDoCPU
	doMEM := stressDoMEM
	doIO := stressDoIO

	// --deps-only：禁用所有压测步骤
	if stressDepsOnly {
		doCPU = false
		doMEM = false
		doIO = false
	}

	params := map[string]interface{}{
		"output_dir":         outDir,
		"archive_no_pack":    noPack,
		"install_deps":       stressInstallDeps,
		"deps_only":          stressDepsOnly,
		"stress_cpu":         doCPU,
		"stress_mem":         doMEM,
		"stress_io":          doIO,
		"stress_net":         stressDoNET,
		"stress_cmd_timeout": stressCmdTimeoutSec,
		"cpu_time":           stressCPUTimeSec,
		"cpu_max_prime":      stressCPUMaxPrime,
		"cpu_numa_bind":      stressCPUNumaBind,
		"cpu_numa_node":      stressCPUNumaNode,
		"mem_time":           stressMEMTimeSec,
		"mem_block_size":     stressMEMBlockSize,
		"mem_total_size":     stressMEMTotalSize,
		"mem_numa_bind":      stressMEMNumaBind,
		"io_dir":             stressIODir,
		"io_size":            stressIOSize,
		"io_direct":          stressIODirect,
		"io_time":            stressIOTimeSec,
		"keep_io_files":      stressKeepIOFiles,
		"io_engine":          stressIOEngine,
		"io_engine_data":     stressIOEngineData,
		"io_engine_logwrite": stressIOEngineLogwrite,
		"io_engine_randrw":   stressIOEngineRandrw,
		"io_engine_randread": stressIOEngineRandread,
		"io_bs_randrw":       stressIOBSRandrw,
		"io_bs_randread":     stressIOBSRandread,
		"io_bs_logwrite":     stressIOBSLogwrite,
		"io_iodepth":         stressIOIodepth,
		"io_numjobs":         stressIONumjobs,
		"io_rwmix_read":      stressIORwmixRead,
		"io_fsync":           stressIOFsync,
		"io_device":          stressIODevice,
		"io_device_mode":     stressIODeviceMode,
		"danger_io_device":   stressDangerIODevice,
		"stress_net_mode":    stressNetModePing,
		"iperf3_time":        60,
		"iperf3_port":        5201,
		"rules_file":         stressRulesFile,
		"sudo":               useSudo,
	}
	for k, v := range buildOSYumISOParams() {
		params[k] = v
	}
	return params
}

func findHostInfoByTarget(infos []*HostInfo, host string) *HostInfo {
	for _, info := range infos {
		if info.Host == host {
			return info
		}
	}
	return nil
}

// printStressStepCatalog 打印 stressos 步骤目录（-l 标志）。
func printStressStepCatalog() {
	steps := stresssteps.GetAllSteps()
	fmt.Printf("%-8s %-40s %s\n", "ID", "Name", "Description")
	fmt.Printf("%-8s %-40s %s\n", "----", "----", "-----------")
	for _, s := range steps {
		desc := s.Description
		if desc == "" {
			if s.Optional {
				desc = "(optional)"
			} else {
				desc = ""
			}
		}
		fmt.Printf("%-8s %-40s %s\n", s.ID, s.Name, desc)
	}
}
