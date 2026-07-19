// standby_cleanup_failed_expansion.go - 清理失败扩容产物
// 默认只打印解决方案；--standby-cleanup-on-failure 或 -F/--force-steps 时执行安全清理

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepCleanupFailedExpansion 清理失败扩容产物步骤
func stepCleanupFailedExpansion() *runner.Step {
	return &runner.Step{
		Name:        "Cleanup Failed Expansion",
		Description: "Print cleanup remediation; with --standby-cleanup-on-failure or force, execute safe cleanup (DANGEROUS)",
		Tags:        []string{"standby", "cleanup", "dangerous"},
		Dangerous:   true,
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			cleanupOnFailure := ctx.GetParamBool("standby_cleanup_on_failure", false)
			isForce := ctx.IsForceStep()
			if !cleanupOnFailure && !isForce {
				return fmt.Errorf("cleanup step requires %s (or global -F) or --standby-cleanup-on-failure flag", ctx.ForceStepsHint())
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Cleanup Failed Expansion")
			standbyLogPhase(ctx, "check-start", "cluster status for cleanup")
			_ = EnsureStandbyCEPath(ctx, "")
			auto := ctx.GetParamBool("standby_cleanup_on_failure", false) || ctx.IsForceStep()
			if err := RunFailedExpansionCleanup(ctx, auto); err != nil {
				return err
			}
			standbyLogPhase(ctx, "check-done", fmt.Sprintf("auto=%v", auto))
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

// RunFailedExpansionCleanup 打印扩备失败解决方案；execute=true 时执行安全清理（CE group remove --clean / SE node remove --clean）。
func RunFailedExpansionCleanup(ctx *runner.StepContext, execute bool) error {
	if ctx == nil {
		return fmt.Errorf("step context is nil")
	}
	clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
	user := GetPrimaryOSUser(ctx)
	envFile, err := GetPrimaryEnvFile(ctx)
	if err != nil {
		beginPort := ctx.GetParamInt("db_begin_port", 1688)
		homeDir, hErr := commonos.GetUserHomeDir(ctx, user)
		if hErr != nil {
			return fmt.Errorf("cleanup: env file: %w", err)
		}
		envFile = commonos.DetermineEnvFile(homeDir, beginPort)
	}

	ctx.Logger.Info("WARNING: Failed-expansion cleanup path (standby side only; never wipe primary yasdb_data/YFS)")
	statusRes, _ := commonos.ExecuteAsUserWithEnv(ctx, user, envFile, fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), true)
	if statusRes != nil {
		ctx.Logger.Info("Current cluster status:")
		for _, line := range strings.Split(statusRes.GetStdout(), "\n") {
			if line != "" {
				ctx.Logger.Info("  %s", line)
			}
		}
	}

	useCE := ctx.GetParamBool("standby_ce_path", false)
	if useCE {
		return runCEFailedCleanup(ctx, user, envFile, clusterName, execute)
	}
	return runSEFailedCleanup(ctx, user, envFile, clusterName, execute)
}

func runCEFailedCleanup(ctx *runner.StepContext, user, envFile, clusterName string, execute bool) error {
	groupOut := ""
	if gRes, gErr := commonos.ExecuteAsUserWithEnv(ctx, user, envFile,
		fmt.Sprintf("yasboot cluster status -c %s -b group -d", clusterName), true); gErr == nil && gRes != nil {
		groupOut = gRes.GetStdout()
		ctx.Logger.Info("Current group status:")
		for _, line := range strings.Split(groupOut, "\n") {
			if line != "" {
				ctx.Logger.Info("  %s", line)
			}
		}
		for _, line := range FormatCEGroupRoleSummary(groupOut) {
			ctx.Logger.Info("CE group role: %s", line)
		}
	}
	prim, stbys := ParseCEGroupNamesByRole(groupOut)
	baseline := SplitCSVParam(ctx.GetParamString("ce_baseline_standby_groups", ""))
	expected := ctx.GetParamString("ce_new_group_name", "")
	if expected == "" {
		expected = ctx.GetParamString("ce_expected_new_group", "")
	}
	toClean := SelectCEGroupsForFailedCleanup(baseline, stbys, expected)
	ctx.Logger.Info("CE cleanup scope: baseline_standby=[%s] current_standby=[%s] target=[%s]",
		strings.Join(baseline, ","), strings.Join(stbys, ","), strings.Join(toClean, ","))
	if len(baseline) > 0 {
		ctx.Logger.Info("Protected existing standby groups (will NOT clean): [%s]", strings.Join(baseline, ","))
	}
	cmds, cErr := BuildSafeCECleanupCommands(clusterName, toClean, prim, true)
	if cErr != nil {
		ctx.Logger.Warn("Safe cleanup command build: %v", cErr)
		ctx.Logger.Info("%s", FormatCEExpansionFailureRemediation(clusterName, execute))
		ctx.Logger.Info("REFUSING blanket group remove --clean without group-id when existing standby groups are present.")
		return fmt.Errorf("cannot determine failed CE group to clean (baseline standby=%v); clean manually with --group-ids <newN> --clean --ce", baseline)
	}

	ctx.Logger.Info("%s", FormatCEExpansionFailureRemediation(clusterName, execute))
	ctx.Logger.Info("Safe cleanup command(s) for THIS expansion only:")
	for _, c := range cmds {
		ctx.Logger.Info("  %s", c)
	}
	ctx.Logger.Info("RED LINE: do NOT group remove primary (ceg1) or pre-existing open standby groups.")

	if !execute {
		ctx.Logger.Info("Cleanup NOT executed (print-only). Re-run with --standby-cleanup-on-failure or -F to auto-clean.")
		return nil
	}

	sysPass := strings.TrimSpace(ctx.GetParamString("db_admin_password", ""))
	for _, c := range cmds {
		cmd := c
		if sysPass != "" && !strings.Contains(cmd, " -p ") {
			cmd = cmd + " -p " + commonos.ShellSingleQuote(sysPass)
		}
		ctx.Logger.Info("Executing: %s", loggingRedactPassword(cmd))
		res, err := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, user, envFile, cmd)
		out := ""
		if res != nil {
			out = YasbootCombinedOutput(res.GetStdout(), res.GetStderr())
		}
		if err != nil || (res != nil && res.GetExitCode() != 0) {
			ctx.Logger.Error("cleanup command failed: %v\n%s", err, out)
			return fmt.Errorf("auto cleanup failed: %v; output: %s", err, strings.TrimSpace(out))
		}
		if out != "" {
			for _, line := range strings.Split(out, "\n") {
				if line != "" {
					ctx.Logger.Info("  %s", line)
				}
			}
		}
	}
	ctx.Logger.Info("Auto cleanup finished; verify cluster status then retry yinstall standby")
	return nil
}

func runSEFailedCleanup(ctx *runner.StepContext, user, envFile, clusterName string, execute bool) error {
	ctx.Logger.Info("SE path cleanup guidance:")
	ctx.Logger.Info("  yasboot node remove -c %s --clean", clusterName)
	ctx.Logger.Info("  (or: yasboot node remove -c %s -n <standby_node_id> --clean)", clusterName)
	if !execute {
		ctx.Logger.Info("Cleanup NOT executed (print-only). Re-run with --standby-cleanup-on-failure or -F to auto-clean.")
		return nil
	}
	cmd := fmt.Sprintf("yasboot node remove --clean -c %s", clusterName)
	ctx.Logger.Info("Executing: %s", cmd)
	res, err := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, user, envFile, cmd)
	out := ""
	if res != nil {
		out = YasbootCombinedOutput(res.GetStdout(), res.GetStderr())
	}
	if err != nil || (res != nil && res.GetExitCode() != 0) {
		ctx.Logger.Error("SE cleanup failed: %v\n%s", err, out)
		return fmt.Errorf("auto cleanup failed: %v; output: %s", err, strings.TrimSpace(out))
	}
	ctx.Logger.Info("Auto cleanup finished; verify cluster status then retry yinstall standby")
	return nil
}

// loggingRedactPassword 避免把 -p 密码打到终端摘要（粗脱敏）。
func loggingRedactPassword(cmd string) string {
	// ShellSingleQuote 形态： -p '...'
	re := strings.Index(cmd, " -p ")
	if re < 0 {
		return cmd
	}
	rest := cmd[re+4:]
	if strings.HasPrefix(rest, "'") {
		if end := strings.Index(rest[1:], "'"); end >= 0 {
			return cmd[:re+4] + "'***'" + rest[1+end+1:]
		}
	}
	return cmd[:re+4] + "***"
}
