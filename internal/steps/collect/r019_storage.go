// r019_storage.go - 存储与磁盘信息采集
// 采集块设备、文件系统、LVM、multipath 等存储信息，写入 os/storage/ 目录。
package collect

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// StepR019StorageLVM 返回 R-019 步骤：采集存储与磁盘信息。
func StepR019StorageLVM() *runner.Step {
	return &runner.Step{
		ID:   "R-019",
		Name: "Collect storage and LVM info",
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "os", "storage")

			cmds := []struct {
				cmd  string
				dest string
				sudo bool
			}{
				// lsblk/df/findmnt 无需 root
				{"lsblk -o NAME,SIZE,TYPE,FSTYPE,MOUNTPOINT,UUID,MODEL 2>/dev/null || lsblk 2>/dev/null || true", filepath.Join(dir, "lsblk.txt"), false},
				{"df -hT 2>/dev/null || df -h 2>/dev/null || true", filepath.Join(dir, "df.txt"), false},
				{"findmnt 2>/dev/null || cat /proc/mounts 2>/dev/null || true", filepath.Join(dir, "mounts.txt"), false},
				{"cat /etc/fstab 2>/dev/null || true", filepath.Join(dir, "fstab.txt"), false},
				// LVM 命令需要 root
				{"pvs 2>/dev/null || true", filepath.Join(dir, "pvs.txt"), true},
				{"vgs 2>/dev/null || true", filepath.Join(dir, "vgs.txt"), true},
				{"lvs 2>/dev/null || true", filepath.Join(dir, "lvs.txt"), true},
				// multipath 需要 root
				{"multipath -ll 2>/dev/null || true", filepath.Join(dir, "multipath.txt"), true},
				// /proc/diskstats 和 lsscsi 无需 root
				{"cat /proc/diskstats 2>/dev/null || true", filepath.Join(dir, "diskstats.txt"), false},
				{"lsscsi 2>/dev/null || true", filepath.Join(dir, "lsscsi.txt"), false},
			}

			collectLogPhase(ctx, "plan", fmt.Sprintf("cmds=%d dir=os/storage", len(cmds)))

			for _, c := range cmds {
				if err := runAndSave(ctx, c.cmd, c.dest, c.sudo); err != nil {
					appendWarning(ctx, "R-019", err.Error())
				}
			}

			ctx.Logger.Info("[R-019] storage info collected to %s", dir)
			return nil
		},
	}
}
