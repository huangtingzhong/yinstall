// h015_cleanup.go - 清理失败安装产物
// H-015: 可选/危险步骤，仅在用户显式指定 --ymp-cleanup 时执行（通常在安装失败后使用）

package ymp

import (
	"fmt"
	"path"

	"github.com/yinstall/internal/runner"
)

// StepH015Cleanup 清理失败安装产物
func StepH015Cleanup() *runner.Step {
	return &runner.Step{
		ID:          "H-015",
		Name:        "Cleanup Failed Install",
		Description: "Remove failed YMP installation artifacts (optional, dangerous, use --ymp-cleanup to enable)",
		Tags:        []string{"ymp", "cleanup"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			// 仅在显式 --ymp-cleanup 时执行
			cleanup := ctx.GetParamBool("ymp_cleanup", false)
			if !cleanup {
				return fmt.Errorf("skip: cleanup not requested (use --ymp-cleanup flag to enable)")
			}

			ctx.Logger.Warn("⚠ YMP cleanup requested - this will remove installation files")
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			ympUser := ctx.GetParamString("ymp_user", "ymp")

			// 先停止 YMP 进程
			ympSh := path.Join(installDir, "yashan-migrate-platform", "bin", "ymp.sh")
			ctx.Logger.Info("Stopping YMP service...")
			ctx.Execute(fmt.Sprintf("su - %s -c 'sh %s stop' 2>/dev/null", ympUser, ympSh), true)

			// 清理安装产物
			cleanupPaths := []string{
				path.Join(installDir, "yashan-migrate-platform", "db", "*"),
				fmt.Sprintf("/home/%s/.yasboot/ymp.env", ympUser),
				path.Join(installDir, "yashan-migrate-platform"),
				fmt.Sprintf("%s/instantclient_*", installDir),
			}

			for _, path := range cleanupPaths {
				ctx.Logger.Info("Removing: %s", path)
				ctx.Execute(fmt.Sprintf("rm -rf %s", path), true)
			}

			ctx.Logger.Info("YMP cleanup completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			ympDir := path.Join(installDir, "yashan-migrate-platform")

			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", ympDir), false)
			if result != nil && result.GetExitCode() == 0 {
				return fmt.Errorf("YMP directory still exists: %s", ympDir)
			}
			ctx.Logger.Info("✓ Cleanup verified: %s removed", ympDir)
			return nil
		},
	}
}
