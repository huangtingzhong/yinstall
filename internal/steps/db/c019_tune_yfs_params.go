package db

import (
	"fmt"
	"path"

	"github.com/yinstall/internal/runner"
)

// StepC019TuneYFSParams 为 YAC 集群调优 YFS 参数
func StepC019TuneYFSParams() *runner.Step {
	return &runner.Step{
		ID:          "C-019",
		Name:        "Tune YFS Parameters",
		Description: "Configure YFS tuning parameters for YAC cluster",
		Tags:        []string{"db", "yac", "config", "tuning"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if !isYACMode {
				return fmt.Errorf("not in YAC mode, skipping")
			}

			yfsEnable := ctx.GetParamBool("yac_yfs_tune_enable", false)
			if !yfsEnable {
				return fmt.Errorf("YFS tuning not enabled, skipping")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			auSize := ctx.GetParamString("yac_yfs_au_size", "32M")
			redoFileSize := ctx.GetParamString("yac_redo_file_size", "128")
			redoFileNum := ctx.GetParamInt("yac_redo_file_num", 6)
			shmPoolSize := ctx.GetParamString("yac_shm_pool_size", "2G")
			maxInstances := ctx.GetParamInt("yac_max_instances", 64)

			// 如果 redoFileSize 没有单位后缀，自动添加 "M" (MB)
			if redoFileSize != "" && !fmtUnitSuffix(redoFileSize) {
				redoFileSize = redoFileSize + "M"
				ctx.Logger.Info("Added 'M' suffix to redo file size: %s", redoFileSize)
			}

			ctx.Logger.Info("Tuning YFS parameters...")
			ctx.Logger.Info("  au_size: %s", auSize)
			ctx.Logger.Info("  REDO_FILE_SIZE: %s", redoFileSize)
			ctx.Logger.Info("  REDO_FILE_NUM: %d", redoFileNum)
			ctx.Logger.Info("  SHM_POOL_SIZE: %s", shmPoolSize)
			ctx.Logger.Info("  MAXINSTANCES: %d", maxInstances)

			// 应用调优参数
			cmds := []string{
				fmt.Sprintf(`sed -i 's/au_size.*/au_size = "%s"/' %s`, auSize, configPath),
				fmt.Sprintf(`sed -i 's/REDO_FILE_SIZE.*/REDO_FILE_SIZE = "%s"/' %s`, redoFileSize, configPath),
				fmt.Sprintf(`sed -i 's/REDO_FILE_NUM.*/REDO_FILE_NUM = %d/' %s`, redoFileNum, configPath),
				fmt.Sprintf(`sed -i 's/SHM_POOL_SIZE.*/SHM_POOL_SIZE = "%s"/' %s`, shmPoolSize, configPath),
			}

			for _, cmd := range cmds {
				if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
					ctx.Logger.Warn("Failed to apply tuning: %v", err)
				}
			}

			// 若不存在则追加 MAXINSTANCES
			result, _ := ctx.Execute(fmt.Sprintf("grep -q MAXINSTANCES %s", configPath), false)
			if result == nil || result.GetExitCode() != 0 {
				cmd := fmt.Sprintf(`sed -i '/^\[db\]/a MAXINSTANCES = %d' %s`, maxInstances, configPath)
				ctx.Execute(cmd, false)
			} else {
				cmd := fmt.Sprintf(`sed -i 's/MAXINSTANCES.*/MAXINSTANCES = %d/' %s`, maxInstances, configPath)
				ctx.Execute(cmd, false)
			}

			ctx.Logger.Info("YFS parameters tuned successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
