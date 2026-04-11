// e009_config_autostart.go - 备库自启动配置
// 本步骤在备库节点配置自启动脚本和 systemd 服务，复用 common/os 的公共函数
// 支持单机模式和 YAC 模式（YAC 模式需要启动 ycsrootagent）

package standby

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE009ConfigAutostart 备库自启动配置步骤
func StepE013ConfigAutostart() *runner.Step {
	return &runner.Step{
		ID:          "E-013",
		Name:        "Configure Standby Autostart",
		Description: "Configure autostart script and systemd service on standby nodes",
		Tags:        []string{"standby", "autostart"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			// Check if systemd is available
			if !commonos.CheckSystemdAvailable(ctx) {
				return fmt.Errorf("systemctl not found, systemd may not be available")
			}
			// Check if script exists (will be created in Action)
			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", commonos.ScriptPath), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Logger.Info("yashan_monit.sh already exists, will be updated")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			beginPort := ctx.GetParamInt("db_begin_port", 1688)
			isYACMode := ctx.GetParamBool("yac_mode", false)

			ctx.Logger.Info("Configuring autostart for standby")
			ctx.Logger.Info("  YASDB_USER: %s", user)
			ctx.Logger.Info("  Cluster: %s", clusterName)
			ctx.Logger.Info("  Begin Port: %d", beginPort)

			// 获取 yasdb 进程数（优先从 Results 获取，如果没有则重新检测）
			yasdbCount := 0
			if yasdbCountVal, ok := ctx.Results["yasdb_count"]; ok {
				if count, ok := yasdbCountVal.(int); ok {
					yasdbCount = count
				}
			}

			// 如果没有存储的进程数，重新检测
			if yasdbCount == 0 {
				yasdbCount = commonos.GetYasdbProcessCount(ctx)
			}

			ctx.Logger.Info("  Running yasdb processes: %d", yasdbCount)

			// 步骤 1: 创建自启动脚本
			cfg := &commonos.AutostartConfig{
				User:        user,
				ClusterName: clusterName,
				BeginPort:   beginPort,
				IsYACMode:   isYACMode,
			}

			if err := commonos.CreateAutostartScript(ctx, cfg); err != nil {
				return err
			}

			ctx.Logger.Info("Created yashan_monit.sh at %s", commonos.ScriptPath)

			// 步骤 2: 创建并启动 systemd 服务
			result, err := commonos.CreateAutostartService(ctx, cfg)
			if err != nil {
				return err
			}

			ctx.Logger.Info("Running yasdb processes: %d", result.YasdbCount)
			if result.YasdbCount <= 1 {
				ctx.Logger.Info("Single instance mode: service=%s, arg=%s", result.ServiceName, result.ServiceArg)
			} else {
				ctx.Logger.Info("Multiple instances mode: service=%s, arg=%s", result.ServiceName, result.ServiceArg)
			}
			ctx.Logger.Info("Autostart service configured: %s", result.ServiceName)

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			beginPort := ctx.GetParamInt("db_begin_port", 1688)

			// 验证脚本是否可执行
			result, _ := ctx.Execute(fmt.Sprintf("test -x %s", commonos.ScriptPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("yashan_monit.sh is not executable")
			}
			ctx.Logger.Info("yashan_monit.sh verified: exists and executable")

			// 获取 yasdb 进程数
			yasdbCount := commonos.GetYasdbProcessCount(ctx)
			serviceName, _ := commonos.DetermineServiceName(yasdbCount, beginPort)

			// 验证服务状态
			if commonos.VerifyAutostartService(ctx, serviceName) {
				ctx.Logger.Info("Service %s is enabled for autostart", serviceName)
			} else {
				ctx.Logger.Warn("Service %s may not be enabled", serviceName)
			}

			// 检查服务状态
			result, _ = ctx.Execute(fmt.Sprintf("systemctl is-active %s 2>/dev/null", serviceName), false)
			if result != nil {
				ctx.Logger.Info("Service %s status: %s", serviceName, result.GetStdout())
			}

			return nil
		},
	}
}
