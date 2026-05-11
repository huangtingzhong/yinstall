// c007_extract_package.go - 解压数据库安装包
// 本步骤从本地或远程查找安装包，上传（如需）并解压到 stage 目录

package db

import (
	"fmt"
	"path"
	"strings"

	"github.com/yinstall/internal/common/file"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC007ExtractPackage 解压数据库安装包步骤
func StepC007ExtractPackage() *runner.Step {
	return &runner.Step{
		ID:          "C-007",
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

		// C-005 仅在首节点执行（单机/YAC 都只需在首节点解压，yasboot package install 会自动分发到所有节点）
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

			// 解压前校验 stage 目录应为空，避免历史残留导致 yasboot/om 与 package 内容版本错配，
			// 进而在后续 C-014 才以 tar 形式报错（如 “database-xxx not found in archive”）。
			// 若用户显式强制该步（--force-steps C-007 或 -f 全局强制），则清空后再解压。
			stageQ := commonos.ShellSingleQuote(stageDir)
			emptyCheckCmd := fmt.Sprintf(`test -z "$(find %s -mindepth 1 2>/dev/null | head -1)" && echo EMPTY || echo NOT_EMPTY`, stageQ)
			emptyRes, _ := ctx.Execute(emptyCheckCmd, true)
			isEmpty := emptyRes != nil && strings.Contains(strings.TrimSpace(emptyRes.GetStdout()), "EMPTY")
			if !isEmpty {
				if ctx.IsForceStep() {
					ctx.Logger.Warn("Stage directory %s is not empty; force mode enabled, cleaning before extraction", stageDir)
					if !commonos.IsSafeUnixRmRfPath(stageDir) {
						return fmt.Errorf("refusing to clean stage directory %q: path is not under allowed installation roots", stageDir)
					}
					// 仅删除 stage 下顶层项，避免未引号通配或 "rm -rf $dir/*" 在异常路径上扩大范围
					cleanCmd := fmt.Sprintf(`find %s -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true`, stageQ)
					if _, err := ctx.ExecuteWithCheck(cleanCmd, true); err != nil {
						return fmt.Errorf("failed to cleanup stage directory %s before extraction: %w", stageDir, err)
					}
				} else {
					return fmt.Errorf("stage directory %s is not empty; please clean it first or re-run with --force-steps C-007 (or global -F) to auto-clean before extraction", stageDir)
				}
			}

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

			ctx.Execute(fmt.Sprintf("mkdir -p %s", stageQ), true)

			fullQ := commonos.ShellSingleQuote(fullPath)
			var cmd string
			if strings.HasSuffix(fullPath, ".tar.gz") || strings.HasSuffix(fullPath, ".tgz") {
				cmd = fmt.Sprintf("tar -zxf %s -C %s", fullQ, stageQ)
			} else if strings.HasSuffix(fullPath, ".tar") {
				cmd = fmt.Sprintf("tar -xf %s -C %s", fullQ, stageQ)
			} else if strings.HasSuffix(fullPath, ".zip") {
				cmd = fmt.Sprintf("unzip -o %s -d %s", fullQ, stageQ)
			} else {
				return fmt.Errorf("unsupported package format: %s", fullPath)
			}

			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to extract package: %w", err)
			}

			cmd = fmt.Sprintf("chown -R %s:%s %s", user, group, stageQ)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to set ownership: %w", err)
			}

			// 校验解压结果包含 database 负载文件，避免后续 yasboot package se/ce gen 才报 “Not found in archive”
			// 只做最小校验：至少存在一个 database-*.tar.gz
			payloadCmd := fmt.Sprintf(`test -n "$(find %s -maxdepth 2 -type f -name 'database-*.tar.gz' 2>/dev/null | head -1)" && echo OK || echo MISSING`, stageQ)
			payloadRes, _ := ctx.Execute(payloadCmd, true)
			if payloadRes == nil || !strings.Contains(strings.TrimSpace(payloadRes.GetStdout()), "OK") {
				return fmt.Errorf("extracted package in %s does not contain database-*.tar.gz payload; the package may be incomplete or mismatched. Please provide the correct DB package via --db-package", stageDir)
			}

			ctx.Logger.Info("Package extracted successfully on first node")
			if len(ctx.TargetHosts) > 1 {
				ctx.Logger.Info("Note: yasboot package install (C-020) will distribute software to all %d nodes", len(ctx.TargetHosts))
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			yasbootPath := path.Join(stageDir, "bin/yasboot")
			result, _ := ctx.Execute(fmt.Sprintf("test -x %s", commonos.ShellSingleQuote(yasbootPath)), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("yasboot not found at %s", yasbootPath)
			}
			ctx.Logger.Info("Verified: yasboot exists at %s", yasbootPath)
			return nil
		},
	}
}
