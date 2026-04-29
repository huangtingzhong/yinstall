package db

import (
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC024SetEnvVars 配置数据库用户相关环境变量
func StepC024SetEnvVars() *runner.Step {
	return &runner.Step{
		ID:          "C-024",
		Name:        "Set Environment Variables",
		Description: "Configure environment variables for DB user",
		Tags:        []string{"db", "env"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				_, err := commonos.GetUserHomeDir(hctx, user)
				if err != nil {
					return err
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				user := hctx.GetParamString("os_user", "yashan")
				clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
				dataPath := hctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
				beginPort := hctx.GetParamInt("db_begin_port", 1688)
				isYACMode := hctx.GetParamBool("yac_mode", false)

				hctx.Logger.Info("Configuring environment variables for user: %s", user)
				cfg := &commonos.EnvConfig{
					User:        user,
					ClusterName: clusterName,
					DataPath:    dataPath,
					BeginPort:   beginPort,
					IsYACMode:   isYACMode,
				}

				result, err := commonos.ConfigureEnvVars(hctx, cfg)
				if err != nil {
					if strings.Contains(err.Error(), "bashrc not found") {
						hctx.Logger.Warn("%s", err.Error())
						continue
					}
					return err
				}

				hctx.Logger.Info("Home directory: %s", result.HomeDir)
				hctx.Logger.Info("Running yasdb processes: %d", result.YasdbCount)
				if cfg.BeginPort == 1688 {
					hctx.Logger.Info("Default port (1688): writing to .bashrc")
				} else {
					hctx.Logger.Info("Non-default port (%d): writing to .port%d", beginPort, beginPort)
				}
				hctx.Logger.Info("Found generated bashrc: %s", result.BashrcPath)
				hctx.Logger.Info("Environment variables configured to: %s", result.TargetEnvFile)

				// 将环境变量文件路径存储到全局 Results 中，供后续步骤使用
				ctx.Results["env_file"] = result.TargetEnvFile
				ctx.Results["yasdb_count"] = result.YasdbCount
				hctx.Logger.Info("Stored env_file in context: %s", result.TargetEnvFile)
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				path, found := commonos.VerifyYasboot(hctx, user)
				if found {
					hctx.Logger.Info("yasboot found at: %s", path)
				} else {
					hctx.Logger.Warn("yasboot not found in PATH after environment setup")
				}
			}
			return nil
		},
	}
}
