package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC005CreateDataDirs 创建数据库数据、日志与软件目录
func StepC005CreateDataDirs() *runner.Step {
	return &runner.Step{
		ID:          "C-005",
		Name:        "Create Data Directories",
		Description: "Create DB data, log, and software directories",
		Tags:        []string{"db", "directory"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			installPath := strings.TrimSpace(ctx.GetParamString("db_install_path", ""))
			dataPath := strings.TrimSpace(ctx.GetParamString("db_data_path", ""))
			logPath := strings.TrimSpace(ctx.GetParamString("db_log_path", ""))

			if installPath == "" || dataPath == "" || logPath == "" {
				return fmt.Errorf("db_install_path, db_data_path, db_log_path are required")
			}

			user := ctx.GetParamString("os_user", "yashan")
			group := ctx.GetParamString("os_group", "yashan")

			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				isForce := hctx.IsForceStep()
				dirs := []string{installPath, dataPath, logPath}

				for _, dir := range dirs {
					dirQ := commonos.ShellSingleQuote(dir)
					// 路径已存在且非目录则失败
					res, _ := hctx.Execute(fmt.Sprintf("if [ -e %s ] && [ ! -d %s ]; then echo NOT_DIR; elif [ -d %s ]; then echo IS_DIR; else echo MISSING; fi",
						dirQ, dirQ, dirQ), false)
					kind := ""
					if res != nil {
						kind = strings.TrimSpace(res.GetStdout())
					}
					if strings.Contains(kind, "NOT_DIR") {
						return fmt.Errorf("path exists but is not a directory on %s: %s", th.Host, dir)
					}
					if strings.Contains(kind, "IS_DIR") {
						if isForce {
							ctx.ReportPrecheckIssue(runner.PrecheckIssue{
								StepID:      "C-005",
								StepName:    "Create Data Directories",
								Host:        th.Host,
								Severity:    runner.PrecheckSeverityWarn,
								Code:        "PC.DB.DIR.FORCE_DELETE",
								Message:     fmt.Sprintf("directory already exists: %s; --force-steps C-005 detected; apply will rm -rf and recreate (owner will be set to %s:%s)", dir, user, group),
								Remediation: "ensure the directory does not contain important data; back up first or choose a different path",
							})
						} else {
							// Single English message for structured issue, terminal precheck line, and PreCheck error (must stay ASCII only).
							dirErr := fmt.Errorf("directory %s already exists on %s; apply will fail without --force-steps %s (or use a different path)", dir, th.Host, ctx.CurrentStepID)
							ctx.ReportPrecheckIssue(runner.PrecheckIssue{
								StepID:      "C-005",
								StepName:    "Create Data Directories",
								Host:        th.Host,
								Severity:    runner.PrecheckSeverityError,
								Code:        "PC.DB.DIR.ALREADY_EXISTS",
								Message:     dirErr.Error(),
								Remediation: "choose a different directory, or use --force-steps C-005 to delete and recreate (this will wipe the directory)",
							})
							return dirErr
						}
					} else {
						// 缺失则在 apply 阶段创建
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepID:      "C-005",
							StepName:    "Create Data Directories",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityInfo,
							Code:        "PC.DB.DIR.MISSING",
							Message:     fmt.Sprintf("directory does not exist: %s; apply will mkdir and chown to %s:%s", dir, user, group),
							Remediation: "you may pre-create the directory and set ownership/permissions, or let apply create it",
						})
					}
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			// YAC 模式下，force 执行需要在所有节点执行（通过 ctx.HostsToRun() 遍历所有节点）
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				installPath := hctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
				dataPath := hctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
				logPath := hctx.GetParamString("db_log_path", "/data/yashan/log")
				user := hctx.GetParamString("os_user", "yashan")
				group := hctx.GetParamString("os_group", "yashan")
				isForce := hctx.IsForceStep()
				dirs := []string{installPath, dataPath, logPath}

				for _, dir := range dirs {
					dirQ := commonos.ShellSingleQuote(dir)
					// 检查目录是否存在
					result, _ := hctx.Execute(fmt.Sprintf("test -d %s", dirQ), false)
					dirExists := result != nil && result.GetExitCode() == 0

					if dirExists {
						if isForce {
							// 强制模式：如果目录存在，删除它
							hctx.Logger.Warn("Force mode: deleting existing directory %s on %s", dir, th.Host)
							if !commonos.IsSafeUnixRmRfPath(dir) {
								return fmt.Errorf("refusing to delete directory %s on %s: path is not under allowed installation roots", dir, th.Host)
							}
							if _, err := hctx.ExecuteWithCheck(fmt.Sprintf("rm -rf %s", dirQ), true); err != nil {
								return fmt.Errorf("failed to delete directory %s on %s: %w", dir, th.Host, err)
							}
						} else {
							// 非强制模式：目录已存在，报错
							return fmt.Errorf("directory %s already exists on %s, use -f %s to delete and recreate", dir, th.Host, ctx.CurrentStepID)
						}
					}

					// 创建目录（目录不存在或已被删除）
					hctx.Logger.Info("Creating directory: %s on %s", dir, th.Host)
					cmd := fmt.Sprintf("mkdir -p %s", dirQ)
					if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
						return fmt.Errorf("failed to create directory %s on %s: %w", dir, th.Host, err)
					}

					// 设置目录属主和属组
					cmd = fmt.Sprintf("chown -R %s:%s %s", user, group, dirQ)
					if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
						return fmt.Errorf("failed to set ownership on %s: %w", th.Host, err)
					}
					hctx.Logger.Info("Created directory: %s (owner: %s:%s) on %s", dir, user, group, th.Host)
				}
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				installPath := hctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
				dataPath := hctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")
				logPath := hctx.GetParamString("db_log_path", "/data/yashan/log")
				dirs := []string{installPath, dataPath, logPath}
				for _, dir := range dirs {
					result, _ := hctx.Execute(fmt.Sprintf("test -d %s", commonos.ShellSingleQuote(dir)), false)
					if result == nil || result.GetExitCode() != 0 {
						return fmt.Errorf("directory %s not found on %s", dir, th.Host)
					}
				}
			}
			return nil
		},
	}
}
