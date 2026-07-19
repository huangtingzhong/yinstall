// standby_show_cluster_status.go - 显示集群状态与备库扩容摘要
// 本步骤在主库上执行 yasboot cluster status，必要时轮询 mounted standby，并输出 Expansion Summary

package standby

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
	dbsteps "github.com/yinstall/internal/steps/db"
)

// stepShowClusterStatus 显示集群状态与扩容摘要步骤
func stepShowClusterStatus() *runner.Step {
	return &runner.Step{
		Name:        "Show Cluster Status",
		Description: "Display cluster status and post-expansion summary on primary",
		Tags:        []string{"standby", "status", "display"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			primaryUser := GetPrimaryOSUser(ctx)
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return err
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			_, err = commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile, fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), true)
			if err != nil {
				return fmt.Errorf("yasboot cluster status check failed (precheck): %w", err)
			}
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Show Cluster Status",
				Host:        ctx.Executor.Host(),
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.STANDBY.DISPLAY.APPLY_ONLY",
				Message:     "This step displays cluster status and expansion summary during apply; in --precheck it only validates envFile/yasboot availability.",
				Remediation: "Run during apply or after installation (or run without --precheck) to view real status output.",
			})
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Show Cluster Status")
			standbyLogPhase(ctx, "check-start", "yasboot cluster status -d")
			primaryUser := GetPrimaryOSUser(ctx)

			ctx.Logger.Info("Displaying cluster status on primary")
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			ctx.Logger.Info("Using primary environment file: %s", envFile)
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return err
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			ctx.Logger.Info("  Cluster: %s", clusterName)

			statusCmd := fmt.Sprintf("yasboot cluster status -c %s -d", clusterName)
			fetchStatus := func() (string, error) {
				result, err := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile, statusCmd, true)
				if err != nil {
					errMsg := "Failed to get cluster status"
					if result != nil {
						if result.GetStderr() != "" {
							errMsg = result.GetStderr()
						} else if result.GetStdout() != "" {
							errMsg = result.GetStdout()
						}
					}
					return "", fmt.Errorf("%s: %w", errMsg, err)
				}
				if result == nil || result.GetExitCode() != 0 {
					errMsg := "Failed to get cluster status"
					if result != nil {
						if result.GetStderr() != "" {
							errMsg = result.GetStderr()
						} else if result.GetStdout() != "" {
							errMsg = result.GetStdout()
						}
					}
					return "", fmt.Errorf("%s", errMsg)
				}
				return result.GetStdout(), nil
			}

			clusterStatusOut, err := fetchStatus()
			if err != nil {
				ctx.Logger.Warn("Failed to get cluster status: %v", err)
				return err
			}
			if strings.TrimSpace(clusterStatusOut) == "" {
				ctx.Logger.Warn("Cluster status command returned empty output")
			} else {
				logClusterStatusLines(ctx, clusterStatusOut)
			}

			attempts, interval := standbyOpenPollConfig(ctx)
			stepID := ctx.CurrentStepID
			standbyIPs := ctx.GetParamStringSlice("standby_targets")
			if len(standbyIPs) == 0 {
				if s := strings.TrimSpace(ctx.GetParamString("standby_targets_str", "")); s != "" {
					for _, p := range strings.Split(s, ",") {
						if p = strings.TrimSpace(p); p != "" {
							standbyIPs = append(standbyIPs, p)
						}
					}
				}
			}
			initialPending := PendingStandbyOpenNodes(dbsteps.ParseClusterStatusTable(clusterStatusOut), standbyIPs)
			finalOut, stillPending, timedOut, pollErr := PollStandbyOpenUntilReadyForTargets(
				clusterStatusOut,
				standbyIPs,
				fetchStatus,
				attempts,
				nil,
				interval,
				func(attempt, maxAttempts int, pendingNodes []string) {
					msg := fmt.Sprintf("Waiting for standby open: attempt %d/%d (node=%s not open yet)",
						attempt, maxAttempts, strings.Join(pendingNodes, ","))
					ctx.Logger.Info("%s", msg)
					if ctx.Logger != nil {
						ctx.Logger.ConsoleNotice(stepID, msg)
					}
				},
			)
			if pollErr != nil {
				ctx.Logger.Warn("Standby open poll interrupted: %v", pollErr)
			}
			clusterStatusOut = finalOut
			if strings.TrimSpace(clusterStatusOut) != "" && len(initialPending) > 0 {
				logClusterStatusLines(ctx, clusterStatusOut)
			}
			if timedOut && len(stillPending) > 0 {
				msg := "Standby still not fully open after polling; continue with summary (manual check recommended)"
				ctx.Logger.Warn("%s", msg)
				ctx.Logger.ConsoleNotice(stepID, msg)
			} else if len(initialPending) > 0 && len(stillPending) == 0 {
				ctx.Logger.ConsoleNotice(stepID, "Standby instances are open")
			}

			groupStatusOut := ""
			if ctx.GetParamBool("standby_ce_path", false) || ctx.GetParamBool("primary_is_ce", false) {
				if gRes, gErr := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile,
					fmt.Sprintf("yasboot cluster status -c %s -b group -d", clusterName), true); gErr == nil && gRes != nil && gRes.GetExitCode() == 0 {
					groupStatusOut = gRes.GetStdout()
					logClusterStatusLines(ctx, groupStatusOut)
				}
			}
			printStandbyExpansionSummary(ctx, stepID, clusterStatusOut, stillPending)
			printStandbyCEGroupSummary(ctx, stepID, groupStatusOut)
			standbyLogPhase(ctx, "check-done", fmt.Sprintf("cluster=%s", clusterName))
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
