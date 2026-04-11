// c004_extract_package.go - 解压数据库安装包
// 本步骤从本地或远程查找安装包，上传（如需）并解压到 stage 目录

package db

import (
	"fmt"
	"path"
	"strings"

	"github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

// StepC004ExtractPackage 解压数据库安装包步骤
func StepC006ExtractPackage() *runner.Step {
	return &runner.Step{
		ID:          "C-006",
		Name:        "Extract Package",
		Description: "Extract DB installation package to stage directory",
		Tags:        []string{"db", "package"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			pkgPath := ctx.GetParamString("db_package", "")
			if pkgPath == "" {
				// 尝试自动查找最新版本的数据库软件包
				ctx.Logger.Info("db_package not specified, searching for latest yashandb package...")
				remoteDir := ctx.RemoteSoftwareDir
				if remoteDir == "" {
					remoteDir = "/data/yashan/soft"
				}

				latestPkg, err := file.FindLatestDBPackage(ctx, ctx.LocalSoftwareDirs, remoteDir)
				if err != nil {
					return fmt.Errorf("db_package not specified and auto-search failed: %w", err)
				}

				ctx.Logger.Info("Found latest package: %s", latestPkg)
				// 将找到的包路径设置到参数中，供 Action 使用
				ctx.Params["db_package"] = latestPkg
			}
			return nil
		},

		// C-004 仅在首节点执行（单机/YAC 都只需在首节点解压，yasboot package install 会自动分发到所有节点）
		Action: func(ctx *runner.StepContext) error {
			pkgPath := ctx.GetParamString("db_package", "")
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			user := ctx.GetParamString("os_user", "yashan")
			group := ctx.GetParamString("os_group", "yashan")
			remoteDir := ctx.RemoteSoftwareDir
			if remoteDir == "" {
				remoteDir = "/data/yashan/soft"
			}

			// 只在首节点（ctx.Executor）执行解压
			ctx.Logger.Info("Extracting package on first node: %s", ctx.Executor.Host())
			ctx.Logger.Info("Looking for package: %s", pkgPath)
			ctx.Logger.Info("Remote software dir: %s", remoteDir)
			ctx.Logger.Info("Local software dirs: %v", ctx.LocalSoftwareDirs)

			fullPath, err := file.FindAndDistribute(
				ctx,
				pkgPath,
				ctx.LocalSoftwareDirs,
				remoteDir,
			)
			if err != nil {
				return fmt.Errorf("package %s not found: %w", pkgPath, err)
			}

			ctx.Logger.Info("Package found at: %s", fullPath)
			ctx.Logger.Info("Extracting package: %s -> %s", fullPath, stageDir)

			ctx.Execute(fmt.Sprintf("mkdir -p %s", stageDir), true)

			var cmd string
			if strings.HasSuffix(fullPath, ".tar.gz") || strings.HasSuffix(fullPath, ".tgz") {
				cmd = fmt.Sprintf("tar -zxf %s -C %s", fullPath, stageDir)
			} else if strings.HasSuffix(fullPath, ".tar") {
				cmd = fmt.Sprintf("tar -xf %s -C %s", fullPath, stageDir)
			} else if strings.HasSuffix(fullPath, ".zip") {
				cmd = fmt.Sprintf("unzip -o %s -d %s", fullPath, stageDir)
			} else {
				return fmt.Errorf("unsupported package format: %s", fullPath)
			}

			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to extract package: %w", err)
			}

			cmd = fmt.Sprintf("chown -R %s:%s %s", user, group, stageDir)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to set ownership: %w", err)
			}

			ctx.Logger.Info("Package extracted successfully on first node")
			if len(ctx.TargetHosts) > 1 {
				ctx.Logger.Info("Note: yasboot package install (C-008) will distribute software to all %d nodes", len(ctx.TargetHosts))
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			yasbootPath := path.Join(stageDir, "bin/yasboot")
			result, _ := ctx.Execute(fmt.Sprintf("test -x %s", yasbootPath), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("yasboot not found at %s", yasbootPath)
			}
			ctx.Logger.Info("Verified: yasboot exists at %s", yasbootPath)
			return nil
		},
	}
}
