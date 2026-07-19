package os

import (
	"fmt"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// areRequiredPackagesInstalledForYum 检查写 repo 所需包是否已安装（与 b012 共用逻辑）
func areRequiredPackagesInstalledForYum(ctx *runner.StepContext) bool {
	return areRequiredPackagesInstalled(ctx)
}

// stepWriteYumRepo 写入本地 YUM repo 配置（可选）
func stepWriteYumRepo() *runner.Step {
	return &runner.Step{
		Name:        "Write YUM Repo Config",
		Description: "Configure local YUM source",
		Tags:        []string{"os", "yum"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			if !commonos.IsLocalYumMode(commonos.GetYumMode(ctx)) {
				return fmt.Errorf("yum mode is not local")
			}

			// 检查必需的软件包是否已安装，如果都已安装则跳过
			if areRequiredPackagesInstalledForYum(ctx) {
				return fmt.Errorf("all required packages already installed, skipping YUM repo configuration")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			osLogPhase(ctx, "plan", "B-014: Write YUM Repo Config")
			if err := commonos.EnsureLocalISORepo(ctx); err != nil {
				return fmt.Errorf("failed to ensure local YUM repo: %w", err)
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			repoFile := ctx.GetParamString("os_yum_repo_file", "/etc/yum.repos.d/local.repo")
			result, err := ctx.Execute(fmt.Sprintf("test -f %s", repoFile), false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("repo file not created")
			}
			return nil
		},
	}
}
