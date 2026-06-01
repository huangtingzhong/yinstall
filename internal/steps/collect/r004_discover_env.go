// r004_discover_env.go - 数据库环境发现
// 自动发现目标主机上的 YashanDB 环境变量文件（env file）和集群名，
// 将结果写入 ctx.Results 供后续 DB 采集步骤使用，并持久化到 db/env-discovery.json。
// 复用 standby.GetPrimaryEnvFile / ClusterNameFromEnvFileContent 的发现逻辑，
// 通过在 ctx.Params 中注入 collect 专用键完成适配（无逻辑复制）。
package collect

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yinstall/internal/runner"
	standbysteps "github.com/yinstall/internal/steps/standby"
)

// StepR004DiscoverEnv 返回 R-004 步骤：自动发现 YashanDB env 文件与集群名。
// 发现结果写入 ctx.Results:
//   - env_file:     env 文件的绝对路径
//   - cluster_name: 集群名
//   - begin_port:   数据库端口（若可从 env 文件推断）
func StepR004DiscoverEnv() *runner.Step {
	return &runner.Step{
		ID:       "R-004",
		Name:     "Discover DB environment",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			// 明确指定了 env_file 则直接通过
			if ctx.GetParamString("env_file", "") != "" {
				return nil
			}
			// 需要 os_user 才能定位家目录
			if ctx.GetParamString("os_user", "") == "" {
				return fmt.Errorf("os_user not set, cannot discover env file")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			// 构造 standby.GetPrimaryEnvFile 所需的 param 键映射
			// standby 使用 primary_os_user / primary_env_file / db_cluster_name / db_begin_port
			adaptedParams := buildAdaptedParams(ctx)
			adaptedCtx := &runner.StepContext{
				Executor:      ctx.Executor,
				Logger:        ctx.Logger,
				Params:        adaptedParams,
				Results:       ctx.Results,
				OSInfo:        ctx.OSInfo,
				CurrentStepID: ctx.CurrentStepID,
			}

			envFile, err := standbysteps.GetPrimaryEnvFile(adaptedCtx)
			if err != nil {
				// env 发现失败：非致命，DB 步骤将被跳过
				appendWarning(ctx, "R-004", fmt.Sprintf("env file not found: %v", err))
				discovery := map[string]interface{}{
					"status": "not_found",
					"error":  err.Error(),
				}
				dest := filepath.Join(collectHostDir(ctx), "db", "env-discovery.json")
				_ = writeJSON(dest, discovery)
				return nil
			}

			// 读取 env 文件内容以解析集群名
			readResult, _ := collectExecute(ctx, fmt.Sprintf("cat %s", envFile), false, collectCmdTimeout(ctx))
			clusterName := ctx.GetParamString("cluster_name", "")
			if clusterName == "" && readResult != nil && readResult.GetExitCode() == 0 {
				cn, _ := standbysteps.ClusterNameFromEnvFileContent(readResult.GetStdout())
				clusterName = strings.TrimSpace(cn)
			}
			if clusterName == "" {
				clusterName = ctx.GetParamString("db_cluster_name", "yashandb")
			}

			// 写入 ctx.Results 供下游步骤使用
			ctx.Results["env_file"] = envFile
			ctx.Results["cluster_name"] = clusterName

			discovery := map[string]interface{}{
				"status":       "found",
				"env_file":     envFile,
				"cluster_name": clusterName,
				"host":         ctx.Executor.Host(),
			}
			dest := filepath.Join(collectHostDir(ctx), "db", "env-discovery.json")
			if err := writeJSON(dest, discovery); err != nil {
				appendWarning(ctx, "R-004", fmt.Sprintf("write env-discovery.json: %v", err))
			}

			ctx.Logger.Info("[R-004] env file discovered: %s (cluster: %s)", envFile, clusterName)
			return nil
		},
	}
}

// buildAdaptedParams 将 collect 的 ctx.Params 映射为 standby.GetPrimaryEnvFile 期望的键名。
func buildAdaptedParams(ctx *runner.StepContext) map[string]interface{} {
	adapted := make(map[string]interface{}, len(ctx.Params))
	for k, v := range ctx.Params {
		adapted[k] = v
	}
	// collect 使用 os_user；standby 使用 primary_os_user
	adapted["primary_os_user"] = ctx.GetParamString("os_user", "yashan")
	// collect 使用 env_file；standby 使用 primary_env_file
	if ef := ctx.GetParamString("env_file", ""); ef != "" {
		adapted["primary_env_file"] = ef
	}
	// cluster_name -> db_cluster_name
	if cn := ctx.GetParamString("cluster_name", ""); cn != "" {
		adapted["db_cluster_name"] = cn
	}
	return adapted
}
