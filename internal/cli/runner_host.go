package cli

// runner_host.go - 通用两阶段主机执行编排
// 将 os.go / db.go 中重复的 Phase 1（连通性）+ Phase 2（Global/PerHost 步骤）提取为共享函数。
// db.go 中 DB 专用全局预检（C-001/VIP/SCAN）与 DB StepContext（单 ctx + TargetHosts）仍保留在 db.go。

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
)

// ExecutorAdapterFactory 将 ssh.Executor 转换为 runner.Executor 的工厂函数类型。
// collect 子命令通过此工厂注入 collectExecAdapter（支持 SSH session 超时）；
// 其余子命令传 nil，回落到默认的 runnerExecAdapter。
type ExecutorAdapterFactory func(e ssh.Executor) runner.Executor

// ConnectivityResult 连通性阶段返回结果
type ConnectivityResult struct {
	// HostInfos 成功连接并通过连通步骤的主机列表
	HostInfos []*HostInfo
	// PrecheckFailed 表示 precheck 模式下有主机连通失败
	PrecheckFailed bool
	// NextStepIndex 连通阶段结束后的步骤序号（供 Phase 2 使用）
	NextStepIndex int
}

// RunConnectivityPhase 执行 Phase 1：连通性检查。
// connectivityStep 为 B-001 或 R-001 等；stepIndex 为连通步在整体步骤列表中的序号。
// 若 connectivityStep 为 nil，则仅建连（不执行 RunStep）并返回所有 target 的 HostInfo。
// precheck 模式下单个 target 失败不会立刻返回 error，而是继续收集其它 target；
// 非 precheck 模式下任意 target 失败即返回 error。
func RunConnectivityPhase(
	connectivityStep *runner.Step,
	targets []string,
	flags GlobalFlags,
	params map[string]interface{},
	logger *logging.Logger,
	stepIndex int,
	totalSteps int,
	progress *runner.StepProgress,
) (*ConnectivityResult, error) {
	res := &ConnectivityResult{NextStepIndex: stepIndex}

	if connectivityStep == nil {
		// 无连通步：只建连，不执行 RunStep
		for _, target := range targets {
			executor, err := createExecutor(target, flags, logger, "")
			if err != nil {
				return nil, fmt.Errorf("failed to connect to %s: %w", target, err)
			}
			res.HostInfos = append(res.HostInfos, &HostInfo{Host: target, Executor: executor})
		}
		return res, nil
	}

	logger.Info("======== Phase 1: Connectivity check ========")

	var progressFrozen struct{ idx, total int }
	progressFrozenSet := false

	for ti, target := range targets {
		executor, err := createExecutor(target, flags, logger, "")
		if err != nil {
			logger.Error("Failed to connect to %s: %v", target, err)
			if flags.Precheck {
				res.PrecheckFailed = true
				continue
			}
			return nil, fmt.Errorf("connectivity check failed for %s: %w", target, err)
		}

		ctx := &runner.StepContext{
			Executor:          &runnerExecAdapter{e: executor},
			Logger:            logger,
			Params:            params,
			DryRun:            flags.DryRun,
			Precheck:          flags.Precheck,
			Results:           make(map[string]interface{}),
			LocalSoftwareDirs: flags.LocalSoftwareDirs,
			RemoteSoftwareDir: flags.RemoteSoftwareDir,
			ForceAll:          flags.ForceAll,
			ForceSteps:        flags.ForceSteps,
			ForceDeleteUser:   flags.ForceDeleteUser,
			StepIndex:         stepIndex,
			TotalSteps:        totalSteps,
			Progress:          progress,
		}
		if progress != nil && ti > 0 && progressFrozenSet {
			ctx.Progress = nil
			ctx.StepIndex = progressFrozen.idx
			ctx.TotalSteps = progressFrozen.total
		}

		result := runner.RunStep(connectivityStep, ctx)
		if progress != nil && ti == 0 {
			progressFrozen.idx = ctx.StepIndex
			progressFrozen.total = ctx.TotalSteps
			progressFrozenSet = true
		}
		if !result.Success && !result.Skipped {
			executor.Close()
			if flags.Precheck {
				res.PrecheckFailed = true
				continue
			}
			return nil, fmt.Errorf("connectivity check failed for %s: %w", target, result.Error)
		}

		res.HostInfos = append(res.HostInfos, hostInfoFromConnectivityStep(target, executor, ctx))
	}

	if flags.Precheck && res.PrecheckFailed {
		logger.Error("Connectivity precheck has failures; continuing to collect all issues.")
	}

	res.NextStepIndex = stepIndex + 1
	return res, nil
}

// PerHostRunResult 逐主机执行阶段返回结果
type PerHostRunResult struct {
	PrecheckFailed bool
	LastError      error
}

// RunPerHostSteps 执行 Phase 2：按 Global / PerHost 顺序运行 steps。
// sharedResults 为跨步骤共享的 Results map（如 collect 的 output_dir 等），
// 若为 nil 则每个 Global 步骤各自使用独立 map。
// stepIndex 为当前步骤在整体列表中的起始序号（连通步之后）。
// 函数内部不关闭 hostInfos 的 executor（调用方负责）。
func RunPerHostSteps(
	steps []*runner.Step,
	hostInfos []*HostInfo,
	params map[string]interface{},
	flags GlobalFlags,
	logger *logging.Logger,
	stepIndex int,
	totalSteps int,
	sharedResults map[string]interface{},
) *PerHostRunResult {
	return RunPerHostStepsEx(steps, hostInfos, params, flags, logger, stepIndex, totalSteps, sharedResults, nil, nil)
}

// RunPerHostStepsEx 与 RunPerHostSteps 相同，但允许通过 execFactory 注入自定义 runner.Executor 适配器。
// execFactory 为 nil 时使用默认的 runnerExecAdapter，与 RunPerHostSteps 行为完全一致。
// collect 子命令通过 collectExecFactory 注入 collectExecAdapter，以支持 SSH session 级超时。
func RunPerHostStepsEx(
	steps []*runner.Step,
	hostInfos []*HostInfo,
	params map[string]interface{},
	flags GlobalFlags,
	logger *logging.Logger,
	stepIndex int,
	totalSteps int,
	sharedResults map[string]interface{},
	execFactory ExecutorAdapterFactory,
	progress *runner.StepProgress,
) *PerHostRunResult {
	res := &PerHostRunResult{}
	if len(hostInfos) == 0 {
		return res
	}

	// makeExec 将 ssh.Executor 转换为 runner.Executor（工厂或默认）
	makeExec := execFactory
	if makeExec == nil {
		makeExec = func(e ssh.Executor) runner.Executor { return &runnerExecAdapter{e: e} }
	}

	// 构建 TargetHosts（供 Global 步骤使用）
	targetHosts := make([]runner.TargetHost, 0, len(hostInfos))
	for _, info := range hostInfos {
		targetHosts = append(targetHosts, runner.TargetHost{
			Host:     info.Host,
			Executor: makeExec(info.Executor),
		})
	}

	// 将 steps 分为 Global 与 PerHost，保持注册顺序
	var globalSteps []*runner.Step
	var perHostSteps []*runner.Step
	for _, step := range steps {
		if step.Global {
			globalSteps = append(globalSteps, step)
		} else {
			perHostSteps = append(perHostSteps, step)
		}
	}

	if len(steps) > 0 {
		logger.Info("======== Phase 2: Executing steps ========")
	}

	// 执行 Global 步骤（跨节点，仅执行一次，使用首节点 executor）
	if len(globalSteps) > 0 {
		logger.Info("-------- Global steps (all nodes) --------")
		globalResults := sharedResults
		if globalResults == nil {
			globalResults = make(map[string]interface{})
		}
		for i, step := range globalSteps {
			ctx := &runner.StepContext{
				Executor:          makeExec(hostInfos[0].Executor),
				Logger:            logger,
				Params:            params,
				DryRun:            flags.DryRun,
				Precheck:          flags.Precheck,
				Results:           globalResults,
				OSInfo:            hostInfos[0].OSInfo,
				LocalSoftwareDirs: flags.LocalSoftwareDirs,
				RemoteSoftwareDir: flags.RemoteSoftwareDir,
				ForceAll:          flags.ForceAll,
				ForceSteps:        flags.ForceSteps,
				ForceDeleteUser:   flags.ForceDeleteUser,
				StepIndex:         stepIndex + i,
				TotalSteps:        totalSteps,
				TargetHosts:       targetHosts,
				Progress:          progress,
			}

			result := runner.RunStep(step, ctx)
			if !result.Success && !result.Skipped {
				logger.Error("Step %s failed: %v", step.ID, result.Error)
				if flags.Precheck {
					res.PrecheckFailed = true
					continue
				}
				res.LastError = result.Error
				return res
			}
		}
		stepIndex += len(globalSteps)
	}

	// 执行逐主机步骤
	perStepFrozen := make([]bool, len(perHostSteps))
	perStepProgress := make([]struct{ idx, total int }, len(perHostSteps))

	for hi, info := range hostInfos {
		logger.Info("-------- Host: %s --------", info.Host)

		hostResults := make(map[string]interface{})
		// 将 sharedResults 中的条目复制到 hostResults，使步骤能读取跨主机共享键
		for k, v := range sharedResults {
			hostResults[k] = v
		}
		if id := connectivityIdentityForHost(info); len(id) > 0 {
			hostResults["stress_connectivity_identity"] = id
		}

		for i, step := range perHostSteps {
			ctx := &runner.StepContext{
				Executor:          makeExec(info.Executor),
				Logger:            logger,
				Params:            params,
				DryRun:            flags.DryRun,
				Precheck:          flags.Precheck,
				Results:           hostResults,
				OSInfo:            info.OSInfo,
				LocalSoftwareDirs: flags.LocalSoftwareDirs,
				RemoteSoftwareDir: flags.RemoteSoftwareDir,
				ForceAll:          flags.ForceAll,
				ForceSteps:        flags.ForceSteps,
				ForceDeleteUser:   flags.ForceDeleteUser,
				StepIndex:         stepIndex + i,
				TotalSteps:        totalSteps,
				Progress:          progress,
			}
			if progress != nil && hi > 0 && perStepFrozen[i] {
				ctx.Progress = nil
				ctx.StepIndex = perStepProgress[i].idx
				ctx.TotalSteps = perStepProgress[i].total
			}

			result := runner.RunStep(step, ctx)
			if progress != nil && hi == 0 {
				perStepProgress[i].idx = ctx.StepIndex
				perStepProgress[i].total = ctx.TotalSteps
				perStepFrozen[i] = true
			}
			if !result.Success && !result.Skipped {
				logger.Error("Step %s failed: %v", step.ID, result.Error)
				if flags.Precheck {
					res.PrecheckFailed = true
					continue
				}
				res.LastError = result.Error
				break
			}
		}

		if res.LastError != nil {
			break
		}
	}

	return res
}

// hostInfoFromConnectivityStep 从连通性步骤上下文组装 HostInfo（含 S-01/B-001 探测快照）。
func hostInfoFromConnectivityStep(target string, executor ssh.Executor, ctx *runner.StepContext) *HostInfo {
	return &HostInfo{
		Host:        target,
		Executor:    executor,
		OSInfo:      ctx.OSInfo,
		Hostname:    resultString(ctx.Results, "hostname"),
		CPUCores:    resultString(ctx.Results, "cpu_cores"),
		MemoryTotal: resultString(ctx.Results, "total_memory"),
	}
}

// connectivityIdentityForHost 将 HostInfo 中的连通性快照转为 per-host Results 条目。
func connectivityIdentityForHost(info *HostInfo) map[string]string {
	if info == nil {
		return nil
	}
	m := make(map[string]string)
	if s := strings.TrimSpace(info.Hostname); s != "" {
		m["hostname"] = s
	}
	if s := strings.TrimSpace(info.CPUCores); s != "" {
		m["cpu_cores"] = s
	}
	if s := strings.TrimSpace(info.MemoryTotal); s != "" {
		m["memory_total"] = s
	}
	return m
}

func resultString(results map[string]interface{}, key string) string {
	if results == nil {
		return ""
	}
	v, ok := results[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}
