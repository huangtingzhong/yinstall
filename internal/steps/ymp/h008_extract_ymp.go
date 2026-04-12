// h007_extract_ymp.go - 解压 YMP 软件包
// H-008: 解压 YMP zip 到安装目录

package ymp

import (
	"fmt"
	"path"

	commonfile "github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

// StepH008ExtractYMP 解压 YMP 软件包
func StepH008ExtractYMP() *runner.Step {
	return &runner.Step{
		ID:          "H-008",
		Name:        "Extract YMP Package",
		Description: "Extract YMP software package to install directory",
		Tags:        []string{"ymp", "package"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			ympPackage := ctx.GetParamString("ymp_package", "")
			if ympPackage == "" {
				// 尝试自动查找最新版本的 YMP 软件包
				ctx.Logger.Info("ymp_package not specified, searching for latest yashan-migrate-platform package...")
				remoteDir := ctx.RemoteSoftwareDir
				if remoteDir == "" {
					remoteDir = "/data/yashan/soft"
				}
				latestPkg, err := commonfile.FindLatestYMPPackage(ctx, ctx.LocalSoftwareDirs, remoteDir)
				if err != nil {
					return fmt.Errorf("ymp_package not specified and auto-search failed: %w", err)
				}
				ctx.Logger.Info("Found latest YMP package: %s", latestPkg)
				ctx.Params["ymp_package"] = latestPkg
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ympPackage := ctx.GetParamString("ymp_package", "")
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			ympUser := ctx.GetParamString("ymp_user", "ymp")

			ctx.Logger.Info("Looking for YMP package: %s", ympPackage)

			fullPath, err := commonfile.FindAndDistribute(
				ctx,
				ympPackage,
				ctx.LocalSoftwareDirs,
				ctx.RemoteSoftwareDir,
			)
			if err != nil {
				return fmt.Errorf("YMP package not found: %w", err)
			}

			// 确保安装目录存在
			ctx.Execute(fmt.Sprintf("mkdir -p %s", installDir), true)

			// 设置包属主
			ctx.Execute(fmt.Sprintf("chown %s:%s %s", ympUser, ympUser, fullPath), true)

			// 解压
			ctx.Logger.Info("Extracting: %s -> %s", fullPath, installDir)
			cmd := fmt.Sprintf("unzip -o %s -d %s", fullPath, installDir)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to extract YMP package: %w", err)
			}

			// 设置目录属主
			ctx.Execute(fmt.Sprintf("chown -R %s:%s %s", ympUser, ympUser, installDir), true)

			ctx.Logger.Info("YMP package extracted successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			ympSh := path.Join(installDir, "yashan-migrate-platform", "bin", "ymp.sh")

			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", ympSh), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("ymp.sh not found at %s", ympSh)
			}
			ctx.Logger.Info("✓ YMP directory verified: %s", ympSh)
			return nil
		},
	}
}
