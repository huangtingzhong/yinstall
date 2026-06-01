// r011_dmidecode.go - DMI/硬件信息采集（可选）
// 若目标主机存在 dmidecode 命令则采集完整 DMI 信息，否则跳过。
package collect

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// StepR011CollectDMI 返回 R-011 步骤：采集 DMI 硬件信息（Optional）。
func StepR011CollectDMI() *runner.Step {
	return &runner.Step{
		ID:       "R-011",
		Name:     "Collect DMI hardware info",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			// dmidecode 通常需要 root 或 sudo；先检查命令是否存在
			r, _ := collectExecute(ctx, "command -v dmidecode", false, collectCmdTimeout(ctx))
			if r == nil || r.GetExitCode() != 0 {
				return fmt.Errorf("dmidecode not found, skipping")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "os", "dmidecode")

			collectLogPhase(ctx, "plan", "cmds=6 dir=os/dmidecode (sudo dmidecode)")

			cmds := []struct {
				cmd  string
				dest string
			}{
				{"dmidecode 2>/dev/null || true", filepath.Join(dir, "full.txt")},
				{"dmidecode -s system-manufacturer 2>/dev/null || true", filepath.Join(dir, "manufacturer.txt")},
				{"dmidecode -s system-product-name 2>/dev/null || true", filepath.Join(dir, "product.txt")},
				{"dmidecode -s system-serial-number 2>/dev/null || true", filepath.Join(dir, "serial.txt")},
				{"dmidecode -t memory 2>/dev/null || true", filepath.Join(dir, "memory.txt")},
				{"dmidecode -t processor 2>/dev/null || true", filepath.Join(dir, "cpu.txt")},
			}

			// dmidecode 需要 root 权限；非 root 用户通过 sudo -n 执行
			for _, c := range cmds {
				if err := runAndSave(ctx, c.cmd, c.dest, true); err != nil {
					appendWarning(ctx, "R-011", err.Error())
				}
			}

			ctx.Logger.Info("[R-011] DMI info collected to %s", dir)
			return nil
		},
	}
}
