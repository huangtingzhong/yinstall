package db

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// StepC002CreateDataDirs Create DB data/log/software directories
func StepC005CreateDataDirs() *runner.Step {
	return &runner.Step{
		ID:          "C-005",
		Name:        "Create Data Directories",
		Description: "Create DB data, log, and software directories",
		Tags:        []string{"db", "directory"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			installPath := ctx.GetParamString("db_install_path", "")
			dataPath := ctx.GetParamString("db_data_path", "")
			logPath := ctx.GetParamString("db_log_path", "")

			if installPath == "" || dataPath == "" || logPath == "" {
				return fmt.Errorf("db_install_path, db_data_path, db_log_path are required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			// YAC 模式下，force 执行需要在所有节点执行（通过 ctx.HostsToRun() 遍历所有节点）
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				installPath := hctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
				dataPath := hctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
				logPath := hctx.GetParamString("db_log_path", "/data/yashan/log")
				user := hctx.GetParamString("os_user", "yashan")
				group := hctx.GetParamString("os_group", "yashan")
				isForce := hctx.IsForceStep()
				dirs := []string{installPath, dataPath, logPath}

				for _, dir := range dirs {
					// 检查目录是否存在
					result, _ := hctx.Execute(fmt.Sprintf("test -d %s", dir), false)
					dirExists := result != nil && result.GetExitCode() == 0

					if dirExists {
						if isForce {
							// 强制模式：如果目录存在，删除它
							hctx.Logger.Warn("Force mode: deleting existing directory %s on %s", dir, th.Host)
							if _, err := hctx.ExecuteWithCheck(fmt.Sprintf("rm -rf %s", dir), true); err != nil {
								return fmt.Errorf("failed to delete directory %s on %s: %w", dir, th.Host, err)
							}
						} else {
							// 非强制模式：目录已存在，报错
							return fmt.Errorf("directory %s already exists on %s, use --force %s to delete and recreate", dir, th.Host, ctx.CurrentStepID)
						}
					}

					// 创建目录（目录不存在或已被删除）
					hctx.Logger.Info("Creating directory: %s on %s", dir, th.Host)
					cmd := fmt.Sprintf("mkdir -p %s", dir)
					if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
						return fmt.Errorf("failed to create directory %s on %s: %w", dir, th.Host, err)
					}

					// 设置目录属主和属组
					cmd = fmt.Sprintf("chown -R %s:%s %s", user, group, dir)
					if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
						return fmt.Errorf("failed to set ownership on %s: %w", th.Host, err)
					}
					hctx.Logger.Info("Created directory: %s (owner: %s:%s) on %s", dir, user, group, th.Host)
				}
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				installPath := hctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
				dataPath := hctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
				logPath := hctx.GetParamString("db_log_path", "/data/yashan/log")
				dirs := []string{installPath, dataPath, logPath}
				for _, dir := range dirs {
					result, _ := hctx.Execute(fmt.Sprintf("test -d %s", dir), false)
					if result == nil || result.GetExitCode() != 0 {
						return fmt.Errorf("directory %s not found on %s", dir, th.Host)
					}
				}
			}
			return nil
		},
	}
}
