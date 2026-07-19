// r018_packages.go - 已安装软件包采集（可选）
// 根据系统包管理器（rpm/dnf/apt）采集已安装包列表，写入 os/packages/ 目录。
package collect

import (
	"fmt"
	"path/filepath"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepPackages 返回 R-018 步骤：采集已安装软件包（Optional）。
func stepPackages() *runner.Step {
	return &runner.Step{
		Name:     "Collect installed packages",
		Optional: true,
		Action: func(ctx *runner.StepContext) error {
			dir := filepath.Join(collectHostDir(ctx), "os", "packages")

			// 通过 OSInfo 或实时探测确定包管理器
			pkgManager := ""
			if ctx.OSInfo != nil {
				pkgManager = ctx.OSInfo.PkgManager
			}
			if pkgManager == "" {
				pkgManager = commonos.GetPkgManager(ctx.OSInfo)
			}

			collectLogPhase(ctx, "plan", fmt.Sprintf("pkg_manager=%s dir=os/packages", pkgManager))

			switch pkgManager {
			case "apt":
				_ = runAndSave(ctx, "dpkg -l 2>/dev/null || true", filepath.Join(dir, "dpkg-list.txt"))
				_ = runAndSave(ctx, "apt list --installed 2>/dev/null || true", filepath.Join(dir, "apt-installed.txt"))
			default:
				// rpm（yum/dnf 均使用 rpm 数据库）
				_ = runAndSave(ctx, "rpm -qa --queryformat '%{NAME} %{VERSION}-%{RELEASE} %{ARCH}\n' 2>/dev/null | sort || true", filepath.Join(dir, "rpm-qa.txt"))
				_ = runAndSave(ctx, "yum repolist 2>/dev/null || dnf repolist 2>/dev/null || true", filepath.Join(dir, "repolist.txt"))
				_ = runAndSave(ctx, "yum history 2>/dev/null | head -30 || dnf history 2>/dev/null | head -30 || true", filepath.Join(dir, "yum-history.txt"))
			}

			// YashanDB 相关包
			_ = runAndSave(ctx, "rpm -qa 2>/dev/null | grep -iE 'yashan|yasdb|zstd|libzstd' || dpkg -l 2>/dev/null | grep -iE 'yashan|yasdb|zstd' || true", filepath.Join(dir, "yashan-related.txt"))

			ctx.Logger.Info("[R-018] package list collected to %s (pkg_manager=%s)", dir, pkgManager)
			return nil
		},
	}
}
