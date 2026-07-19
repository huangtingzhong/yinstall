package mysql

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepPlatformDetect detects target platform and resolves product UID/GID on Linux.
func stepPlatformDetect() *runner.Step {
	return &runner.Step{
		Name:        "Platform Detect",
		Description: "Detect target platform (linux/darwin/windows) and adopt product UID/GID",
		Tags:        []string{"mysql", "platform", "mysql-both"},
		PreCheck: func(ctx *runner.StepContext) error {
			platform := DetectTargetPlatform(ctx)
			StoreTargetPlatform(ctx, platform)
			ctx.Logger.Info("M-001: target_platform=%s host=%s", platform, ctx.Executor.Host())

			if platform == PlatformLinux || platform == PlatformDarwin {
				user := ctx.GetParamString("os_user", "mysql")
				group := ctx.GetParamString("os_group", "mysql")
				uid, gid, err := commonos.ResolveProductUserIDs(ctx, user, group,
					ctx.GetParamInt("os_user_uid", 701),
					ctx.GetParamInt("os_group_gid", 701))
				if err != nil {
					return err
				}
				ctx.Params["os_user_uid"] = uid
				ctx.Params["os_group_gid"] = gid
				ctx.SetResult("os_resolved_uid", uid)
				ctx.SetResult("os_resolved_gid", gid)
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mysqlLogPhase(ctx, "plan", fmt.Sprintf("M-001 platform=%s", ctx.GetTargetPlatform()))
			return nil
		},
	}
}
