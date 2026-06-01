// collect_run.go - collect 执行管线（独立子命令与 os/db 安装后挂钩共用）
package cli

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/common/archive"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
	collectsteps "github.com/yinstall/internal/steps/collect"
)

// CollectPipelineOpts 配置一次 collect 运行（可复用已有 SSH 连接）。
type CollectPipelineOpts struct {
	Profile       string
	OutDir        string
	NoPack        bool
	Params        map[string]interface{}
	Flags         GlobalFlags
	Logger        *logging.Logger
	HostInfos     []*HostInfo // 非 nil 时跳过 Phase 1 建连/连通步，直接使用已有连接
	SharedResults map[string]interface{}
	// IgnoreStepFilters 为 true 时不应用全局 -s/-e（安装后挂钩须仅用 profile 决定范围）
	IgnoreStepFilters bool
	// Progress 非 nil 时与安装阶段共用进度（分母已含 collect；跳过 R-001 等不计入）
	Progress *runner.StepProgress
}

// runCollectPipeline 执行 collect 三阶段编排；返回非 nil 表示主循环或连通阶段失败。
func runCollectPipeline(opts CollectPipelineOpts) error {
	allSteps := collectsteps.GetAllSteps()
	cats := ExpandProfile(opts.Profile)
	profileFiltered := FilterStepsByCategories(allSteps, cats)

	flags := opts.Flags
	if opts.IgnoreStepFilters {
		flags.IncludeSteps = nil
		flags.ExcludeSteps = nil
		flags.ListSteps = false
	}
	steps := filterSteps(profileFiltered, flags)
	if len(steps) == 0 {
		opts.Logger.Info("No collect steps to execute after filtering")
		return nil
	}

	postStepIDs := map[string]bool{"R-028": true, "R-029": true, "R-030": true}
	var connectivityStep *runner.Step
	var postSteps []*runner.Step
	var mainSteps []*runner.Step
	for _, s := range steps {
		switch {
		case s.ID == "R-001":
			connectivityStep = s
		case postStepIDs[s.ID]:
			postSteps = append(postSteps, s)
		default:
			mainSteps = append(mainSteps, s)
		}
	}
	skipConnectivity := len(opts.HostInfos) > 0 && connectivityStep != nil
	progress := opts.Progress
	if progress == nil {
		progress = runner.NewStepProgress(runner.CountCollectProgressSteps(steps, skipConnectivity))
	}

	sharedResults := opts.SharedResults
	if sharedResults == nil {
		sharedResults = map[string]interface{}{}
	}
	if _, ok := sharedResults["output_dir"]; !ok {
		sharedResults["output_dir"] = opts.OutDir
	}

	var hostInfos []*HostInfo
	var connResult *ConnectivityResult
	var err error

	if len(opts.HostInfos) > 0 {
		hostInfos = opts.HostInfos
		connResult = &ConnectivityResult{
			HostInfos:     hostInfos,
			NextStepIndex: 0,
		}
		if connectivityStep != nil {
			connResult.NextStepIndex = 1
		}
	} else {
		connResult, err = RunConnectivityPhase(connectivityStep, flags.Targets, flags, opts.Params, opts.Logger, 0, progress.Total(), progress)
		if err != nil {
			return err
		}
		hostInfos = connResult.HostInfos
	}

	if len(hostInfos) == 0 {
		return fmt.Errorf("no reachable hosts for collect")
	}

	phaseResult := RunPerHostStepsEx(mainSteps, hostInfos, opts.Params, flags, opts.Logger, connResult.NextStepIndex, progress.Total(), sharedResults, collectExecFactory, progress)

	if len(postSteps) > 0 {
		targetHosts := make([]runner.TargetHost, 0, len(hostInfos))
		for _, info := range hostInfos {
			targetHosts = append(targetHosts, runner.TargetHost{
				Host:     info.Host,
				Executor: collectExecFactory(info.Executor),
			})
		}
		for _, step := range postSteps {
			ctx := &runner.StepContext{
				Executor:    collectExecFactory(hostInfos[0].Executor),
				Logger:      opts.Logger,
				Params:      opts.Params,
				Results:     sharedResults,
				TargetHosts: targetHosts,
				Progress:    progress,
			}
			result := runner.RunStep(step, ctx)
			if !result.Success && !result.Skipped {
				opts.Logger.Error("Post-step %s failed: %v", step.ID, result.Error)
			}
		}
	}

	if phaseResult.LastError != nil {
		return phaseResult.LastError
	}
	return nil
}

// sanitizeParamsForArchive 拷贝安装 Params 并脱敏密码类字段（写入 install-run.json）。
func sanitizeParamsForArchive(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "password") || strings.Contains(lk, "secret") {
			out[k] = "***"
			continue
		}
		out[k] = v
	}
	return out
}

// buildInstallParamsSnapshot 供 R-003 写入 install-run.json。
func buildInstallParamsSnapshot(command, runID string, params map[string]interface{}, stepIDs []string) map[string]interface{} {
	return map[string]interface{}{
		"command": command,
		"run_id":  runID,
		"params":  sanitizeParamsForArchive(params),
		"steps":   stepIDs,
	}
}

func collectStepIDs(steps []*runner.Step) []string {
	ids := make([]string, 0, len(steps))
	for _, s := range steps {
		ids = append(ids, s.ID)
	}
	return ids
}

// CountArchiveCollectSteps 返回安装挂钩 collect（install-os / install-db profile）过滤后的步骤数。
func CountArchiveCollectSteps(hook string, yacMode bool, flags GlobalFlags) int {
	profile := ProfileForInstallArchive(hook, yacMode)
	allSteps := collectsteps.GetAllSteps()
	cats := ExpandProfile(profile)
	profileFiltered := FilterStepsByCategories(allSteps, cats)
	archiveFlags := flags
	archiveFlags.IncludeSteps = nil
	archiveFlags.ExcludeSteps = nil
	archiveFlags.ListSteps = false
	steps := filterSteps(profileFiltered, archiveFlags)
	return runner.CountCollectProgressSteps(steps, true)
}

// runInstallArchiveCollect 在安装成功后执行挂钩 collect；失败只记日志，不导致安装失败。
func runInstallArchiveCollect(
	hook string,
	yacMode bool,
	progress *runner.StepProgress,
	hostInfos []*HostInfo,
	installParams map[string]interface{},
	installResults map[string]interface{},
	flags GlobalFlags,
	logger *logging.Logger,
) {
	if !flags.ArchiveOnSuccess || flags.DryRun || flags.Precheck {
		return
	}
	if len(hostInfos) == 0 {
		logger.Warn("--archive: no hosts, skipping post-install collect")
		return
	}

	profile := ProfileForInstallArchive(hook, yacMode)
	subdir := "install-os"
	if hook == "db" {
		subdir = "install-db"
	}
	outDir, outFallback, err := archive.ResolveOutputDir(flags.Output, subdir)
	if err != nil {
		logger.Warn("--archive: resolve output dir: %v", err)
		return
	}
	if err := archive.EnsureOutputDir(outDir); err != nil {
		logger.Warn("--archive: mkdir output: %v", err)
		return
	}

	logger.Info("======== Post-install archive collect (profile=%s) ========", profile)
	if progress != nil {
		logger.Info("Step progress continues from %d / %d (collect phase)", progress.Next(), progress.Total())
	}
	logger.Info("Archive output directory: %s", outDir)
	if outFallback {
		logger.Warn("Archive output under temp dir (./output not writable): %s", outDir)
	}

	collectParams := buildCollectParams(outDir, false, flags.UseSudo)
	collectParams["profile"] = profile
	if v, ok := installParams["params"].(map[string]interface{}); ok {
		if u, ok := v["os_user"].(string); ok && u != "" {
			collectParams["os_user"] = u
		}
	}

	sharedResults := map[string]interface{}{"output_dir": outDir}
	if installParams != nil {
		sharedResults["install_params"] = installParams
	}
	if installResults != nil {
		if envFile, ok := installResults["env_file"].(string); ok && envFile != "" {
			sharedResults["env_file"] = envFile
			collectParams["env_file"] = envFile
		}
		if cn, ok := installResults["cluster_name"].(string); ok && cn != "" {
			collectParams["cluster_name"] = cn
		}
	}

	if err := runCollectPipeline(CollectPipelineOpts{
		Profile:           profile,
		OutDir:            outDir,
		Params:            collectParams,
		Flags:             flags,
		Logger:            logger,
		HostInfos:         hostInfos,
		SharedResults:     sharedResults,
		IgnoreStepFilters: true,
		Progress:          progress,
	}); err != nil {
		logger.Warn("--archive: collect failed (installation still succeeded): %v", err)
		logger.Info("Partial archive may exist at: %s", outDir)
		return
	}

	archive.LogSummary(logger, outDir, sharedResults)
	archive.PrintTerminalSummary("Install archive", "Packaged file", outDir, sharedResults)
}
