// r023_db_cluster_status.go - 数据库集群状态采集
//
// 官方推荐（yasboot cluster status）：
//
//	yasboot cluster status -c <cluster> -d          按 host 展示进程/实例/库状态详情
//	yasboot cluster status -b group -c <cluster> -d  按 group 展示
//
// 输出字段含 hostid、nodeid、pid、instance_status、database_status、database_role、listen_address、data_path 等。
//
// 补充 OM 进程守护视图（可选）：
//
//	yasboot monit summary -c <cluster>
//
// 与 C-029 / C-026 安装验证步骤一致，必须带 -c 集群名；勿使用无 -c 的 cluster status。
// SQL 层状态（V$INSTANCE 等）由 R-026 采集。
package collect

import (
	"fmt"
	"path/filepath"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepR023DBClusterStatus 返回 R-023 步骤：采集集群状态。
func StepR023DBClusterStatus() *runner.Step {
	return &runner.Step{
		ID:       "R-023",
		Name:     "Collect DB cluster status",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if getCollectEnvFile(ctx) == "" {
				return fmt.Errorf("env_file not available, skipping R-023")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			osUser := getCollectOSUser(ctx)
			envFile := getCollectEnvFile(ctx)
			dir := filepath.Join(collectHostDir(ctx), "db", "cluster")

			clusterName := getCollectClusterName(ctx)
			if clusterName == "" {
				clusterName = ctx.GetParamString("db_cluster_name", "yashandb")
			}
			cn := commonos.ShellSingleQuote(clusterName)
			timeout := collectCmdTimeout(ctx)

			collectLogPhase(ctx, "plan",
				fmt.Sprintf("cluster=%s yasboot_cmds=3 dir=db/cluster", clusterName))

			cmds := []struct {
				cmd  string
				dest string
			}{
				{
					fmt.Sprintf("yasboot cluster status -c %s -d 2>/dev/null || true", cn),
					filepath.Join(dir, "cluster-status-by-host.txt"),
				},
				{
					fmt.Sprintf("yasboot cluster status -b group -c %s -d 2>/dev/null || true", cn),
					filepath.Join(dir, "cluster-status-by-group.txt"),
				},
				{
					fmt.Sprintf("yasboot monit summary -c %s 2>/dev/null || true", cn),
					filepath.Join(dir, "monit-summary.txt"),
				},
			}

			var warnings []string
			for _, c := range cmds {
				if err := runAndSaveAsUser(ctx, osUser, envFile, c.cmd, c.dest, timeout); err != nil {
					warnings = append(warnings, err.Error())
				}
			}

			// 结构化摘要：便于 manifest 与自动化解析
			summary := map[string]interface{}{
				"cluster_name": clusterName,
				"host":         ctx.Executor.Host(),
				"artifacts": []string{
					"cluster-status-by-host.txt",
					"cluster-status-by-group.txt",
					"monit-summary.txt",
				},
				"yasboot_commands": []string{
					fmt.Sprintf("yasboot cluster status -c %s -d", clusterName),
					fmt.Sprintf("yasboot cluster status -b group -c %s -d", clusterName),
					fmt.Sprintf("yasboot monit summary -c %s", clusterName),
				},
			}
			if len(warnings) > 0 {
				summary["warnings"] = warnings
			}
			if err := writeJSON(filepath.Join(dir, "cluster-status.json"), summary); err != nil {
				appendWarning(ctx, "R-023", err.Error())
			}

			ctx.Logger.Info("[R-023] cluster status collected for cluster=%s in %s", clusterName, dir)
			return nil
		},
	}
}
