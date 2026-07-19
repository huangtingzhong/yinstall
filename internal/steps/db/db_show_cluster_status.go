package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepShowClusterStatus 安装流程末尾展示集群状态与安装摘要
func stepShowClusterStatus() *runner.Step {
	return &runner.Step{
		Name:        "Show Cluster Status",
		Description: "Display cluster status and post-install summary",
		Tags:        []string{"db", "status", "display"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-034: Show Cluster Status")
			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)

			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			isYAC := hctx.GetParamBool("yac_mode", false) || len(ctx.HostsToRun()) > 1

			envFile, err := resolveDBEnvFileForSummary(ctx, hctx)
			if err != nil {
				return err
			}

			statusCmd := fmt.Sprintf("yasboot cluster status -c %s -d", clusterName)
			hctx.Logger.Info("Querying cluster status: %s", statusCmd)
			result, err := commonos.ExecuteAsUserWithEnvCheck(hctx, user, envFile, statusCmd, false)
			if err != nil {
				return fmt.Errorf("failed to get cluster status: %w", err)
			}
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("failed to get cluster status")
			}

			clusterStatusOut := result.GetStdout()
			if strings.TrimSpace(clusterStatusOut) == "" {
				hctx.Logger.Warn("Cluster status command returned empty output")
			} else {
				dbLogPhase(hctx, "cluster-status-done", fmt.Sprintf("cluster=%s bytes=%d", clusterName, len(clusterStatusOut)))
				for _, line := range strings.Split(clusterStatusOut, "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						hctx.Logger.Info("cluster-status| %s", line)
					}
				}
			}

			groupStatusOut := ""
			if isYAC {
				groupCmd := fmt.Sprintf("yasboot cluster status -b group -c %s -d", clusterName)
				hctx.Logger.Info("Querying YAC group cluster status: %s", groupCmd)
				groupRes, groupErr := commonos.ExecuteAsUserWithEnv(hctx, user, envFile, groupCmd, false)
				if groupErr != nil {
					hctx.Logger.Warn("YAC group cluster status failed: %v", groupErr)
				} else if groupRes != nil && groupRes.GetExitCode() == 0 {
					groupStatusOut = groupRes.GetStdout()
					dbLogPhase(hctx, "cluster-group-status-done", fmt.Sprintf("cluster=%s bytes=%d", clusterName, len(groupStatusOut)))
					for _, line := range strings.Split(groupStatusOut, "\n") {
						line = strings.TrimSpace(line)
						if line != "" {
							hctx.Logger.Info("cluster-group-status| %s", line)
						}
					}
				} else {
					hctx.Logger.Warn("YAC group cluster status command failed (exit=%v)", groupRes)
				}
			}

			printDBInstallSummary(ctx, hctx, ctx.CurrentStepID, clusterStatusOut, groupStatusOut)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
