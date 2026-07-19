package mysql

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yinstall/internal/common/file"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepInstallVcredist installs VC++ on Windows when missing.
func stepInstallVcredist() *runner.Step {
	return &runner.Step{
		Name:        "Install VC++ Redistributable",
		Description: "Install VC_redist.x64.exe on Windows when not present",
		Tags:        []string{"mysql", "windows", "vcredist", "mysql-software"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetTargetPlatform() != PlatformWindows {
				return fmt.Errorf("not a windows target")
			}
			if commonos.IsVCRedistInstalled(ctx) {
				ctx.Logger.Info("M-007: VC++ already installed, skip")
				return fmt.Errorf("vcredist already installed")
			}
			pkg := ctx.GetParamString("mysql_vc_redist_package", "")
			if pkg == "" {
				remoteDir := ctx.RemoteSoftwareDir
				found, err := file.FindVCRedistPackage(ctx, ctx.LocalSoftwareDirs, remoteDir)
				if err != nil {
					return err
				}
				pkg = found
				ctx.Params["mysql_vc_redist_package"] = pkg
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mysqlLogPhase(ctx, "plan", "M-007 install VC++")
			pkg := ctx.GetParamString("mysql_vc_redist_package", "")
			remoteExe, err := distributeVCRedistToTarget(ctx, pkg)
			if err != nil {
				return err
			}
			exeQ := commonos.PowerShellSingleQuote(remoteExe)
			cmd := fmt.Sprintf(`powershell -NoProfile -Command "$c=Start-Process -FilePath %s -ArgumentList '/install','/quiet','/norestart' -Wait -PassThru; exit $c.ExitCode"`, exeQ)
			res, err := ctx.Execute(cmd, false)
			if err != nil {
				return err
			}
			code := res.GetExitCode()
			if code != 0 && code != 1638 {
				return fmt.Errorf("VC++ install exit code %d stderr=%s stdout=%s", code, res.GetStderr(), res.GetStdout())
			}
			if !commonos.IsVCRedistInstalled(ctx) {
				return fmt.Errorf("VC++ still not detected after install")
			}
			return nil
		},
	}
}

// distributeVCRedistToTarget uploads VC_redist when pkg is a control-plane or relative path.
func distributeVCRedistToTarget(ctx *runner.StepContext, pkg string) (string, error) {
	if strings.TrimSpace(pkg) == "" {
		return "", fmt.Errorf("mysql_vc_redist_package not set")
	}
	if ctx.GetTargetPlatform() == PlatformWindows {
		if len(pkg) > 1 && pkg[1] == ':' {
			return filepath.ToSlash(pkg), nil
		}
		uploaded, err := file.FindAndDistribute(ctx, pkg, ctx.LocalSoftwareDirs, ctx.RemoteSoftwareDir)
		if err != nil {
			return "", err
		}
		return filepath.ToSlash(uploaded), nil
	}
	if strings.HasPrefix(pkg, "/") {
		return filepath.ToSlash(pkg), nil
	}
	uploaded, err := file.FindAndDistribute(ctx, pkg, ctx.LocalSoftwareDirs, ctx.RemoteSoftwareDir)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(uploaded), nil
}
