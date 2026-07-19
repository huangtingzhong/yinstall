// r002_init_archive.go - 归档目录初始化
// 在控制端创建本次采集的主机子目录结构，并写入初始 meta.json 骨架。
package collect

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yinstall/internal/runner"
)

// stepInitArchive 返回 R-002 步骤：初始化归档目录。
// 在控制端创建 <output_dir>/hosts/<host>/ 目录树，并写入 meta.json 骨架，
// 供后续步骤将采集结果写入对应子目录。
func stepInitArchive() *runner.Step {
	return &runner.Step{
		Name: "Init archive directory",
		Action: func(ctx *runner.StepContext) error {
			hostDir := collectHostDir(ctx)

			// 创建目录结构
			for _, sub := range []string{"os/identity", "os/network", "os/kernel", "os/storage", "os/packages", "db/config", "db/sql", "db/logs"} {
				if err := os.MkdirAll(filepath.Join(hostDir, sub), 0o755); err != nil {
					return fmt.Errorf("mkdir %s: %w", sub, err)
				}
			}

			// 写入初始 meta.json 骨架（英文字段）
			meta := map[string]interface{}{
				"host":         ctx.Executor.Host(),
				"collected_at": time.Now().UTC().Format(time.RFC3339),
				"os_family":    osFamilyString(ctx.OSInfo),
				"arch":         archString(ctx),
				"role":         "primary",
				"status":       "in_progress",
			}
			metaPath := filepath.Join(hostDir, "meta.json")
			if err := writeJSON(metaPath, meta); err != nil {
				return fmt.Errorf("write meta.json: %w", err)
			}

			ctx.Logger.Info("[R-002] archive dir initialized at %s", hostDir)
			return nil
		},
	}
}

// archString 返回目标主机的 CPU 架构字符串。
func archString(ctx *runner.StepContext) string {
	r, _ := collectExecute(ctx, "uname -m", false, collectCmdTimeout(ctx))
	if r != nil && r.GetExitCode() == 0 {
		return r.GetStdout()
	}
	return "unknown"
}
