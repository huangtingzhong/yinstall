// s002_init_archive_dir.go - 归档目录初始化
// 在控制端创建本次压测的主机子目录结构，并写入 meta.json（含 S-01 主机/OS 身份字段）。
package stressos

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// StepS02InitArchiveDir 返回 S-02 步骤：初始化归档目录。
func StepS02InitArchiveDir() *runner.Step {
	return &runner.Step{
		ID:   "S-02",
		Name: "Init stress archive directory",
		Action: func(ctx *runner.StepContext) error {
			hostDir := stressHostDir(ctx)

			// 压测目录 + os/identity（与 collect R-010 summary 对齐）
			dirs := []string{
				"deps", "cpu", "mem", "io", "net", "runtime", filepath.Join("runtime", "bg"),
				"os/identity",
			}
			for _, sub := range dirs {
				if err := os.MkdirAll(filepath.Join(hostDir, sub), 0o755); err != nil {
					return fmt.Errorf("mkdir %s: %w", sub, err)
				}
			}

			identity := stressHostIdentity(ctx)
			meta := stressBuildMeta(ctx, dirs)
			metaPath := filepath.Join(hostDir, "meta.json")
			if err := writeJSON(metaPath, meta); err != nil {
				return fmt.Errorf("write meta.json: %w", err)
			}
			summaryPath := filepath.Join(hostDir, "os", "identity", "summary.json")
			if err := writeJSON(summaryPath, identity); err != nil {
				return fmt.Errorf("write os/identity/summary.json: %w", err)
			}

			ctx.Logger.Info("[S-02] stress archive dir initialized at %s (hostname=%s os=%s %s)",
				hostDir, identity["hostname"], identity["os_name"], identity["os_version"])
			return nil
		},
	}
}
