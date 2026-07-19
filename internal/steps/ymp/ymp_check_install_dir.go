// h001_check_install_dir.go - 检查 YMP 安装目录
// H-002: 检查安装路径下是否已有文件，如果有则报错退出；如果启用强制模式，则删除目录

package ymp

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepCheckInstallDir 检查 YMP 安装目录
func stepCheckInstallDir() *runner.Step {
	return &runner.Step{
		Name:        "Check YMP Install Directory",
		Description: "Verify YMP installation directory is empty or can be cleaned",
		Tags:        []string{"ymp", "precheck", "directory"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			installDir := strings.TrimSpace(ctx.GetParamString("ymp_install_dir", "/opt/ymp"))
			if installDir == "" {
				return fmt.Errorf("ymp_install_dir is required")
			}
			isForce := ctx.IsForceStep()
			allowNonEmpty := isForce || ctx.IsForceStepID(StepIDByName("Run YMP Install"))

			installDir = strings.TrimSuffix(installDir, "/")
			if !strings.HasPrefix(installDir, "/") {
				return fmt.Errorf("install directory must be an absolute path: %s", installDir)
			}

			installQ := commonos.ShellSingleQuote(installDir)
			// Directory existence/content check in precheck (read-only).
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", installQ), false)
			dirExists := result != nil && result.GetExitCode() == 0
			if dirExists {
				checkCmd := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 2>/dev/null | head -1", installQ)
				r, _ := ctx.Execute(checkCmd, false)
				hasContent := r != nil && r.GetExitCode() == 0 && strings.TrimSpace(r.GetStdout()) != ""
				if hasContent {
					if isForce {
						if foreign, err := commonos.ForeignYmpInstallDirEntries(ctx, installDir); err != nil {
							return err
						} else if len(foreign) > 0 {
							names := make([]string, 0, len(foreign))
							for _, p := range foreign {
								names = append(names, path.Base(p))
							}
							msg := fmt.Sprintf("YMP install directory %s contains non-YMP entries: %s; full wipe blocked", installDir, strings.Join(names, ", "))
							ctx.ReportPrecheckIssue(runner.PrecheckIssue{
								StepName:    "Check YMP Install Directory",
								Host:        ctx.Executor.Host(),
								Severity:    runner.PrecheckSeverityError,
								Code:        "PC.YMP.INSTALL_DIR.FOREIGN_ENTRIES",
								Message:     msg,
								Remediation: "move or remove non-YMP files/dirs under the install path, or use a different --ymp-install-dir",
							})
							return fmt.Errorf("%s", msg)
						}
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Check YMP Install Directory",
							Host:        ctx.Executor.Host(),
							Severity:    runner.PrecheckSeverityWarn,
							Code:        "PC.YMP.INSTALL_DIR.FORCE_DELETE",
							Message:     fmt.Sprintf("YMP install directory exists and is not empty: %s; %s detected; apply will rm -rf and wipe it", installDir, ctx.ForceStepsHint()),
							Remediation: "ensure the directory does not contain important files; back up first or choose a different install path",
						})
					} else if allowNonEmpty {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Check YMP Install Directory",
							Host:        ctx.Executor.Host(),
							Severity:    runner.PrecheckSeverityInfo,
							Code:        "PC.YMP.INSTALL_DIR.REINSTALL_IN_PLACE",
							Message:     fmt.Sprintf("YMP install directory is not empty: %s; %s detected; apply will keep it for in-place reinstall", installDir, StepIDByName("Run YMP Install")),
							Remediation: fmt.Sprintf("use -f %s instead if you need to wipe the directory and reinstall from scratch", ctx.CurrentStepID),
						})
					} else {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Check YMP Install Directory",
							Host:        ctx.Executor.Host(),
							Severity:    runner.PrecheckSeverityError,
							Code:        "PC.YMP.INSTALL_DIR.NOT_EMPTY",
							Message:     fmt.Sprintf("YMP install directory exists and is not empty: %s; apply will fail without force", installDir),
							Remediation: fmt.Sprintf("empty the directory or use a different path; or use %s to delete and recreate (this will wipe the directory)", ctx.ForceStepsHint()),
						})
						return fmt.Errorf("installation directory %s already exists and contains files; use %s to wipe and reinstall, or -f %s for in-place reinstall", installDir, ctx.ForceStepsHint(), StepIDByName("Run YMP Install"))
					}
				} else {
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepName:    "Check YMP Install Directory",
						Host:        ctx.Executor.Host(),
						Severity:    runner.PrecheckSeverityInfo,
						Code:        "PC.YMP.INSTALL_DIR.EMPTY",
						Message:     fmt.Sprintf("YMP install directory exists and is empty: %s", installDir),
						Remediation: "",
					})
				}
			} else {
				ctx.ReportPrecheckIssue(runner.PrecheckIssue{
					StepName:    "Check YMP Install Directory",
					Host:        ctx.Executor.Host(),
					Severity:    runner.PrecheckSeverityInfo,
					Code:        "PC.YMP.INSTALL_DIR.MISSING",
					Message:     fmt.Sprintf("YMP install directory does not exist: %s; apply will create/extract", installDir),
					Remediation: "",
				})
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			ympLogPhase(ctx, "plan", "H-002: Check YMP Install Directory")
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			isForce := ctx.IsForceStep()
			allowNonEmpty := isForce || ctx.IsForceStepID(StepIDByName("Run YMP Install"))

			// 规范化路径，防止模糊匹配（如 /opt/ymp 不会匹配到 /opt/ymp2）
			// 使用绝对路径，并确保路径以 / 结尾时去掉，避免匹配到子目录
			installDir = strings.TrimSuffix(installDir, "/")
			if !strings.HasPrefix(installDir, "/") {
				return fmt.Errorf("install directory must be an absolute path: %s", installDir)
			}

			installQ := commonos.ShellSingleQuote(installDir)

			ctx.Logger.Info("Checking installation directory: %s", installDir)

			// 检查目录是否存在
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", installQ), false)
			dirExists := result != nil && result.GetExitCode() == 0

			if dirExists {
				// 检查目录是否为空
				// 使用精确匹配，只检查指定目录下的内容，不递归检查子目录
				// 使用 find 命令检查目录下是否有文件或子目录（排除 . 和 ..）
				checkCmd := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 2>/dev/null | head -1", installQ)
				result, _ := ctx.Execute(checkCmd, false)
				hasContent := result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != ""

				if hasContent {
					if isForce {
						// 强制模式：删除整个目录（仅当顶层无非 YMP 条目）
						if err := commonos.RefuseYmpInstallDirFullWipeIfForeign(ctx, installDir); err != nil {
							return err
						}
						ctx.Logger.Warn("Force mode: deleting existing directory %s", installDir)
						if err := commonos.ValidateDeletePath(installDir); err != nil {
							return fmt.Errorf("refusing to delete install directory %s: %w", installDir, err)
						}
						// 使用绝对路径，防止误删除（如 /opt/ymp 不会删除 /opt/ymp2）
						// 先检查路径是否确实是目录，再删除
						verifyCmd := fmt.Sprintf("test -d %s && test ! -L %s", installQ, installQ)
						verifyResult, _ := ctx.Execute(verifyCmd, false)
						if verifyResult == nil || verifyResult.GetExitCode() != 0 {
							return fmt.Errorf("install directory %s is not a regular directory (may be a symlink), refusing to delete", installDir)
						}

						// 删除目录（使用绝对路径，防止模糊匹配）
						if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("rm -rf %s", installQ), true); err != nil {
							return fmt.Errorf("failed to delete directory %s: %w", installDir, err)
						}
						ctx.Logger.Info("Directory %s deleted successfully", installDir)
						ympUser := ctx.GetParamString("ymp_user", "ymp")
						if err := commonos.RemoveYmpYasbootArtifactsUnderInstallDir(ctx, ympUser, installDir); err != nil {
							return err
						}
					} else if allowNonEmpty {
						ctx.Logger.Info("Keeping existing directory %s for in-place YMP reinstall (-f H-011)", installDir)
					} else {
						// 非强制模式：列出目录内容并报错
						listCmd := fmt.Sprintf("ls -la %s 2>/dev/null | head -10", installQ)
						listResult, _ := ctx.Execute(listCmd, false)
						dirContent := ""
						if listResult != nil {
							dirContent = strings.TrimSpace(listResult.GetStdout())
						}

						errorMsg := fmt.Sprintf("installation directory %s already exists and contains files", installDir)
						if dirContent != "" {
							errorMsg += fmt.Sprintf(":\n%s", dirContent)
						}
						errorMsg += "; use -f H-002 to wipe and reinstall, or -f H-011 for in-place reinstall"

						return fmt.Errorf("%s", errorMsg)
					}
				} else {
					// 目录存在但为空，可以继续
					ctx.Logger.Info("Directory %s exists but is empty, continuing", installDir)
				}
			} else {
				// 目录不存在，可以继续
				ctx.Logger.Info("Directory %s does not exist, will be created", installDir)
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			installDir = strings.TrimSuffix(installDir, "/")

			if !ctx.IsForceStep() && ctx.IsForceStepID(StepIDByName("Run YMP Install")) {
				ctx.Logger.Info("OK: Installation directory %s retained for in-place reinstall", installDir)
				return nil
			}

			// 验证目录状态：要么不存在，要么存在但为空
			installQ := commonos.ShellSingleQuote(installDir)
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", installQ), false)
			if result != nil && result.GetExitCode() == 0 {
				// 目录存在，检查是否为空
				checkCmd := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 2>/dev/null | head -1", installQ)
				result, _ := ctx.Execute(checkCmd, false)
				if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
					return fmt.Errorf("directory %s still contains files after cleanup", installDir)
				}
			}

			ctx.Logger.Info("OK: Installation directory %s is ready", installDir)
			return nil
		},
	}
}
