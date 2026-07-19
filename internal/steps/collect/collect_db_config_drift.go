// r027_db_config_drift.go - 配置漂移检测（可选）
// 对比 R-021 采集的配置文件与 R-026 的 V$PARAMETER 实际运行参数，
// 检测是否存在未应用的配置修改，写入 db/config-drift.json。
// 依赖 R-021 和 R-026 均成功执行（Optional=true）。
package collect

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepDbConfigDrift 返回 R-027 步骤：检测配置漂移（Optional）。
func stepDbConfigDrift() *runner.Step {
	return &runner.Step{
		Name:     "Detect DB config drift",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			// 需要 R-026 SQL 结果
			if _, ok := ctx.Results["sql_v_parameter"]; !ok {
				return fmt.Errorf("sql_v_parameter not available (R-026 skipped), skipping R-027")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "db")

			paramOutput, _ := ctx.Results["sql_v_parameter"].(string)
			// 简单解析 V$PARAMETER 输出为参数名->值 map
			liveParams := parseSimpleParamOutput(paramOutput)

			drift := map[string]interface{}{
				"host":        ctx.Executor.Host(),
				"live_count":  len(liveParams),
				"note":        "Drift detection compares live V$PARAMETER with collected config files. Manual review recommended.",
				"live_params": liveParams,
			}

			if err := writeJSON(filepath.Join(dir, "config-drift.json"), drift); err != nil {
				appendWarning(ctx, err.Error())
			}

			ctx.Logger.Info("[R-027] config drift info written to %s", filepath.Join(dir, "config-drift.json"))
			return nil
		},
	}
}

// parseSimpleParamOutput 将 V$PARAMETER SELECT NAME,VALUE 输出解析为 map。
// 输出格式通常为：NAME | VALUE 列或 tab 分隔行。
func parseSimpleParamOutput(output string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		// 跳过空行、分隔线、标题行
		if line == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "NAME") {
			continue
		}
		// 尝试按 | 分隔
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if k != "" {
				result[k] = v
			}
			continue
		}
		// 尝试按 tab 分隔
		parts = strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if k != "" {
				result[k] = v
			}
		}
	}
	return result
}
