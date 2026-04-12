// e015_config_env_vars.go - 备库环境变量配置
// 本步骤在备库节点配置环境变量，复用 common/os 的公共函数
// 支持单机模式和 YAC 模式（多实例场景）

package standby

import (
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE015ConfigEnvVars 备库环境变量配置步骤
func StepE015ConfigEnvVars() *runner.Step {
	return &runner.Step{
		ID:          "E-015",
		Name:        "Configure Standby Env Vars",
		Description: "Configure environment variables on standby nodes",
		Tags:        []string{"standby", "env"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")

			// Check user home directory
			_, err := commonos.GetUserHomeDir(ctx, user)
			if err != nil {
				return err
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			dataPath := ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			isYACMode := ctx.GetParamBool("yac_mode", false)

			ctx.Logger.Info("Configuring environment variables for user: %s", user)

			// 使用公共函数配置环境变量
			cfg := &commonos.EnvConfig{
				User:        user,
				ClusterName: clusterName,
				DataPath:    dataPath,
				BeginPort:   beginPort,
				IsYACMode:   isYACMode,
			}

			result, err := commonos.ConfigureEnvVars(ctx, cfg)
			if err != nil {
				// 如果是 bashrc 未找到的警告，只记录不返回错误
				if strings.Contains(err.Error(), "bashrc not found") {
					ctx.Logger.Warn("%s", err.Error())
					return nil
				}
				return err
			}

			ctx.Logger.Info("Home directory: %s", result.HomeDir)
			if beginPort == 1688 {
				ctx.Logger.Info("Default port (1688): writing to .bashrc")
			} else {
				ctx.Logger.Info("Non-default port (%d): writing to .port%d", beginPort, beginPort)
			}
			ctx.Logger.Info("Found generated bashrc: %s", result.BashrcPath)
			ctx.Logger.Info("Environment variables configured to: %s", result.TargetEnvFile)

			// 将环境变量文件路径和 yasdb_count 存储到全局 Results 中，供后续步骤使用
			ctx.Results["env_file"] = result.TargetEnvFile
			ctx.Results["yasdb_count"] = result.YasdbCount
			ctx.Logger.Info("Stored env_file in context: %s", result.TargetEnvFile)

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")

			// 验证 yasboot 是否可用
			path, found := commonos.VerifyYasboot(ctx, user)
			if found {
				ctx.Logger.Info("yasboot found at: %s", path)
			} else {
				ctx.Logger.Warn("yasboot not found in PATH after environment setup")
			}

			return nil
		},
	}
}
