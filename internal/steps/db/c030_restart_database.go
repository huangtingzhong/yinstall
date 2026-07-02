package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC030RestartDatabase restarts the cluster after post-install SQL/SPFILE changes so parameters take effect.
// YAC: yasboot cluster restart on the first node restarts all cluster nodes.
func StepC030RestartDatabase() *runner.Step {
	return &runner.Step{
		ID:          "C-030",
		Name:        "Restart Database",
		Description: "Restart YashanDB cluster to apply SPFILE and post-install configuration changes",
		Tags:        []string{"db", "restart", "yac"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			yasbootPath := path.Join(stageDir, "bin", "yasboot")
			result, err := ctx.Execute(fmt.Sprintf("test -f %s", yasbootPath), false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("yasboot not found at %s, database may not be deployed yet", yasbootPath))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)
			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile := resolveDBEnvFile(ctx, hctx)
			isYAC := hctx.GetParamBool("yac_mode", false) || len(ctx.TargetHosts) > 1

			if isYAC {
				dbLogPhase(ctx, "plan", fmt.Sprintf("C-030: yasboot cluster restart (YAC, all nodes) cluster=%s", clusterName))
				hctx.Logger.Info("YAC mode: restarting cluster %s from first node (all nodes will restart)", clusterName)
			} else {
				dbLogPhase(ctx, "plan", fmt.Sprintf("C-030: yasboot cluster restart cluster=%s", clusterName))
			}

			restartCmd := BuildClusterRestartCommand(clusterName, "")
			hctx.Logger.Info("Restarting database cluster to apply configuration changes...")
			dbLogPhase(hctx, "restart-start", fmt.Sprintf("cluster=%s yac=%v", clusterName, isYAC))
			if _, err := commonos.ExecuteAsUserWithEnvCheck(hctx, user, envFile, restartCmd, true); err != nil {
				dbLogPhase(hctx, "restart-fail", runner.TruncateForLog(err.Error(), 120))
				return fmt.Errorf("cluster restart failed: %w", err)
			}
			dbLogPhase(hctx, "restart-done", clusterName)
			hctx.Logger.Info("Cluster restart completed")

			if err := openPDBTargetsIfNeeded(hctx, user, envFile, clusterName); err != nil {
				return err
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)
			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile := resolveDBEnvFile(ctx, hctx)
			isYAC := hctx.GetParamBool("yac_mode", false) || len(ctx.TargetHosts) > 1

			statusCmd := fmt.Sprintf("yasboot cluster status -c %s -d", clusterName)
			result, err := commonos.ExecuteAsUserWithEnvCheck(hctx, user, envFile, statusCmd, false)
			if err != nil {
				return fmt.Errorf("cluster status check after restart failed: %w", err)
			}
			out := ""
			if result != nil {
				out = result.GetStdout()
			}
			outLower := strings.ToLower(out)
			if !strings.Contains(outLower, "open") {
				return fmt.Errorf("cluster not open after restart; status output: %s", strings.TrimSpace(out))
			}
			if !isYAC && !strings.Contains(outLower, "normal") {
				hctx.Logger.Warn("pdb_status/database_status may not be normal after restart")
			}
			hctx.Logger.Info("Cluster status after restart: OK")
			return nil
		},
	}
}
