package db

import (
	"fmt"
	"path"

	"github.com/yinstall/internal/runner"
)

// StepC007SetNativeType Set USE_NATIVE_TYPE in cluster config
func StepC013SetNativeType() *runner.Step {
	return &runner.Step{
		ID:          "C-013",
		Name:        "Set Native Type",
		Description: "Configure USE_NATIVE_TYPE setting",
		Tags:        []string{"db", "config"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			// Always pass - optional step
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			useNativeType := ctx.GetParamBool("db_use_native_type", true)
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			configPath := path.Join(stageDir, clusterName+".toml")

			var value string
			if useNativeType {
				value = "true"
			} else {
				value = "false"
			}

			ctx.Logger.Info("Setting USE_NATIVE_TYPE to: %s", value)

			// Check if setting exists
			result, _ := ctx.Execute(fmt.Sprintf("grep -q USE_NATIVE_TYPE %s", configPath), false)
			if result != nil && result.GetExitCode() == 0 {
				// Update existing
				cmd := fmt.Sprintf(`sed -i 's/USE_NATIVE_TYPE.*/USE_NATIVE_TYPE = %s/' %s`, value, configPath)
				if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
					return fmt.Errorf("failed to update USE_NATIVE_TYPE: %w", err)
				}
			} else {
				// Add new setting (append to [db] section)
				cmd := fmt.Sprintf(`sed -i '/^\[db\]/a USE_NATIVE_TYPE = %s' %s`, value, configPath)
				ctx.Execute(cmd, false)
			}

			// Verify change
			result, _ = ctx.Execute(fmt.Sprintf("grep USE_NATIVE_TYPE %s", configPath), false)
			if result != nil {
				ctx.Logger.Info("Config updated: %s", result.GetStdout())
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			// Optional verification
			return nil
		},
	}
}
