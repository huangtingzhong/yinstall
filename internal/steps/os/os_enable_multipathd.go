package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepEnableMultipathd 启用 multipathd 服务（YAC）
func stepEnableMultipathd() *runner.Step {
	return &runner.Step{
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
			osLogPhase(ctx, "plan", "cmds=6 op=flush-multipath+enable-multipathd")
			if !ctx.IsForceStep() && multipathdAlreadyReady(ctx) {
				ctx.Logger.Info("multipathd already active, skipping flush/restart (use -f %s to force)", ctx.CurrentStepID)
				osLogPhase(ctx, "skip", "already_configured=multipathd_active")
				return nil
			}

			ctx.Logger.Info("Flushing stale multipath devices and bindings cache...")
			osLogPhase(ctx, "op-start", "flush-stale-multipath")
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

			osLogPhase(ctx, "op-done", "flush-stale-multipath")
			ctx.Execute("systemctl enable multipathd", true)
			_, err := ctx.ExecuteWithCheck("systemctl restart multipathd", true)
			return err
		},

		PostCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("systemctl is-active multipathd", false)
			if result == nil || strings.TrimSpace(result.GetStdout()) != "active" {
				return fmt.Errorf("multipathd is not active")
			}
			return nil
		},
	}
}

// multipathdAlreadyReady 判定 multipathd 已运行且 multipath 命令可用（映射细节由 B-028 校验）。
func multipathdAlreadyReady(ctx *runner.StepContext) bool {
	active, _ := ctx.Execute("systemctl is-active multipathd 2>/dev/null", false)
	if active == nil || strings.TrimSpace(active.GetStdout()) != "active" {
		return false
	}
	ll, _ := ctx.Execute("multipath -ll >/dev/null 2>&1; echo $?", false)
	if ll == nil {
		return false
	}
	return strings.TrimSpace(ll.GetStdout()) == "0"
}
