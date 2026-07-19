// r003_snapshot_install.go - 安装运行快照（可选）
// 若本次 collect 嵌入在安装流程中（ctx.Results["install_params"] 存在），
// 则将安装参数写入 install-run.json，记录安装上下文供对比分析。
// 若不存在则跳过（Optional=true）。
package collect

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// stepSnapshotInstall 返回 R-003 步骤：快照安装运行参数（可选）。
func stepSnapshotInstall() *runner.Step {
	return &runner.Step{
		Name:     "Snapshot install run params",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			// 若无安装参数则跳过
			if _, ok := ctx.Results["install_params"]; !ok {
				return fmt.Errorf("no install_params in context, skipping")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			params := ctx.Results["install_params"]
			dest := filepath.Join(collectHostDir(ctx), "install-run.json")
			if err := writeJSON(dest, params); err != nil {
				appendError(ctx, err.Error())
				return nil // 非致命
			}
			ctx.Logger.Info("[R-003] install run params saved to %s", dest)
			return nil
		},
	}
}
