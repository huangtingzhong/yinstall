package db

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC012ConfigAutostartScript Configure autostart script
func StepC026ConfigAutostartScript() *runner.Step {
	return &runner.Step{
		ID:          "C-026",
		Name:        "Configure Autostart Script",
		Description: "Create yashan_monit.sh script for database autostart",
		Tags:        []string{"db", "autostart"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				result, _ := hctx.Execute(fmt.Sprintf("test -f %s", commonos.ScriptPath), false)
				if result != nil && result.GetExitCode() == 0 {
					hctx.Logger.Info("yashan_monit.sh already exists, will be updated")
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				user := hctx.GetParamString("os_user", "yashan")
				clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
				beginPort := hctx.GetParamInt("db_begin_port", 1688)
				isYACMode := hctx.GetParamBool("yac_mode", false)

				hctx.Logger.Info("Creating yashan_monit.sh script")
				hctx.Logger.Info("  YASDB_USER: %s", user)
				hctx.Logger.Info("  Cluster: %s", clusterName)
				hctx.Logger.Info("  Begin Port: %d", beginPort)

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

				hctx.Logger.Info("  Running yasdb processes: %d", yasdbCount)

				cfg := &commonos.AutostartConfig{
					User:        user,
					ClusterName: clusterName,
					BeginPort:   beginPort,
					IsYACMode:   isYACMode,
				}

				if err := commonos.CreateAutostartScript(hctx, cfg); err != nil {
					return err
				}

				hctx.Logger.Info("Created yashan_monit.sh at %s", commonos.ScriptPath)
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				result, _ := hctx.Execute(fmt.Sprintf("test -x %s", commonos.ScriptPath), false)
				if result == nil || result.GetExitCode() != 0 {
					return fmt.Errorf("yashan_monit.sh is not executable on %s", th.Host)
				}
				hctx.Logger.Info("yashan_monit.sh verified: exists and executable")
			}
			return nil
		},
	}
}
