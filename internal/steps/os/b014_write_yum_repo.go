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

// StepB014WriteYumRepo 写入本地 YUM repo 配置（可选）
func StepB014WriteYumRepo() *runner.Step {
	return &runner.Step{
		ID:          "B-014",
		Name:        "Write YUM Repo Config",
		Description: "Configure local YUM source",
		Tags:        []string{"os", "yum"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			yumMode := ctx.GetParamString("os_yum_mode", "none")
			if yumMode != "local-iso" {
				return fmt.Errorf("yum mode is not local-iso")
			}

			// 检查必需的软件包是否已安装，如果都已安装则跳过
			if areRequiredPackagesInstalledForYum(ctx) {
				return fmt.Errorf("all required packages already installed, skipping YUM repo configuration")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			repoFile := ctx.GetParamString("os_yum_repo_file", "/etc/yum.repos.d/local.repo")
			mountpoint := ctx.GetParamString("os_iso_mountpoint", "/media")

			var repoContent string
			if commonos.IsRHEL8(ctx.OSInfo) {
				repoContent = fmt.Sprintf(`[local-baseos]
name=DVD for RHEL - BaseOS
baseurl=file://%s/BaseOS
enabled=1
gpgcheck=0

[local-appstream]
name=DVD for RHEL - AppStream
baseurl=file://%s/AppStream
enabled=1
gpgcheck=0
`, mountpoint, mountpoint)
			} else {
				repoContent = fmt.Sprintf(`[local]
name=Enterprise Linux DVD
baseurl=file://%s
gpgcheck=0
enabled=1
`, mountpoint)
			}

			cmd := fmt.Sprintf("cat > %s << 'EOF'\n%sEOF", repoFile, repoContent)
			_, err := ctx.ExecuteWithCheck(cmd, true)
			return err
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
