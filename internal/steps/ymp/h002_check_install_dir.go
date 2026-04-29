// h001_check_install_dir.go - 检查 YMP 安装目录
// H-002: 检查安装路径下是否已有文件，如果有则报错退出；如果启用强制模式，则删除目录

package ymp

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepH002CheckInstallDir 检查 YMP 安装目录
func StepH002CheckInstallDir() *runner.Step {
	return &runner.Step{
		ID:          "H-002",
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

			installDir = strings.TrimSuffix(installDir, "/")
			if !strings.HasPrefix(installDir, "/") {
				return fmt.Errorf("install directory must be an absolute path: %s", installDir)
			}

			// Directory existence/content check in precheck (read-only).
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", installDir), false)
			dirExists := result != nil && result.GetExitCode() == 0
			if dirExists {
				checkCmd := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 2>/dev/null | head -1", installDir)
				r, _ := ctx.Execute(checkCmd, false)
				hasContent := r != nil && r.GetExitCode() == 0 && strings.TrimSpace(r.GetStdout()) != ""
				if hasContent {
					if isForce {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepID:      "H-002",
							StepName:    "Check YMP Install Directory",
							Host:        ctx.Executor.Host(),
							Severity:    runner.PrecheckSeverityWarn,
							Code:        "PC.YMP.INSTALL_DIR.FORCE_DELETE",
							Message:     fmt.Sprintf("YMP install directory exists and is not empty: %s; --force-steps H-002 detected; apply will rm -rf and wipe it", installDir),
							Remediation: "ensure the directory does not contain important files; back up first or choose a different install path",
						})
					} else {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepID:      "H-002",
							StepName:    "Check YMP Install Directory",
							Host:        ctx.Executor.Host(),
							Severity:    runner.PrecheckSeverityError,
							Code:        "PC.YMP.INSTALL_DIR.NOT_EMPTY",
							Message:     fmt.Sprintf("YMP install directory exists and is not empty: %s; apply will fail without force", installDir),
							Remediation: "empty the directory or use a different path; or use --force-steps H-002 to delete and recreate (this will wipe the directory)",
						})
						return fmt.Errorf("installation directory %s already exists and contains files; use -f %s to delete and recreate", installDir, ctx.CurrentStepID)
					}
				} else {
					ctx.ReportPrecheckIssue(runner.PrecheckIssue{
						StepID:      "H-002",
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
					StepID:      "H-002",
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
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			isForce := ctx.IsForceStep()

			// 规范化路径，防止模糊匹配（如 /opt/ymp 不会匹配到 /opt/ymp2）
			// 使用绝对路径，并确保路径以 / 结尾时去掉，避免匹配到子目录
			installDir = strings.TrimSuffix(installDir, "/")
			if !strings.HasPrefix(installDir, "/") {
				return fmt.Errorf("install directory must be an absolute path: %s", installDir)
			}

			ctx.Logger.Info("Checking installation directory: %s", installDir)

			// 检查目录是否存在
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", installDir), false)
			dirExists := result != nil && result.GetExitCode() == 0

			if dirExists {
				// 检查目录是否为空
				// 使用精确匹配，只检查指定目录下的内容，不递归检查子目录
				// 使用 find 命令检查目录下是否有文件或子目录（排除 . 和 ..）
				checkCmd := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 2>/dev/null | head -1", installDir)
				result, _ := ctx.Execute(checkCmd, false)
				hasContent := result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != ""

				if hasContent {
					if isForce {
						// 强制模式：删除整个目录
						ctx.Logger.Warn("Force mode: deleting existing directory %s", installDir)
						// 使用绝对路径，防止误删除（如 /opt/ymp 不会删除 /opt/ymp2）
						// 先检查路径是否确实是目录，再删除
						verifyCmd := fmt.Sprintf("test -d %s && test ! -L %s", installDir, installDir)
						verifyResult, _ := ctx.Execute(verifyCmd, false)
						if verifyResult == nil || verifyResult.GetExitCode() != 0 {
							return fmt.Errorf("install directory %s is not a regular directory (may be a symlink), refusing to delete", installDir)
						}

						// 删除目录（使用绝对路径，防止模糊匹配）
						if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("rm -rf %s", installDir), true); err != nil {
							return fmt.Errorf("failed to delete directory %s: %w", installDir, err)
						}
						ctx.Logger.Info("Directory %s deleted successfully", installDir)
					} else {
						// 非强制模式：列出目录内容并报错
						listCmd := fmt.Sprintf("ls -la %s 2>/dev/null | head -10", installDir)
						listResult, _ := ctx.Execute(listCmd, false)
						dirContent := ""
						if listResult != nil {
							dirContent = strings.TrimSpace(listResult.GetStdout())
						}

						errorMsg := fmt.Sprintf("installation directory %s already exists and contains files", installDir)
						if dirContent != "" {
							errorMsg += fmt.Sprintf(":\n%s", dirContent)
						}
						errorMsg += fmt.Sprintf("\nuse -f %s to delete and recreate", ctx.CurrentStepID)

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

			// 验证目录状态：要么不存在，要么存在但为空
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", installDir), false)
			if result != nil && result.GetExitCode() == 0 {
				// 目录存在，检查是否为空
				checkCmd := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 2>/dev/null | head -1", installDir)
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
