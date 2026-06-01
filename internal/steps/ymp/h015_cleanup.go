// h015_cleanup.go - 清理失败安装产物
// H-015: 可选/危险步骤，仅在用户显式指定 --ymp-cleanup 时执行（通常在安装失败后使用）

package ymp

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
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

			ctx.Logger.Warn("WARNING: YMP cleanup requested - this will remove installation files")
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ympLogPhase(ctx, "plan", "H-015: Cleanup Failed Install")
			installDir := strings.TrimSuffix(strings.TrimSpace(ctx.GetParamString("ymp_install_dir", "/opt/ymp")), "/")
			installDir = path.Clean(strings.ReplaceAll(installDir, `\`, `/`))
			ympUser := ctx.GetParamString("ymp_user", "ymp")

			if !strings.HasPrefix(installDir, "/") {
				return fmt.Errorf("ymp_install_dir must be absolute: %s", installDir)
			}
			if err := commonos.ValidateDeletePath(installDir); err != nil {
				return fmt.Errorf("refusing cleanup: invalid ymp_install_dir %q: %w", installDir, err)
			}

			installQ := commonos.ShellSingleQuote(installDir)
			ympRoot := path.Join(installDir, "yashan-migrate-platform")
			if err := commonos.ValidateDeletePath(ympRoot); err != nil {
				return fmt.Errorf("refusing cleanup: invalid derived path %q: %w", ympRoot, err)
			}
			ympRootQ := commonos.ShellSingleQuote(ympRoot)

			ympSh := path.Join(installDir, "yashan-migrate-platform", "bin", "ymp.sh")
			ctx.Logger.Info("Stopping YMP service...")
			_, _ = commonos.ExecuteAsUser(ctx, ympUser, fmt.Sprintf("sh %s stop 2>/dev/null", commonos.ShellSingleQuote(ympSh)), true)

			// 整块删除平台目录（包含 db 子目录）；禁止向 rm 传入 shell glob（原 db/*、instantclient_* 会扩大删除范围）
			ctx.Logger.Info("Removing YMP platform tree: %s", ympRoot)
			_, _ = ctx.Execute(fmt.Sprintf("rm -rf %s", ympRootQ), true)

			// instantclient_* 目录位于 installDir 下一层：用 find 限定删除范围，不把用户可控 glob 交给 rm
			ctx.Logger.Info("Removing instantclient_* directories under %s (if any)", installDir)
			findRm := fmt.Sprintf(
				`find %s -maxdepth 1 -mindepth 1 -type d -name 'instantclient_*' -print0 2>/dev/null | xargs -0r rm -rf 2>/dev/null || true`,
				installQ,
			)
			_, _ = ctx.Execute(findRm, true)

			ympEnv := fmt.Sprintf("/home/%s/.yasboot/ymp.env", ympUser)
			if err := commonos.ValidateDeletePath(ympEnv); err != nil {
				ctx.Logger.Warn("Skipping ymp.env removal: %v", err)
			} else {
				ctx.Logger.Info("Removing: %s", ympEnv)
				_, _ = ctx.Execute(fmt.Sprintf("rm -f %s", commonos.ShellSingleQuote(ympEnv)), true)
			}

			ctx.Logger.Info("YMP cleanup completed")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			installDir := strings.TrimSuffix(strings.TrimSpace(ctx.GetParamString("ymp_install_dir", "/opt/ymp")), "/")
			installDir = path.Clean(strings.ReplaceAll(installDir, `\`, `/`))
			ympDir := path.Join(installDir, "yashan-migrate-platform")

			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(ympDir)), false)
			if result != nil && result.GetExitCode() == 0 {
				return fmt.Errorf("YMP directory still exists: %s", ympDir)
			}
			ctx.Logger.Info("OK: Cleanup verified: %s removed", ympDir)
			return nil
		},
	}
}
