package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	commonmssql "github.com/yinstall/internal/common/mssql"
	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/logging"
	"github.com/yinstall/internal/runner"
)

var mssqlInstallCmd = &cobra.Command{
	Use:          "install",
	Short:        "Install SQL Server on Windows",
	Long:         `Install SQL Server with optional Windows OS baseline (W-*) then MS-* steps.`,
	RunE:         runMssqlInstall,
	SilenceUsage: true,
}

func init() {
	registerMssqlInstallFlags(mssqlInstallCmd)
	registerMssqlListInstancesFlag(mssqlInstallCmd)
}

func runMssqlInstall(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()
	if flags.ListSteps {
		PrintMssqlInstallStepCatalog(mssqlSkipOS)
		return nil
	}
	if err := applyMssqlLocalDefaults(&flags); err != nil {
		return err
	}
	applyMssqlRemoteSoftwareDefaults(cmd, &flags)
	stage, err := normalizeMssqlInstallStage(mssqlStage)
	if err != nil {
		return err
	}
	mssqlStage = stage
	if err := validateMssqlInstallStage(stage, flags.DryRun, flags.Precheck); err != nil {
		return err
	}
	if err := validateMssqlPortFlag(); err != nil {
		return err
	}
	profile := buildMssqlProfile(nil)
	allSteps := buildMssqlAllSteps(mssqlSkipOS, profile)
	steps := ensureConnectivityStep(allSteps, filterSteps(allSteps, flags))
	steps = filterMssqlInstallStepsByStage(steps, stage)
	needsSA := mssqlInstallRequiresSAPassword(steps)
	if needsSA && strings.TrimSpace(commonmssql.ResolveSAPassword(mssqlSaPassword, true)) == "" && !flags.DryRun && !flags.Precheck {
		return fmt.Errorf("--mssql-sa-password is required when MS-002..MS-020 steps are included")
	}
	if mssqlMaxMemoryMB < 0 {
		return fmt.Errorf("invalid --mssql-max-memory-mb: %d", mssqlMaxMemoryMB)
	}
	if mssqlMaxMemoryMB > 0 && mssqlMaxMemoryMB < 512 {
		return fmt.Errorf("invalid --mssql-max-memory-mb: %d (minimum 512)", mssqlMaxMemoryMB)
	}
	if mssqlMaxMemoryMB == 0 && mssqlMemoryPercent != 0 {
		if err := validateMemoryPercent("--mssql-memory-percent", mssqlMemoryPercent); err != nil {
			return err
		}
	}

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("mssql-%s", time.Now().Format("20060102-150405"))
	}
	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer logger.Close()

	params := buildMssqlParams(flags)
	setMssqlSAPasswordParam(params, needsSA)
	params["mssql_stage"] = stage
	if done, err := runMssqlListInstancesIfRequested(flags, logger, flags.Targets, params); err != nil {
		return err
	} else if done {
		return nil
	}
	return RunMssqlInstallOnHosts(flags, logger, params)
}

// RunMssqlInstallOnHosts runs B-001 → pre W-* → MS-* → post W-*.
func RunMssqlInstallOnHosts(flags GlobalFlags, logger *logging.Logger, params map[string]interface{}) error {
	profile := buildMssqlProfile(params)
	allSteps := buildMssqlAllSteps(mssqlSkipOS, profile)
	steps := ensureConnectivityStep(allSteps, filterSteps(allSteps, flags))
	stage := commonmssql.StageAll
	if v, ok := params["mssql_stage"].(string); ok && strings.TrimSpace(v) != "" {
		stage = strings.TrimSpace(v)
	}
	steps = filterMssqlInstallStepsByStage(steps, stage)
	if len(steps) == 0 {
		logger.Info("No steps to execute after filtering")
		return nil
	}

	groups := splitMssqlSteps(steps)
	logger.Info("Steps to execute: %d", len(steps))
	for _, s := range steps {
		logger.Info("  [%s] %s", s.ID, s.Name)
	}

	planned := runner.CountNonOptionalSteps(steps)
	progress := runner.NewStepProgress(planned)
	totalSteps := progress.Total()
	sharedResults := make(map[string]interface{})

	hostInfos, stepIdx, err := runMssqlConnectivityPhase(groups.b001, flags, params, logger, 0, totalSteps, progress, sharedResults)
	if err != nil {
		return err
	}
	defer closeHostInfos(hostInfos)

	if groups.ms001 != nil {
		logger.Info("======== Phase: MS-001 platform detect ========")
		res := RunPerHostStepsEx([]*runner.Step{groups.ms001}, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		stepIdx++
		if res.LastError != nil {
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
	}

	if len(groups.preOS) > 0 && !mssqlSkipOS {
		logger.Info("======== Phase: Windows OS pre-instance ========")
		res := RunPerHostStepsEx(groups.preOS, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		stepIdx += len(groups.preOS)
		if res.LastError != nil {
			sharedResults["win_os_pre_instance_failed"] = true
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
		sharedResults["win_os_pre_instance_ok"] = true
	}

	if len(groups.mssqlSteps) > 0 {
		logger.Info("======== Phase: MSSQL install ========")
		res := RunPerHostStepsEx(groups.mssqlSteps, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		stepIdx += len(groups.mssqlSteps)
		if res.LastError != nil {
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
	}

	if len(groups.postOS) > 0 && !mssqlSkipOS {
		logger.Info("======== Phase: Windows OS post-instance ========")
		res := RunPerHostStepsEx(groups.postOS, hostInfos, params, flags, logger, stepIdx, totalSteps, sharedResults, nil, progress)
		if res.LastError != nil {
			return res.LastError
		}
		if flags.Precheck && res.PrecheckFailed {
			return fmt.Errorf("precheck failed")
		}
	}

	logger.Info("MSSQL install workflow completed")
	return nil
}

func closeHostInfos(hostInfos []*HostInfo) {
	for _, h := range hostInfos {
		if h != nil && h.Executor != nil {
			h.Executor.Close()
		}
	}
}

func runMssqlConnectivityPhase(
	connectivityStep *runner.Step,
	flags GlobalFlags,
	params map[string]interface{},
	logger *logging.Logger,
	stepIndex int,
	totalSteps int,
	progress *runner.StepProgress,
	sharedResults map[string]interface{},
) ([]*HostInfo, int, error) {
	if connectivityStep == nil {
		return nil, stepIndex, fmt.Errorf("B-001 step missing")
	}
	logger.Info("======== Phase 1: Connectivity check ========")
	var hostInfos []*HostInfo
	precheckFailed := false

	for _, target := range flags.Targets {
		executor, err := createWindowsExecutor(target, flags, logger, connectivityStep.ID)
		if err != nil {
			logger.Error("Failed to connect to %s: %v", target, err)
			if flags.Precheck {
				precheckFailed = true
				continue
			}
			return nil, stepIndex, fmt.Errorf("connectivity failed for %s: %w", target, err)
		}
		ctx := &runner.StepContext{
			Executor:       &runnerExecAdapter{e: executor},
			Logger:         logger,
			Params:         params,
			DryRun:         flags.DryRun,
			Precheck:       flags.Precheck,
			Results:        sharedResults,
			ForceAll:       flags.ForceAll,
			ForceSteps:     flags.ForceSteps,
			StepIndex:      stepIndex,
			TotalSteps:     totalSteps,
			Progress:       progress,
			TargetPlatform: "windows",
		}
		result := runner.RunStep(connectivityStep, ctx)
		if result.Error != nil && !result.Skipped {
			executor.Close()
			if flags.Precheck {
				precheckFailed = true
				continue
			}
			return nil, stepIndex, fmt.Errorf("connectivity check failed for %s: %w", target, result.Error)
		}
		hostInfos = append(hostInfos, &HostInfo{Host: target, Executor: executor, TargetPlatform: "windows"})
	}
	if precheckFailed {
		return hostInfos, stepIndex + 1, fmt.Errorf("precheck connectivity failed")
	}
	if len(hostInfos) == 0 {
		return nil, stepIndex, fmt.Errorf("no reachable hosts")
	}
	return hostInfos, stepIndex + 1, nil
}

// PrintMssqlInstallStepCatalog lists mssql install steps.
func PrintMssqlInstallStepCatalog(skipOS bool) {
	profile := commonwin.ProfileMssql()
	steps := buildMssqlAllSteps(skipOS, profile)
	printStepSection("Connectivity", []*runner.Step{steps[0]})
	if !skipOS {
		g := splitMssqlSteps(steps)
		printStepSection("Windows OS pre-instance", g.preOS)
		printStepSection("Windows OS post-instance", g.postOS)
	}
	var ms []*runner.Step
	for _, s := range steps {
		if len(s.ID) >= 3 && s.ID[:3] == "MS-" {
			ms = append(ms, s)
		}
	}
	printStepSection("MSSQL install", ms)
	fmt.Println("Note: filtered by -s/-e and --skip-os")
}
