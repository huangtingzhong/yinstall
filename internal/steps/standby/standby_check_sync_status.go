// standby_check_sync_status.go - 备库同步状态检查
// 本步骤在主库连续执行归档切换后轮询 yasboot cluster status，确认出现 standby 角色
// 执行 yasql / yasboot 前会先 source 主库环境变量配置文件

package standby

import (
	"fmt"
	"strings"
	"time"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
	dbsteps "github.com/yinstall/internal/steps/db"
)

const (
	defaultStandbySyncTimeoutSec = 120
	standbySyncPollInterval      = 5 * time.Second
)

// stepCheckSyncStatus 备库同步状态检查步骤
func stepCheckSyncStatus() *runner.Step {
	return &runner.Step{
		Name:        "Check Sync Status",
		Description: "On primary, run ALTER SYSTEM ARCHIVE LOG CURRENT five times, then poll until standby role appears (or --standby-sync-timeout)",
		Tags:        []string{"standby", "sync", "status"},

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

			res, _ := ctx.Execute(fmt.Sprintf("test -f %s", commonos.ShellSingleQuote(envFile)), false)
			if res == nil || res.GetExitCode() != 0 {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, fmt.Errorf("primary env file not found: %s", envFile))
			}
			_, err = commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile, fmt.Sprintf("yasboot cluster status -c %s -d", clusterName), true)
			if err != nil {
				return fmt.Errorf("yasboot cluster status check failed (precheck): %w", err)
			}
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Check Sync Status",
				Host:        ctx.Executor.Host(),
				Severity:    runner.PrecheckSeverityInfo,
				Code:        "PC.STANDBY.VERIFY.APPLY_ONLY",
				Message:     "This step performs archive switching and sync checks (post-apply verification). In --precheck it only validates envFile/yasboot availability.",
				Remediation: "Run after expansion completes (or run without --precheck) to perform the real sync validation.",
			})
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "Check Sync Status")
			standbyLogPhase(ctx, "check-start", "archive switch + sync status")
			if err := EnsureStandbyCEPath(ctx, ""); err != nil {
				return err
			}
			primaryUser := GetPrimaryOSUser(ctx)

			ctx.Logger.Info("Checking standby synchronization status")
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
			timeoutSec := ctx.GetParamInt("standby_sync_timeout_sec", defaultStandbySyncTimeoutSec)

			const archiveSwitches = 5
			archiveSQL := "ALTER SYSTEM ARCHIVE LOG CURRENT;"
			ctx.Logger.Info("On primary: running %d x %s to advance redo/archive before sync check", archiveSwitches, strings.TrimSpace(archiveSQL))
			for i := 1; i <= archiveSwitches; i++ {
				_, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, primaryUser, envFile, clusterName, archiveSQL, true)
				if err != nil {
					return fmt.Errorf("primary archive log current (attempt %d/%d) failed: %w", i, archiveSwitches, err)
				}
				ctx.Logger.Info("  Archive log current completed (%d/%d)", i, archiveSwitches)
			}

			statusCmd := fmt.Sprintf("yasboot cluster status -c %s -d", clusterName)
			deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
			attempt := 0
			for {
				attempt++
				result, err := commonos.ExecuteAsUserWithEnvCheckCtx(ctx, primaryUser, envFile, statusCmd, true)
				if err != nil {
					return fmt.Errorf("failed to check cluster status: %w", err)
				}
				out := result.GetStdout()
				ctx.Logger.Info("Cluster status (attempt %d):", attempt)
				for _, line := range strings.Split(out, "\n") {
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}

				hasPrimary := strings.Contains(out, "primary")
				if !hasPrimary {
					return fmt.Errorf("primary database not found in cluster status")
				}

				targets := ctx.GetParamStringSlice("standby_targets")
				if len(targets) == 0 {
					if s := strings.TrimSpace(ctx.GetParamString("standby_targets_str", "")); s != "" {
						targets = SplitCSVParam(s)
					}
				}
				want := len(targets)
				if ctx.GetParamBool("standby_ce_path", false) {
					if n := ctx.GetParamInt("standby_node_count", 0); n > 0 {
						want = n
					}
				}
				if want < 1 {
					want = 1
				}

				matched := 0
				newGroup := ""
				if ctx.GetParamBool("standby_ce_path", false) {
					// CE：按本次 targets IP 统计 standby；并尽力确认新 group 已出现
					matched = countStandbyRolesOnTargets(out, targets)
					newGroup = ctx.GetParamString("ce_new_group_name", "")
					if newGroup == "" {
						newGroup = ctx.GetParamString("ce_expected_new_group", "")
					}
					if newGroup != "" {
						if gRes, gErr := commonos.ExecuteAsUserWithEnvCtx(ctx, primaryUser, envFile,
							fmt.Sprintf("yasboot cluster status -c %s -b group -d", clusterName), true); gErr == nil && gRes != nil {
							_, stbys := ParseCEGroupNamesByRole(gRes.GetStdout())
							foundGroup := false
							for _, g := range stbys {
								if strings.EqualFold(g, newGroup) {
									foundGroup = true
									break
								}
							}
							if matched >= want && !foundGroup {
								ctx.Logger.Info("Target standby IPs ready (%d/%d) but group %s not yet in standby role list; keep waiting", matched, want, newGroup)
								matched = 0 // 强制继续等 group 出现
							}
						}
					}
				} else if len(targets) > 0 {
					// SE：按本次 targets，避免被已有 standby 行数误判成功
					matched = countStandbyRolesOnTargets(out, targets)
				} else {
					matched = countStandbyRoles(out)
				}

				if matched >= want {
					if strings.Contains(out, "open") {
						ctx.Logger.Info("Instance status: OPEN")
					} else if strings.Contains(out, "mounted") {
						ctx.Logger.Info("Instance status: MOUNTED (apply in progress is acceptable)")
					}
					if strings.Contains(out, "normal") {
						ctx.Logger.Info("Database status: NORMAL")
					}
					ctx.Logger.Info("Standby sync OK for this expansion (matched_targets=%d want>=%d group=%s)", matched, want, newGroup)
					ctx.Logger.Info("Sync status check completed")
					standbyLogPhase(ctx, "check-done", "sync status")
					return nil
				}

				if timeoutSec <= 0 {
					ctx.Logger.Warn("Standby targets not fully visible yet (matched=%d want>=%d); standby_sync_timeout_sec=0 keeps soft success", matched, want)
					standbyLogPhase(ctx, "check-done", "sync status soft")
					return nil
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("standby for this expansion not ready within %ds (matched_targets=%d want>=%d group=%s); last status:\n%s",
						timeoutSec, matched, want, newGroup, strings.TrimSpace(out))
				}
				ctx.Logger.Info("Expansion standby matched=%d want>=%d group=%s; waiting %s (timeout %ds)",
					matched, want, newGroup, standbySyncPollInterval, timeoutSec)
				time.Sleep(standbySyncPollInterval)
			}
		},
	}
}

// countStandbyRoles 统计 cluster status 输出中 database_role=standby 的行数。
func countStandbyRoles(statusOut string) int {
	n := 0
	for _, row := range dbsteps.ParseClusterStatusTable(statusOut) {
		if strings.EqualFold(strings.TrimSpace(row.DatabaseRole), "standby") {
			n++
		}
	}
	if n > 0 {
		return n
	}
	// 解析失败时退化为子串计数（避免漏过）
	return strings.Count(strings.ToLower(statusOut), "standby")
}

// countStandbyRolesOnTargets 统计 listen_address 落在本次 standby targets 上的 standby 行数。
func countStandbyRolesOnTargets(statusOut string, targets []string) int {
	ipSet := map[string]struct{}{}
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t != "" {
			ipSet[t] = struct{}{}
		}
	}
	if len(ipSet) == 0 {
		return 0
	}
	n := 0
	for _, row := range dbsteps.ParseClusterStatusTable(statusOut) {
		if !strings.EqualFold(strings.TrimSpace(row.DatabaseRole), "standby") {
			continue
		}
		host := listenHost(row.ListenAddress)
		if host == "" {
			continue
		}
		if _, ok := ipSet[host]; ok {
			n++
		}
	}
	return n
}
