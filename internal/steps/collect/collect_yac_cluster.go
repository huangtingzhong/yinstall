// r030_yac_cluster.go - YAC 集群信息采集（全局步骤，可选）
// 仅当 ctx.TargetHosts > 1 时（YAC 模式）执行；
// 聚合各节点 R-015/R-019 产物并通过 yasboot yac status 获取集群概览，
// 写入 yac/ 目录和 cluster.json。
package collect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepYacCluster 返回 R-030 步骤：采集 YAC 集群信息（Optional）。
// 此步骤由 collect.go 在所有 per-host 步骤完成后单独驱动（后置步骤），
// 执行时 ctx.TargetHosts 已由调用方填充。
func stepYacCluster() *runner.Step {
	return &runner.Step{
		Name:     "Collect YAC cluster info",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			// 仅 YAC 模式（多节点）执行；ctx.TargetHosts 由后置步骤驱动填充
			if len(ctx.TargetHosts) < 2 {
				return fmt.Errorf("single-node deployment, skipping R-030 YAC cluster info")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			rootDir := collectRootDir(ctx)
			yacDir := filepath.Join(rootDir, "yac")
			if err := os.MkdirAll(yacDir, 0o755); err != nil {
				return fmt.Errorf("mkdir yac dir: %w", err)
			}

			osUser := getCollectOSUser(ctx)
			envFile := getCollectEnvFile(ctx)

			// yasboot yac status（在首节点执行）
			if envFile != "" {
				r, _ := collectExecuteAsUserWithEnv(ctx, osUser, envFile, "yasboot yac status 2>/dev/null || true", collectCmdTimeout(ctx))
				if r != nil {
					_ = writeTextFile(filepath.Join(yacDir, "yac-status.txt"), r.GetStdout())
				}
			}
			_ = runAndSave(ctx, "yasboot yac status 2>/dev/null || true", filepath.Join(yacDir, "yac-status-raw.txt"))

			// 聚合各节点网络信息（从各主机归档目录读取 ip-addr.txt）
			nodeInfo := make([]map[string]string, 0, len(ctx.TargetHosts))
			for _, th := range ctx.TargetHosts {
				safeHost := strings.NewReplacer(":", "_").Replace(th.Host)
				ipAddrPath := filepath.Join(rootDir, "hosts", safeHost, "os", "network", "ip-addr.txt")
				content := ""
				if b, err := os.ReadFile(ipAddrPath); err == nil {
					content = string(b)
				}
				nodeInfo = append(nodeInfo, map[string]string{
					"host":    th.Host,
					"ip_addr": content,
				})
			}

			cluster := map[string]interface{}{
				"node_count": len(ctx.TargetHosts),
				"hosts":      buildHostsList(ctx),
				"nodes":      nodeInfo,
			}
			if err := writeJSON(filepath.Join(yacDir, "cluster.json"), cluster); err != nil {
				appendWarning(ctx, err.Error())
			}

			ctx.Logger.Info("[R-030] YAC cluster info collected to %s", yacDir)
			return nil
		},
	}
}
