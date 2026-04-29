// g002_extract_package.go - 解压 YCM 安装包
// G-002: 从本地或远程查找 YCM 安装包，上传（如需）并解压到安装目录

package ycm

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

// StepG002ExtractPackage 解压 YCM 安装包
func StepG002ExtractPackage() *runner.Step {
	return &runner.Step{
		ID:          "G-002",
		Name:        "Extract YCM Package",
		Description: "Extract YCM installation package to install directory",
		Tags:        []string{"ycm", "package"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			pkgPath := ctx.GetParamString("ycm_package", "")
			installDir := strings.TrimSpace(ctx.GetParamString("ycm_install_dir", "/opt"))
			if installDir == "" {
				return fmt.Errorf("ycm_install_dir is required")
			}

			// Directory checks should be visible in precheck (read-only).
			// We do not create directories in PreCheck.
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				res, _ := hctx.Execute(fmt.Sprintf("if [ -e %s ] && [ ! -d %s ]; then echo NOT_DIR; elif [ -d %s ]; then echo IS_DIR; else echo MISSING; fi",
					installDir, installDir, installDir), false)
				kind := ""
				if res != nil {
					kind = strings.TrimSpace(res.GetStdout())
				}
				if strings.Contains(kind, "NOT_DIR") {
					return fmt.Errorf("ycm_install_dir exists but is not a directory on %s: %s", th.Host, installDir)
				}
				if strings.Contains(kind, "MISSING") {
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepID:      "G-002",
						StepName:    "Extract YCM Package",
						Host:        th.Host,
						Severity:    runner.PrecheckSeverityInfo,
						Code:        "PC.YCM.INSTALL_DIR.MISSING",
						Message:     fmt.Sprintf("YCM install directory does not exist: %s; apply will mkdir -p and extract", installDir),
						Remediation: "you may pre-create the directory and set permissions, or let apply create it",
					})
				}
			}

			if pkgPath == "" {
				// 尝试自动查找最新版本的 YCM 软件包
				ctx.Logger.Info("ycm_package not specified, searching for latest yashandb-cloud-manager package...")
				remoteDir := ctx.RemoteSoftwareDir
				if remoteDir == "" {
					remoteDir = "/data/yashan/soft"
				}

				latestPkg, err := file.FindLatestYCMPackage(ctx, ctx.LocalSoftwareDirs, remoteDir)
				if err != nil {
					return fmt.Errorf("ycm_package not specified and auto-search failed: %w", err)
				}

				ctx.Logger.Info("Found latest YCM package: %s", latestPkg)
				// 将找到的包路径设置到参数中，供 Action 使用
				ctx.Params["ycm_package"] = latestPkg
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			pkgPath := ctx.GetParamString("ycm_package", "")
			installDir := ctx.GetParamString("ycm_install_dir", "/opt")
			remoteDir := ctx.RemoteSoftwareDir
			if remoteDir == "" {
				remoteDir = "/data/yashan/soft"
			}

			ctx.Logger.Info("Looking for YCM package: %s", pkgPath)
			ctx.Logger.Info("Install directory: %s", installDir)
			ctx.Logger.Info("Remote software dir: %s", remoteDir)
			ctx.Logger.Info("Local software dirs: %v", ctx.LocalSoftwareDirs)

			// 查找并分发安装包
			fullPath, err := file.FindAndDistribute(
				ctx,
				pkgPath,
				ctx.LocalSoftwareDirs,
				remoteDir,
			)
			if err != nil {
				return fmt.Errorf("YCM package %s not found: %w", pkgPath, err)
			}

			ctx.Logger.Info("Package found at: %s", fullPath)

			// 确保安装目录存在
			ctx.Execute(fmt.Sprintf("mkdir -p %s", installDir), true)

			// 根据文件格式解压
			var cmd string
			if strings.HasSuffix(fullPath, ".tar.gz") || strings.HasSuffix(fullPath, ".tgz") {
				cmd = fmt.Sprintf("tar -zxf %s -C %s", fullPath, installDir)
			} else if strings.HasSuffix(fullPath, ".tar") {
				cmd = fmt.Sprintf("tar -xf %s -C %s", fullPath, installDir)
			} else if strings.HasSuffix(fullPath, ".zip") {
				cmd = fmt.Sprintf("unzip -o %s -d %s", fullPath, installDir)
			} else {
				return fmt.Errorf("unsupported package format: %s (expected .tar.gz, .tgz, .tar, or .zip)", fullPath)
			}

			ctx.Logger.Info("Extracting: %s -> %s", fullPath, installDir)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to extract YCM package: %w", err)
			}

			ctx.Logger.Info("YCM package extracted successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			installDir := ctx.GetParamString("ycm_install_dir", "/opt")
			ycmDir := fmt.Sprintf("%s/ycm", installDir)

			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", ycmDir), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("YCM directory not found at %s after extraction", ycmDir)
			}

			// 检查 ycm-init 是否存在
			ycmInit := fmt.Sprintf("%s/ycm-init", installDir)
			result, _ = ctx.Execute(fmt.Sprintf("test -f %s", ycmInit), false)
			if result == nil || result.GetExitCode() != 0 {
				ctx.Logger.Warn("ycm-init not found at %s, checking in ycm subdir", ycmInit)
			} else {
				ctx.Logger.Info("OK: ycm-init found at %s", ycmInit)
			}

			ctx.Logger.Info("OK: YCM directory exists: %s", ycmDir)
			return nil
		},
	}
}
