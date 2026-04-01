package db

import (
	"fmt"
	"path/filepath"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// fmtUnitSuffix 检查字符串是否已经有单位后缀 (M, G, T, K等)
func fmtUnitSuffix(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	suffixes := []string{"M", "G", "T", "K", "B"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

// StepC012AConfigureRedo Configure REDO file parameters in yashandb.toml
func StepC012AConfigureRedo() *runner.Step {
	return &runner.Step{
		ID:          "C-012A",
		Name:        "Configure REDO Parameters",
		Description: "Configure REDO_FILE_NUM and REDO_FILE_SIZE in cluster configuration file",
		Tags:        []string{"db", "config", "redo"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := filepath.Join(stageDir, clusterName+".toml")

			// 获取用户指定的参数
			redoFileNum := ctx.GetParamInt("db_redo_file_num", 0)
			redoFileSize := ctx.GetParamString("db_redo_file_size", "")

			// 如果 redoFileSize 没有单位后缀，自动添加 "M" (MB)
			if redoFileSize != "" && !fmtUnitSuffix(redoFileSize) {
				redoFileSize = redoFileSize + "M"
				ctx.Logger.Info("Added 'M' suffix to redo file size: %s", redoFileSize)
			}

			// 如果参数未指定，根据内存大小自动设置
			if redoFileNum == 0 || redoFileSize == "" {
				memGB, err := commonos.GetTotalMemoryGB(ctx)
				if err != nil {
					ctx.Logger.Warn("Failed to get memory size: %v, using default values", err)
					if redoFileNum == 0 {
						redoFileNum = 4
					}
					if redoFileSize == "" {
						redoFileSize = "128M"
					}
				} else {
					ctx.Logger.Info("Detected system memory: %d GB", memGB)
					if memGB > 128 {
						if redoFileNum == 0 {
							redoFileNum = 6
						}
						if redoFileSize == "" {
							redoFileSize = "4G"
						}
						ctx.Logger.Info("Memory > 128GB, using enhanced REDO settings")
					} else {
						if redoFileNum == 0 {
							redoFileNum = 4
						}
						if redoFileSize == "" {
							redoFileSize = "128M"
						}
						ctx.Logger.Info("Memory <= 128GB, using standard REDO settings")
					}
				}
			}

			ctx.Logger.Info("Configuring REDO parameters:")
			ctx.Logger.Info("  REDO_FILE_NUM: %d", redoFileNum)
			ctx.Logger.Info("  REDO_FILE_SIZE: %s", redoFileSize)

			// 修改配置文件
			cmds := []string{
				fmt.Sprintf(`sed -i 's/REDO_FILE_NUM.*/REDO_FILE_NUM = %d/' %s`, redoFileNum, configPath),
				fmt.Sprintf(`sed -i 's/REDO_FILE_SIZE.*/REDO_FILE_SIZE = "%s"/' %s`, redoFileSize, configPath),
			}

			for _, cmd := range cmds {
				if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
					return fmt.Errorf("failed to configure REDO parameters: %w", err)
				}
			}

			ctx.Logger.Info("REDO parameters configured successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := filepath.Join(stageDir, clusterName+".toml")

			// 验证配置是否生效
			result, _ := ctx.Execute(fmt.Sprintf("grep 'REDO_FILE_NUM' %s", configPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("REDO_FILE_NUM not found in config file")
			}

			result, _ = ctx.Execute(fmt.Sprintf("grep 'REDO_FILE_SIZE' %s", configPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("REDO_FILE_SIZE not found in config file")
			}

			ctx.Logger.Info("Verified REDO parameters in config file")
			return nil
		},
	}
}
