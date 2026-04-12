package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepB027EnableMultipathd Enable multipathd (YAC)
func StepB027EnableMultipathd() *runner.Step {
	return &runner.Step{
		ID:          "B-027",
		Name:        "Enable Multipathd",
		Description: "Start and enable multipath service",
		Tags:        []string{"os", "yac", "multipath"},
		Optional:    true, // 单机环境下不需要多路径，可以跳过

		PreCheck: func(ctx *runner.StepContext) error {
			// YAC 模式下需要启用 multipathd（除非磁盘已经是多路径设备）
			isYACMode := ctx.GetParamBool("yac_mode", false)
			if isYACMode {
				hasMultipathDisks := ctx.GetParamBool("yac_has_multipath_disks", false)
				if hasMultipathDisks {
					return fmt.Errorf("multipath disks already configured")
				}
				return nil
			}

			// 非 YAC 模式：检查是否显式启用
			enabled := ctx.GetParamBool("yac_multipath_enable", false)
			needMultipath := ctx.GetParamBool("yac_need_multipath", false)

			if !enabled && !needMultipath {
				return fmt.Errorf("multipath not enabled and not required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ctx.Logger.Info("Flushing stale multipath devices and bindings cache...")
			ctx.Execute("systemctl stop multipathd 2>/dev/null", true)
			// multipath -F 刷新未使用的 multipath 映射，dmsetup remove_all 清除所有残留 dm 设备
			ctx.Execute("multipath -F 2>/dev/null", true)
			ctx.Execute("dmsetup remove_all 2>/dev/null", true)
			for _, path := range []string{
				"/etc/multipath/bindings",
				"/var/lib/multipath/bindings",
			} {
				ctx.Execute(fmt.Sprintf("rm -f %s", path), true)
			}

			ctx.Execute("systemctl enable multipathd", true)
			_, err := ctx.ExecuteWithCheck("systemctl restart multipathd", true)
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("systemctl is-active multipathd", false)
			if strings.TrimSpace(result.GetStdout()) != "active" {
				return fmt.Errorf("multipathd is not active")
			}
			return nil
		},
	}
}
