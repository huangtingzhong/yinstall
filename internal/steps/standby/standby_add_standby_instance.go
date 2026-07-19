// standby_add_standby_instance.go - 添加备库实例
// SE：yasboot node add；CE：yasboot group add

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
	dbsteps "github.com/yinstall/internal/steps/db"
)

// stepAddStandbyInstance 添加备库实例步骤
func stepAddStandbyInstance() *runner.Step {
	return &runner.Step{
		Name:        "Add Standby Instance",
		Description: "Create standby via yasboot node add (SE) or group add (CE)",
		Tags:        []string{"standby", "instance"},

		PreCheck: func(ctx *runner.StepContext) error {
			if err := EnsureStandbyCEPath(ctx, ""); err != nil {
				return err
			}
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, err)
			}
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return err
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			// Check cluster_add.toml exists
			clusterAddFile := fmt.Sprintf("%s/%s_add.toml", stageDir, clusterName)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", clusterAddFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, fmt.Errorf("%s_add.toml not found, run Generate Expansion Config first", clusterName))
			}
			if ctx.GetParamBool("standby_ce_path", false) && strings.TrimSpace(ctx.GetParamString("db_admin_password", "")) == "" {
				return fmt.Errorf("--db-admin-password is required for yasboot group add on CE standby path")
			}

			return precheckStandbyTargetsAlreadyAdded(ctx, envFile, clusterName)
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Add Standby Instance")
			if v, ok := ctx.Results["standby_instance_add_skip"].(bool); ok && v && !ctx.IsForceStep() {
				ctx.Logger.Info("All standby targets already present at begin-port; skip node/group add")
				standbyLogPhase(ctx, "expand-skip", "already_added")
				return nil
			}
			if err := EnsureStandbyCEPath(ctx, ""); err != nil {
				return err
			}
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			primaryUser := GetPrimaryOSUser(ctx)

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			ctx.Logger.Info("Using primary environment file: %s", envFile)
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return err
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			clusterAddFile := fmt.Sprintf("%s/%s_add.toml", stageDir, clusterName)

			ctx.Logger.Info("Adding standby database instances")
			ctx.Logger.Info("  Cluster: %s", clusterName)
			ctx.Logger.Info("  Config file: %s", clusterAddFile)
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			useCE := ctx.GetParamBool("standby_ce_path", false)
			var addCmd string
			if useCE {
				sysPass := strings.TrimSpace(ctx.GetParamString("db_admin_password", ""))
				if sysPass == "" {
					return fmt.Errorf("--db-admin-password is required for yasboot group add")
				}
				addCmd = fmt.Sprintf("cd %s && yasboot group add -c %s -t %s -p %s",
					stageDir, clusterName, clusterAddFile, commonos.ShellSingleQuote(sysPass))
				ctx.Logger.Info("Running: yasboot group add ...")
			} else {
				addCmd = fmt.Sprintf("cd %s && yasboot node add -c %s -t %s",
					stageDir, clusterName, clusterAddFile)
				ctx.Logger.Info("Running: yasboot node add ...")
			}

			// Execute as primary user with environment sourced
			standbyLogPhase(ctx, "expand-start", clusterName)
			ctx.Logger.Info("NOTE: This command triggers background data synchronization")
			ctx.Logger.Info("      Command completion does not mean sync is finished")

			result, err := runYasbootOnPrimaryWithEnvFile(ctx, primaryUser, envFile, addCmd)
			if err != nil || (result != nil && result.GetExitCode() != 0) {
				var combined string
				if result != nil {
					combined = YasbootCombinedOutput(result.GetStdout(), result.GetStderr())
				}
				// SE：scale failed 时尝试 node remove --clean 后重试一次（CE 禁止盲清，避免误伤主组）
				if !useCE && result != nil && (strings.Contains(strings.ToLower(combined), "scale failed node") ||
					strings.Contains(strings.ToLower(combined), "node remove --clean")) {
					ctx.Logger.Warn("Failed nodes detected, cleaning up before retrying...")
					cleanupCmd := fmt.Sprintf("yasboot node remove --clean -c %s", clusterName)
					cleanupResult, cleanupErr := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, primaryUser, envFile, cleanupCmd)
					cleanupOut := ""
					if cleanupResult != nil {
						cleanupOut = YasbootCombinedOutput(cleanupResult.GetStdout(), cleanupResult.GetStderr())
					}
					if cleanupErr == nil && cleanupResult != nil &&
						(strings.Contains(strings.ToLower(cleanupOut), "clean") ||
							strings.Contains(strings.ToLower(cleanupOut), "no scalefailed node") ||
							strings.Contains(strings.ToLower(cleanupOut), "environment is clean") ||
							cleanupResult.GetExitCode() == 0) {
						ctx.Logger.Info("Cleanup completed, retrying node add...")
						result, err = runYasbootOnPrimaryWithEnvFile(ctx, primaryUser, envFile, addCmd)
						if err == nil && result != nil && result.GetExitCode() == 0 {
							if result.GetStdout() != "" {
								ctx.Logger.Info("Command output:")
								for _, line := range strings.Split(result.GetStdout(), "\n") {
									if line != "" {
										ctx.Logger.Info("  %s", line)
									}
								}
							}
							ctx.Logger.Info("Standby instance creation command completed")
							ctx.Logger.Info("Data synchronization may still be in progress")
							return nil
						}
						if result != nil {
							combined = YasbootCombinedOutput(result.GetStdout(), result.GetStderr())
						}
					}
				}
				hint := ExplainYasbootNodeAddFailure(combined)
				op := "node add"
				if useCE {
					op = "group add"
					hint = hint + "\n" + FormatCEExpansionFailureRemediation(clusterName, ctx.GetParamBool("standby_cleanup_on_failure", false) || ctx.ForceAll)
				}
				ctx.Logger.Error("yasboot %s failed: %s", op, hint)
				if combined != "" {
					ctx.Logger.Error("--- full yasboot %s output ---\n%s", op, combined)
				}
				if err != nil {
					return fmt.Errorf("failed to add standby instance: %w\n%s", err, hint)
				}
				if result != nil && result.GetExitCode() != 0 {
					return fmt.Errorf("failed to add standby instance: exit code %d: %s\n%s",
						result.GetExitCode(), strings.TrimSpace(YasbootCombinedOutput(result.GetStdout(), result.GetStderr())), hint)
				}
			}

			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Info("Command output:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}
			}

			ctx.Logger.Info("Standby instance creation command completed")
			ctx.Logger.Info("Data synchronization may still be in progress")
			standbyLogPhase(ctx, "expand-done", clusterName)
			return nil
		},
	}
}

// precheckStandbyTargetsAlreadyAdded 只读：目标 IP:begin-port 已在集群则报告；全部已在则标记 skip。
func precheckStandbyTargetsAlreadyAdded(ctx *runner.StepContext, envFile, clusterName string) error {
	targets := ctx.GetParamStringSlice("standby_targets")
	if len(targets) == 0 {
		return nil
	}
	beginPort := ctx.GetParamInt("db_begin_port", 1688)
	primaryUser := GetPrimaryOSUser(ctx)
	clusterRes, _ := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, primaryUser, envFile, fmt.Sprintf("yasboot cluster status -c %s -d", clusterName))
	if clusterRes == nil || clusterRes.GetExitCode() != 0 {
		ctx.Logger.Info("Skip already-added precheck: cluster status unavailable")
		return nil
	}
	statusOut := clusterRes.GetStdout()
	var already []string
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if !ClusterHasIPListenPort(statusOut, target, beginPort) {
			continue
		}
		already = append(already, target)
		role, inst, dbStat := clusterListenRowSummary(statusOut, target, beginPort)
		ctx.ReportPrecheckIssue(runner.PrecheckIssue{
			StepName: "Add Standby Instance",
			Host:     ctx.Executor.Host(),
			Severity: runner.PrecheckSeverityInfo,
			Code:     "PC.STANDBY.ALREADY_IN_CLUSTER",
			Message: fmt.Sprintf("standby target %s already has listen %s:%d (role=%s instance=%s db=%s); node/group add may be unnecessary",
				target, target, beginPort, role, inst, dbStat),
			Remediation: fmt.Sprintf("omit this target or use %s to force re-add after cleanup", ctx.ForceStepsHint()),
		})
	}
	if len(already) == 0 {
		return nil
	}
	// 仅当全部目标都已占端口时标记 skip（部分已存在仍走 Action，由 yasboot 报错或 E-010 清理）
	nonEmpty := 0
	for _, t := range targets {
		if strings.TrimSpace(t) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 0 && len(already) == nonEmpty && !ctx.IsForceStep() {
		if ctx.Results == nil {
			ctx.Results = make(map[string]interface{})
		}
		ctx.Results["standby_instance_add_skip"] = true
		ctx.ReportPrecheckIssue(runner.PrecheckIssue{
			StepName: "Add Standby Instance",
			Host:     ctx.Executor.Host(),
			Severity: runner.PrecheckSeverityInfo,
			Code:     "PC.STANDBY.ADD_SKIP_ALL_EXIST",
			Message:  fmt.Sprintf("all %d standby target(s) already present at begin-port %d; apply will skip node/group add", len(already), beginPort),
		})
		ctx.Logger.Info("All standby targets already in cluster at port %d; apply will skip add", beginPort)
	}
	return nil
}

func clusterListenRowSummary(statusOut, ip string, port int) (role, instanceStatus, dbStatus string) {
	role, instanceStatus, dbStatus = "?", "?", "?"
	for _, r := range dbsteps.ParseClusterStatusTable(statusOut) {
		if !SameHostIP(ListenIPFromAddress(r.ListenAddress), ip) {
			continue
		}
		if ListenPortFromAddress(r.ListenAddress) != port {
			continue
		}
		if r.DatabaseRole != "" {
			role = r.DatabaseRole
		}
		if r.InstanceStatus != "" {
			instanceStatus = r.InstanceStatus
		}
		if r.DatabaseStatus != "" {
			dbStatus = r.DatabaseStatus
		}
		return role, instanceStatus, dbStatus
	}
	return role, instanceStatus, dbStatus
}
