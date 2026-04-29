package db

import (
	"fmt"
	"path"

	"github.com/yinstall/internal/runner"
)

// StepC016DisableArchivelog 按需将 yashandb.toml 中 ISARCHIVELOG 改为 false（关闭归档）
func StepC016DisableArchivelog() *runner.Step {
	return &runner.Step{
		ID:          "C-016",
		Name:        "Configure Archive Log Mode",
		Description: "When --db-disable-archivelog is set, set ISARCHIVELOG = false in cluster toml",
		Tags:        []string{"db", "config", "archivelog"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", configPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("cluster config not found at %s (run C-014 first)", configPath))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("db_disable_archivelog", false) {
				ctx.Logger.Info("Archive log: keeping yasboot default (ISARCHIVELOG unchanged; typically enabled)")
				return nil
			}

			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			has, _ := ctx.Execute(fmt.Sprintf("grep -qE '^[[:space:]]*ISARCHIVELOG[[:space:]]*=' %s", configPath), false)
			if has == nil || has.GetExitCode() != 0 {
				return fmt.Errorf("ISARCHIVELOG not found in %s; cannot disable archive log", configPath)
			}

			ctx.Logger.Info("Disabling archive log mode: setting ISARCHIVELOG = false in %s", configPath)
			// 与 CHARACTER_SET 类似：整行替换，兼容 true/false 及空格
			cmd := fmt.Sprintf(`sed -i 's/^[[:space:]]*ISARCHIVELOG[[:space:]]*=.*/ISARCHIVELOG = false/' %s`, configPath)
			if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
				return fmt.Errorf("failed to set ISARCHIVELOG: %w", err)
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("db_disable_archivelog", false) {
				return nil
			}

			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			result, _ := ctx.Execute(fmt.Sprintf("grep -E '^[[:space:]]*ISARCHIVELOG[[:space:]]*=[[:space:]]*false' %s", configPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("ISARCHIVELOG is not false after edit; check %s", configPath)
			}
			ctx.Logger.Info("Verified ISARCHIVELOG = false in cluster config")
			return nil
		},
	}
}
