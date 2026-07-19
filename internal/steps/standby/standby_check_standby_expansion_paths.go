// e007_check_standby_expansion_paths.go - 备库扩容 install/data/log 目录预检与创建

package standby

import (
	"fmt"
	"path/filepath"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
	dbsteps "github.com/yinstall/internal/steps/db"
)

type expansionPathItem struct {
	label string
	path  string
}

func expansionPathItems(ctx *runner.StepContext) ([]expansionPathItem, error) {
	EnsureExpansionPathParams(ctx)
	install := strings.TrimSpace(ctx.GetParamString("db_install_path", ""))
	data := strings.TrimSpace(ctx.GetParamString("db_data_path", ""))
	logp := strings.TrimSpace(ctx.GetParamString("db_log_path", ""))
	items := []expansionPathItem{
		{"install (home)", install},
		{"data", data},
		{"log", logp},
	}
	for _, it := range items {
		if it.path == "" {
			return nil, fmt.Errorf("standby %s path is empty (set --db-home-path/--db-data-path/--db-log-path or rely on defaults)", it.label)
		}
	}
	return items, nil
}

func dedupeExpansionPaths(items []expansionPathItem) []expansionPathItem {
	seen := map[string]bool{}
	out := make([]expansionPathItem, 0, len(items))
	for _, it := range items {
		canonical := filepath.Clean(it.path)
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		out = append(out, expansionPathItem{label: it.label, path: canonical})
	}
	return out
}

// stepCheckStandbyExpansionPaths 检查 db_install_path、db_data_path、db_log_path：不存在则 mkdir+chown；已存在则必须为空目录并 chown
func stepCheckStandbyExpansionPaths() *runner.Step {
	return &runner.Step{
		Name:        "Check Standby Expansion Paths",
		Description: "Ensure expansion home/data/log directories exist (or create empty) and contain no files",
		Tags:        []string{"standby", "precheck", "paths"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			items, err := expansionPathItems(ctx)
			if err != nil {
				return err
			}
			user := ctx.GetParamString("os_user", "yashan")
			group := ctx.GetParamString("os_group", "yashan")
			expectedOwner := fmt.Sprintf("%s:%s", user, group)

			for _, it := range dedupeExpansionPaths(items) {
				q := commonos.ShellSingleQuote(it.path)
				for _, th := range ctx.HostsToRun() {
					hctx := ctx.ForHost(th)
					isForce := hctx.IsForceStep()
					hctx.Logger.Info("Prechecking expansion path %s (%s) on %s", it.label, it.path, th.Host)

					existRes, err := hctx.Execute(fmt.Sprintf("if [ -e %s ] && [ ! -d %s ]; then echo NOT_DIR; elif [ -d %s ]; then echo IS_DIR; else echo MISSING; fi", q, q, q), false)
					if err != nil {
						return fmt.Errorf("path check failed on %s for %s: %w", th.Host, it.path, err)
					}
					kind := ""
					if existRes != nil {
						kind = strings.TrimSpace(existRes.GetStdout())
					}
					if strings.Contains(kind, "NOT_DIR") {
						return fmt.Errorf("path %s (%s) on %s exists but is not a directory", it.path, it.label, th.Host)
					}
					if strings.Contains(kind, "MISSING") {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Check Standby Expansion Paths",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityWarn,
							Code:        "PC.STANDBY.PATH.MISSING",
							Message:     fmt.Sprintf("path does not exist: %s (%s); apply will create it and chown", it.path, it.label),
							Remediation: "you may pre-create the directory and ensure ownership/permissions are correct",
						})
						continue
					}
					if !strings.Contains(kind, "IS_DIR") {
						continue
					}

					if isForce {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Check Standby Expansion Paths",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityWarn,
							Code:        "PC.STANDBY.PATH.FORCE_DELETE",
							Message:     fmt.Sprintf("directory already exists: %s (%s); %s detected; apply will rm -rf and recreate (owner will be set to %s:%s)", it.path, it.label, hctx.ForceStepsHint(), user, group),
							Remediation: "ensure the directory does not contain important data; back up first or choose a different path",
						})
						continue
					}

					if dbsteps.DirHasMinDepthEntries(hctx, it.path) {
						dirErr := fmt.Errorf("directory %s (%s) on %s must be empty before expansion; remove existing files, pick another path, or re-run with %s", it.path, it.label, th.Host, hctx.ForceStepsHint())
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Check Standby Expansion Paths",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityError,
							Code:        "PC.STANDBY.PATH.NOT_EMPTY",
							Message:     dirErr.Error(),
							Remediation: fmt.Sprintf("clean the directory, choose a different path, or use %s to delete and recreate", hctx.ForceStepsHint()),
						})
						return dirErr
					}

					ownerRes, _ := hctx.Execute(fmt.Sprintf("stat -c '%%U:%%G' %s 2>/dev/null", q), false)
					owner := ""
					if ownerRes != nil {
						owner = strings.TrimSpace(ownerRes.GetStdout())
					}
					if owner != "" && owner != expectedOwner {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Check Standby Expansion Paths",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityWarn,
							Code:        "PC.STANDBY.PATH.OWNERSHIP_MISMATCH",
							Message:     fmt.Sprintf("directory exists but ownership mismatches: %s (%s) current=%s expected=%s; apply will chown -R to fix", it.path, it.label, owner, expectedOwner),
							Remediation: "if you do not want recursive chown, fix ownership manually or use a dedicated directory",
						})
					} else {
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepName:    "Check Standby Expansion Paths",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityInfo,
							Code:        "PC.STANDBY.PATH.EMPTY_EXISTS",
							Message:     fmt.Sprintf("empty directory already exists: %s (%s); apply will verify ownership and skip mkdir", it.path, it.label),
							Remediation: "no action required if ownership is correct; empty pre-created directories are supported",
						})
					}
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "E-007: Check Standby Expansion Paths")
			standbyLogPhase(ctx, "check-start", "install/data/log paths per host")

			items, err := expansionPathItems(ctx)
			if err != nil {
				return err
			}
			user := ctx.GetParamString("os_user", "yashan")
			group := ctx.GetParamString("os_group", "yashan")

			for _, it := range dedupeExpansionPaths(items) {
				q := commonos.ShellSingleQuote(it.path)

				for _, th := range ctx.HostsToRun() {
					hctx := ctx.ForHost(th)
					isForce := hctx.IsForceStep()
					hctx.Logger.Info("Checking expansion path %s (%s) on %s", it.label, it.path, th.Host)

					existRes, err := hctx.Execute(fmt.Sprintf("if [ -e %s ] && [ ! -d %s ]; then echo NOT_DIR; elif [ -d %s ]; then echo IS_DIR; else echo MISSING; fi", q, q, q), false)
					if err != nil {
						return fmt.Errorf("path check failed on %s for %s: %w", th.Host, it.path, err)
					}
					kind := ""
					if existRes != nil {
						kind = strings.TrimSpace(existRes.GetStdout())
					}
					if strings.Contains(kind, "NOT_DIR") {
						return fmt.Errorf("path %s (%s) on %s exists but is not a directory", it.path, it.label, th.Host)
					}

					dirExists := strings.Contains(kind, "IS_DIR")
					if dirExists {
						if isForce {
							hctx.Logger.Warn("Force mode: deleting existing directory %s on %s", it.path, th.Host)
							if err := commonos.ValidateDeletePath(it.path); err != nil {
								return fmt.Errorf("refusing to delete directory %s on %s: %w", it.path, th.Host, err)
							}
							if _, err := hctx.ExecuteWithCheck(fmt.Sprintf("rm -rf %s", q), true); err != nil {
								return fmt.Errorf("failed to delete directory %s on %s: %w", it.path, th.Host, err)
							}
						} else if dbsteps.DirHasMinDepthEntries(hctx, it.path) {
							return fmt.Errorf("directory %s (%s) on %s must be empty before expansion; remove existing files, pick another path, or re-run with %s", it.path, it.label, th.Host, hctx.ForceStepsHint())
						} else {
							ownerRes, _ := hctx.Execute(fmt.Sprintf("stat -c '%%U' %s", q), false)
							owner := ""
							if ownerRes != nil && ownerRes.GetStdout() != "" {
								owner = strings.TrimSpace(ownerRes.GetStdout())
							}
							if owner == user {
								hctx.Logger.Info("OK: Path %s (%s) exists, is empty, and ownership set on %s", it.path, it.label, th.Host)
								standbyLogPhase(hctx, "path-skip", fmt.Sprintf("path=%s skip=empty_exists", it.path))
								continue
							}
							hctx.Logger.Info("Empty directory %s exists but owner is %s, fixing ownership to %s:%s", it.path, owner, user, group)
							cmd := fmt.Sprintf("chown -R %s:%s %s", user, group, q)
							if _, err := hctx.ExecuteWithCheck(cmd, true); err != nil {
								return fmt.Errorf("chown failed for %s on %s: %w", it.path, th.Host, err)
							}
							hctx.Logger.Info("OK: Path %s (%s) exists, is empty, and ownership set on %s", it.path, it.label, th.Host)
							continue
						}
					}

					hctx.Logger.Info("Creating directory %s on %s", it.path, th.Host)
					if _, err := hctx.ExecuteWithCheck(fmt.Sprintf("mkdir -p %s", q), true); err != nil {
						return fmt.Errorf("failed to create %s on %s: %w", it.path, th.Host, err)
					}
					if _, err := hctx.ExecuteWithCheck(fmt.Sprintf("chown -R %s:%s %s", user, group, q), true); err != nil {
						return fmt.Errorf("chown failed for %s on %s: %w", it.path, th.Host, err)
					}
					verify, _ := hctx.Execute(fmt.Sprintf("test -d %s && echo OK", q), false)
					if verify == nil || verify.GetExitCode() != 0 || !strings.Contains(verify.GetStdout(), "OK") {
						return fmt.Errorf("mkdir failed for %s on %s", it.path, th.Host)
					}
					hctx.Logger.Info("OK: Created empty directory %s on %s", it.path, th.Host)
				}
			}
			standbyLogPhase(ctx, "check-done", fmt.Sprintf("paths=%d hosts=%d", len(items), len(ctx.HostsToRun())))
			return nil
		},
	}
}
