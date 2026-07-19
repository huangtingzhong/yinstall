package db

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// stepCreateDataDirs 创建数据库数据、日志与软件目录
func stepCreateDataDirs() *runner.Step {
	return &runner.Step{
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
			expectedOwner := fmt.Sprintf("%s:%s", user, group)

			for _, th := range ctx.HostsToRun() {
				hctx := ctx.ForHost(th)
				isForce := hctx.IsForceStep()
				dirs := []string{installPath, dataPath, logPath}

				for _, dir := range dirs {
					dirQ := commonos.ShellSingleQuote(dir)
					res, _ := hctx.Execute(fmt.Sprintf("if [ -e %s ] && [ ! -d %s ]; then echo NOT_DIR; elif [ -d %s ]; then echo IS_DIR; else echo MISSING; fi",
						dirQ, dirQ, dirQ), false)
					kind := ""
					if res != nil {
						kind = strings.TrimSpace(res.GetStdout())
					}
					if strings.Contains(kind, "NOT_DIR") {
						return fmt.Errorf("path exists but is not a directory on %s: %s", th.Host, dir)
					}
					if strings.Contains(kind, "MISSING") {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Create Data Directories",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityInfo,
							Code:        "PC.DB.DIR.MISSING",
							Message:     fmt.Sprintf("directory does not exist: %s; apply will mkdir and chown to %s:%s", dir, user, group),
							Remediation: "you may pre-create the directory and set ownership/permissions, or let apply create it",
						})
						continue
					}
					if !strings.Contains(kind, "IS_DIR") {
						continue
					}

					if isForce {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Create Data Directories",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityWarn,
							Code:        "PC.DB.DIR.FORCE_DELETE",
							Message:     fmt.Sprintf("directory already exists: %s; %s detected; apply will rm -rf and recreate (owner will be set to %s:%s)", dir, hctx.ForceStepsHint(), user, group),
							Remediation: "ensure the directory does not contain important data; back up first or choose a different path",
						})
						continue
					}

					if DirHasMinDepthEntries(hctx, dir) {
						dirErr := fmt.Errorf("directory %s on %s is not empty; remove existing files manually or re-run with %s (or use a different path)", dir, th.Host, hctx.ForceStepsHint())
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Create Data Directories",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityError,
							Code:        "PC.DB.DIR.NOT_EMPTY",
							Message:     dirErr.Error(),
							Remediation: fmt.Sprintf("clean the directory, choose a different path, or use %s to delete and recreate", hctx.ForceStepsHint()),
						})
						return dirErr
					}

					ownerRes, _ := hctx.Execute(fmt.Sprintf("stat -c '%%U:%%G' %s 2>/dev/null", dirQ), false)
					owner := ""
					if ownerRes != nil {
						owner = strings.TrimSpace(ownerRes.GetStdout())
					}
					if owner != "" && owner != expectedOwner {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Create Data Directories",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityWarn,
							Code:        "PC.DB.DIR.OWNERSHIP_MISMATCH",
							Message:     fmt.Sprintf("directory exists but ownership mismatches: %s current=%s expected=%s; apply will chown -R to fix", dir, owner, expectedOwner),
							Remediation: "if you do not want recursive chown, fix ownership manually or use a dedicated directory",
						})
					} else {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Create Data Directories",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityInfo,
							Code:        "PC.DB.DIR.EMPTY_EXISTS",
							Message:     fmt.Sprintf("empty directory already exists: %s; apply will verify ownership and skip mkdir", dir),
							Remediation: "no action required if ownership is correct; empty pre-created directories are supported",
						})
					}
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			hosts := ctx.HostsToRun()
			dbLogPhase(ctx, "plan", fmt.Sprintf("hosts=%d dirs=3-per-host", len(hosts)))
			for _, th := range hosts {
				dbLogPhase(ctx, "host-start", fmt.Sprintf("host=%s dirs=3", th.Host))
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
					result, _ := hctx.Execute(fmt.Sprintf("test -d %s", dirQ), false)
					dirExists := result != nil && result.GetExitCode() == 0

					if dirExists {
						if isForce {
							hctx.Logger.Warn("Force mode: deleting existing directory %s on %s", dir, th.Host)
							if err := commonos.ValidateDeletePath(dir); err != nil {
								return fmt.Errorf("refusing to delete directory %s on %s: %w", dir, th.Host, err)
							}
							if _, err := hctx.ExecuteWithCheck(fmt.Sprintf("rm -rf %s", dirQ), true); err != nil {
								return fmt.Errorf("failed to delete directory %s on %s: %w", dir, th.Host, err)
							}
						} else {
							if DirHasMinDepthEntries(hctx, dir) {
								return fmt.Errorf("directory %s on %s is not empty; remove existing files manually or re-run with %s (or global -F) to delete and recreate the directory", dir, th.Host, hctx.ForceStepsHint())
							}
							ownerRes, _ := hctx.Execute(fmt.Sprintf("stat -c '%%U' %s", dirQ), false)
							owner := ""
							if ownerRes != nil && ownerRes.GetStdout() != "" {
								owner = strings.TrimSpace(ownerRes.GetStdout())
							}
							if owner == user {
								hctx.Logger.Info("Directory %s already exists and is empty (owner %s), skipping creation", dir, user)
								dbLogPhase(hctx, "dir-skip", fmt.Sprintf("dir=%s skip=empty_exists", dir))
								continue
							}
							if owner != "" {
								hctx.Logger.Info("Empty directory %s exists but owner is %s, fixing ownership to %s:%s", dir, owner, user, group)
								cmd := fmt.Sprintf("chown -R %s:%s %s", user, group, dirQ)
								if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
									return fmt.Errorf("failed to fix ownership on %s: %w", th.Host, err)
								}
								dbLogPhase(hctx, "dir-chown", fmt.Sprintf("dir=%s", dir))
								continue
							}
							return fmt.Errorf("directory %s already exists on %s, use -f %s to delete and recreate", dir, th.Host, ctx.CurrentStepID)
						}
					}

					hctx.Logger.Info("Creating directory: %s on %s", dir, th.Host)
					cmd := fmt.Sprintf("mkdir -p %s", dirQ)
					if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
						return fmt.Errorf("failed to create directory %s on %s: %w", dir, th.Host, err)
					}

					cmd = fmt.Sprintf("chown -R %s:%s %s", user, group, dirQ)
					if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
						return fmt.Errorf("failed to set ownership on %s: %w", th.Host, err)
					}
					hctx.Logger.Info("Created directory: %s (owner: %s:%s) on %s", dir, user, group, th.Host)
				}
				dbLogPhase(hctx, "host-done", fmt.Sprintf("host=%s", th.Host))
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
