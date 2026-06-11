package mysql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/common/file"
	commonmysql "github.com/yinstall/internal/common/mysql"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepM005CreateDirs creates MYSQL_HOME/DATA/OTHER layout.
func StepM005CreateDirs() *runner.Step {
	return &runner.Step{
		ID:          "M-005",
		Name:        "Create Directories",
		Description: "Create mysql directory layout",
		Tags:        []string{"mysql", "dirs", "mysql-both"},
		PreCheck: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, err)
			}
			for _, d := range []string{layout.Home, layout.Data, layout.Other} {
				if !file.DirExists(ctx, d) && !ctx.Precheck && !ctx.DryRun {
					continue
				}
			}
			_ = layout
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return err
			}
			mysqlLogPhase(ctx, "plan", "M-005 create dirs")
			useSudo := UseSudo(ctx)
			for _, d := range dirsForInstallStage(installStage(ctx), layout) {
				if strings.TrimSpace(d) == "" {
					continue
				}
				if err := file.EnsureDir(ctx, d, useSudo); err != nil {
					return err
				}
			}
			if installStage(ctx) == commonmysql.StageInstance && ctx.GetTargetPlatform() == PlatformLinux {
				user := ctx.GetParamString("os_user", "mysql")
				group := ctx.GetParamString("os_group", "mysql")
				for _, d := range []string{layout.Data, layout.Other} {
					if strings.TrimSpace(d) == "" {
						continue
					}
					cmd := fmt.Sprintf("chown -R %s:%s %s", user, group, commonos.ShellSingleQuote(d))
					if _, err := ctx.ExecuteWithCheck(cmd, useSudo); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}
