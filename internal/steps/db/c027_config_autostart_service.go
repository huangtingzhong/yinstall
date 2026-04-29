package db

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC027ConfigAutostartService 配置 systemd 服务以实现数据库开机自启
func StepC027ConfigAutostartService() *runner.Step {
	return &runner.Step{
		ID:          "C-027",
		Name:        "Configure Autostart Service",
		Description: "Create and enable systemd service for database autostart",
		Tags:        []string{"db", "autostart", "systemd"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				if !commonos.CheckSystemdAvailable(hctx) {
					return fmt.Errorf("systemctl not found on %s, systemd may not be available", th.Host)
				}
				result, _ := hctx.Execute(fmt.Sprintf("test -x %s", commonos.ScriptPath), false)
				if result == nil || result.GetExitCode() != 0 {
					return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, fmt.Errorf("yashan_monit.sh not found or not executable on %s, run C-026 first", th.Host))
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				beginPort := hctx.GetParamInt("db_begin_port", 1688)
				clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
				isYACMode := hctx.GetParamBool("yac_mode", false)

				// 获取 yasdb 进程数
				yasdbCount := 0
				if yasdbCountVal, ok := ctx.Results["yasdb_count"]; ok {
					if count, ok := yasdbCountVal.(int); ok {
						yasdbCount = count
					}
				}

				// 如果没有存储的进程数，重新检测
				if yasdbCount == 0 {
					yasdbCount = commonos.GetYasdbProcessCount(hctx)
				}

				cfg := &commonos.AutostartConfig{
					ClusterName: clusterName,
					BeginPort:   beginPort,
					IsYACMode:   isYACMode,
				}

				result, err := commonos.CreateAutostartService(hctx, cfg)
				if err != nil {
					return err
				}

				hctx.Logger.Info("Running yasdb processes: %d", result.YasdbCount)
				if result.YasdbCount <= 1 {
					hctx.Logger.Info("Single instance mode: service=%s, arg=%s", result.ServiceName, result.ServiceArg)
				} else {
					hctx.Logger.Info("Multiple instances mode: service=%s, arg=%s", result.ServiceName, result.ServiceArg)
				}
				hctx.Logger.Info("Autostart service configured: %s", result.ServiceName)
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				beginPort := hctx.GetParamInt("db_begin_port", 1688)
				yasdbCount := commonos.GetYasdbProcessCount(hctx)
				serviceName, _ := commonos.DetermineServiceName(yasdbCount, beginPort)

				if commonos.VerifyAutostartService(hctx, serviceName) {
					hctx.Logger.Info("Service %s is enabled for autostart", serviceName)
				} else {
					hctx.Logger.Warn("Service %s may not be enabled", serviceName)
				}

				result, _ := hctx.Execute(fmt.Sprintf("systemctl is-active %s 2>/dev/null", serviceName), false)
				if result != nil {
					hctx.Logger.Info("Service %s status: %s", serviceName, result.GetStdout())
				}
			}
			return nil
		},
	}
}
