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

// stepExtractPackage 解压数据库安装包步骤
func stepExtractPackage() *runner.Step {
	return &runner.Step{
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
				pkgPath = latestPkg
			}
			if err := ensureMultitenantPackageVersionCtx(ctx, ctx.CurrentStepID); err != nil {
				return err
			}
			if err := file.EnsureArchiveExtractTools(ctx, pkgPath); err != nil {
				return err
			}
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			return precheckStageReadyForExtract(ctx, stageDir, pkgPath)
		},

		// C-005 仅在首节点执行（单机/YAC 都只需在首节点解压，yasboot package install 会自动分发到所有节点）
		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-007: Extract Package")
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

			stageQ := commonos.ShellSingleQuote(stageDir)
			// PreCheck 已校验；此处：force 清空，或 stage 已齐则跳过解压
			empty, ready := stageEmptyAndReady(ctx, stageDir)
			if !empty {
				if ctx.IsForceStep() {
					ctx.Logger.Warn("Stage directory %s is not empty; force mode enabled, cleaning before extraction", stageDir)
					if err := commonos.ValidateDeletePath(stageDir); err != nil {
						return fmt.Errorf("refusing to clean stage directory %q: %w", stageDir, err)
					}
					cleanCmd := fmt.Sprintf(`find %s -mindepth 1 -maxdepth 1 -exec rm -rf {} + 2>/dev/null || true`, stageQ)
					if _, err := ctx.ExecuteWithCheck(cleanCmd, true); err != nil {
						return fmt.Errorf("failed to cleanup stage directory %s before extraction: %w", stageDir, err)
					}
				} else if ready && stageVersionMatchesPackage(ctx, stageDir, pkgPath) {
					ctx.Logger.Info("Stage %s already has yasboot + database payload matching package; skip extract", stageDir)
					dbLogPhase(ctx, "extract-skip", "already_extracted")
					return nil
				} else {
					return fmt.Errorf("stage directory %s is not empty; please clean it first or re-run with %s (or global -F) to auto-clean before extraction", stageDir, ctx.ForceStepsHint())
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
			dbLogPhase(ctx, "extract-start", runner.TruncateForLog(fullPath, 80))

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
				dbLogPhase(ctx, "extract-fail", runner.TruncateForLog(err.Error(), 80))
				return fmt.Errorf("failed to extract package: %w", err)
			}
			dbLogPhase(ctx, "extract-done", stageDir)

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

// precheckStageReadyForExtract 只读：stage 空 / 已齐可 skip / force 将清空；否则失败。
func precheckStageReadyForExtract(ctx *runner.StepContext, stageDir, pkgPath string) error {
	empty, ready := stageEmptyAndReady(ctx, stageDir)
	if empty {
		return nil
	}
	if ctx.IsForceStep() {
		ctx.ReportPrecheckIssue(runner.PrecheckIssue{
			StepName:    "Extract Package",
			Host:        ctx.Executor.Host(),
			Severity:    runner.PrecheckSeverityInfo,
			Code:        "PC.DB.STAGE_FORCE_CLEAN",
			Message:     fmt.Sprintf("stage %s is not empty; apply will clean then extract (%s)", stageDir, ctx.ForceStepsHint()),
			Remediation: "Omit force to keep existing stage, or clean manually first.",
		})
		return nil
	}
	if ready && stageVersionMatchesPackage(ctx, stageDir, pkgPath) {
		ctx.ReportPrecheckIssue(runner.PrecheckIssue{
			StepName: "Extract Package",
			Host:     ctx.Executor.Host(),
			Severity: runner.PrecheckSeverityInfo,
			Code:     "PC.DB.STAGE_ALREADY_EXTRACTED",
			Message:  fmt.Sprintf("stage %s already contains yasboot + database payload matching package; apply will skip extract", stageDir),
		})
		return nil
	}
	return fmt.Errorf("stage directory %s is not empty; please clean it first or re-run with %s (or global -F) to auto-clean before extraction", stageDir, ctx.ForceStepsHint())
}

// stageEmptyAndReady 返回 (是否为空, 是否已有 yasboot+database 负载)。
func stageEmptyAndReady(ctx *runner.StepContext, stageDir string) (empty bool, ready bool) {
	stageQ := commonos.ShellSingleQuote(stageDir)
	emptyCheckCmd := fmt.Sprintf(`if [ ! -d %s ]; then echo EMPTY; elif test -z "$(find %s -mindepth 1 2>/dev/null | head -1)"; then echo EMPTY; else echo NOT_EMPTY; fi`, stageQ, stageQ)
	emptyRes, _ := ctx.Execute(emptyCheckCmd, true)
	// 必须精确匹配 EMPTY：NOT_EMPTY 含子串 EMPTY，用 Contains 会误判为空
	empty = emptyRes != nil && strings.TrimSpace(emptyRes.GetStdout()) == "EMPTY"
	if empty {
		return true, false
	}
	yasbootPath := path.Join(stageDir, "bin/yasboot")
	yb, _ := ctx.Execute(fmt.Sprintf("test -x %s && echo OK", commonos.ShellSingleQuote(yasbootPath)), false)
	payloadCmd := fmt.Sprintf(`test -n "$(find %s -maxdepth 2 -type f -name 'database-*.tar.gz' 2>/dev/null | head -1)" && echo OK || echo MISSING`, stageQ)
	payloadRes, _ := ctx.Execute(payloadCmd, true)
	ready = yb != nil && strings.Contains(yb.GetStdout(), "OK") &&
		payloadRes != nil && strings.Contains(payloadRes.GetStdout(), "OK")
	return false, ready
}

// stageVersionMatchesPackage 包文件名中的版本串若能解析，则要求出现在 stage 路径/文件名中；无法解析时仅依赖 ready。
func stageVersionMatchesPackage(ctx *runner.StepContext, stageDir, pkgPath string) bool {
	ver := extractYashanPackageVersion(pkgPath)
	if ver == "" {
		return true
	}
	stageQ := commonos.ShellSingleQuote(stageDir)
	verQ := commonos.ShellSingleQuote(ver)
	cmd := fmt.Sprintf(`find %s -maxdepth 3 -type f 2>/dev/null | grep -F %s | head -1`, stageQ, verQ)
	res, _ := ctx.Execute(cmd, true)
	return res != nil && strings.TrimSpace(res.GetStdout()) != ""
}

func extractYashanPackageVersion(pkgPath string) string {
	base := path.Base(strings.TrimSpace(pkgPath))
	// yashandb-23.5.2.101-linux-aarch64.tar.gz
	const prefix = "yashandb-"
	if !strings.HasPrefix(strings.ToLower(base), prefix) {
		return ""
	}
	rest := base[len(prefix):]
	for i, r := range rest {
		if (r < '0' || r > '9') && r != '.' {
			if i == 0 {
				return ""
			}
			return rest[:i]
		}
	}
	return rest
}
