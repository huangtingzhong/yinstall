// e007_check_standby_expansion_paths.go - 备库扩容 install/data/log 目录预检与创建

package standby

import (
	"fmt"
	"path/filepath"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepE007CheckStandbyExpansionPaths 检查 db_install_path、db_data_path、db_log_path：不存在则 mkdir+chown；已存在则必须为空目录并 chown
func StepE007CheckStandbyExpansionPaths() *runner.Step {
	return &runner.Step{
		ID:          "E-007",
		Name:        "Check Standby Expansion Paths",
		Description: "Ensure expansion home/data/log directories exist (or create empty) and contain no files",
		Tags:        []string{"standby", "precheck", "paths"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			install := strings.TrimSpace(ctx.GetParamString("db_install_path", ""))
			data := strings.TrimSpace(ctx.GetParamString("db_data_path", ""))
			logp := strings.TrimSpace(ctx.GetParamString("db_log_path", ""))
			type pathItem struct {
				label string
				path  string
			}
			items := []pathItem{
				{"install (home)", install},
				{"data", data},
				{"log", logp},
			}
			seen := map[string]bool{}
			for _, it := range items {
				if it.path == "" {
					return fmt.Errorf("standby %s path is empty (set --db-home-path/--db-data-path/--db-log-path or rely on defaults)", it.label)
				}
				canonical := filepath.Clean(it.path)
				if seen[canonical] {
					continue
				}
				seen[canonical] = true
				q := commonos.ShellSingleQuote(canonical)
				for _, th := range ctx.HostsToRun() {
					hctx := ctx.ForHost(th)
					hctx.Logger.Info("Prechecking expansion path %s (%s) on %s", it.label, canonical, th.Host)
					existCmd := fmt.Sprintf("if [ -e %s ] && [ ! -d %s ]; then echo NOT_DIR; elif [ -d %s ]; then echo IS_DIR; else echo MISSING; fi", q, q, q)
					existRes, err := hctx.Execute(existCmd, false)
					if err != nil {
						return fmt.Errorf("path check failed on %s for %s: %w", th.Host, canonical, err)
					}
					kind := strings.TrimSpace(existRes.GetStdout())
					if strings.Contains(kind, "NOT_DIR") {
						return fmt.Errorf("path %s (%s) on %s exists but is not a directory", canonical, it.label, th.Host)
					}
					if strings.Contains(kind, "MISSING") {
						// Read-only: report, do not mkdir/chown.
						ctx.ReportPrecheckIssue(runner.PrecheckIssue{
							StepID:      "E-007",
							StepName:    "Check Standby Expansion Paths",
							Host:        th.Host,
							Severity:    runner.PrecheckSeverityWarn,
							Code:        "PC.STANDBY.PATH.MISSING",
							Message:     fmt.Sprintf("path does not exist: %s (%s); apply will create it and chown", canonical, it.label),
							Remediation: "you may pre-create the directory and ensure ownership/permissions are correct",
						})
						continue
					}
					emptyCmd := fmt.Sprintf(`test -z "$(find %s -mindepth 1 2>/dev/null | head -1)" && echo EMPTY || echo NOT_EMPTY`, q)
					emptyRes, err := hctx.Execute(emptyCmd, false)
					if err != nil {
						return fmt.Errorf("directory listing failed on %s for %s: %w", th.Host, canonical, err)
					}
					if emptyRes == nil || !strings.Contains(strings.TrimSpace(emptyRes.GetStdout()), "EMPTY") {
						return fmt.Errorf("directory %s (%s) on %s must be empty before expansion; remove existing files or pick another path", canonical, it.label, th.Host)
					}
				}
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			install := strings.TrimSpace(ctx.GetParamString("db_install_path", ""))
			data := strings.TrimSpace(ctx.GetParamString("db_data_path", ""))
			logp := strings.TrimSpace(ctx.GetParamString("db_log_path", ""))
			user := ctx.GetParamString("os_user", "yashan")
			grp := ctx.GetParamString("os_group", "yashan")

			type pathItem struct {
				label string
				path  string
			}
			items := []pathItem{
				{"install (home)", install},
				{"data", data},
				{"log", logp},
			}

			seen := map[string]bool{}
			for _, it := range items {
				if it.path == "" {
					return fmt.Errorf("standby %s path is empty (set --db-home-path/--db-data-path/--db-log-path or rely on defaults)", it.label)
				}
				canonical := filepath.Clean(it.path)
				if seen[canonical] {
					continue
				}
				seen[canonical] = true

				q := commonos.ShellSingleQuote(canonical)
				uq := commonos.ShellSingleQuote(user)
				gq := commonos.ShellSingleQuote(grp)

				for _, th := range ctx.HostsToRun() {
					hctx := ctx.ForHost(th)
					hctx.Logger.Info("Checking expansion path %s (%s) on %s", it.label, canonical, th.Host)

					existCmd := fmt.Sprintf("if [ -e %s ] && [ ! -d %s ]; then echo NOT_DIR; elif [ -d %s ]; then echo IS_DIR; else echo MISSING; fi", q, q, q)
					existRes, err := hctx.Execute(existCmd, false)
					if err != nil {
						return fmt.Errorf("path check failed on %s for %s: %w", th.Host, canonical, err)
					}
					kind := strings.TrimSpace(existRes.GetStdout())
					if strings.Contains(kind, "NOT_DIR") {
						return fmt.Errorf("path %s (%s) on %s exists but is not a directory", canonical, it.label, th.Host)
					}

					if strings.Contains(kind, "MISSING") {
						hctx.Logger.Info("Creating directory %s on %s", canonical, th.Host)
						mk := fmt.Sprintf("mkdir -p %s && chown %s:%s %s", q, uq, gq, q)
						if _, err := hctx.Execute(mk, false); err != nil {
							return fmt.Errorf("failed to create %s on %s: %w", canonical, th.Host, err)
						}
						verify, _ := hctx.Execute(fmt.Sprintf("test -d %s && echo OK", q), false)
						if verify == nil || verify.GetExitCode() != 0 || !strings.Contains(verify.GetStdout(), "OK") {
							return fmt.Errorf("mkdir failed for %s on %s", canonical, th.Host)
						}
						hctx.Logger.Info("OK: Created empty directory %s on %s", canonical, th.Host)
						continue
					}

					emptyCmd := fmt.Sprintf(`test -z "$(find %s -mindepth 1 2>/dev/null | head -1)" && echo EMPTY || echo NOT_EMPTY`, q)
					emptyRes, err := hctx.Execute(emptyCmd, false)
					if err != nil {
						return fmt.Errorf("directory listing failed on %s for %s: %w", th.Host, canonical, err)
					}
					if emptyRes == nil || !strings.Contains(strings.TrimSpace(emptyRes.GetStdout()), "EMPTY") {
						return fmt.Errorf("directory %s (%s) on %s must be empty before expansion; remove existing files or pick another path", canonical, it.label, th.Host)
					}
					chownCmd := fmt.Sprintf("chown %s:%s %s", uq, gq, q)
					if _, err := hctx.Execute(chownCmd, false); err != nil {
						return fmt.Errorf("chown failed for %s on %s: %w", canonical, th.Host, err)
					}
					hctx.Logger.Info("OK: Path %s (%s) exists, is empty, and ownership set on %s", canonical, it.label, th.Host)
				}
			}
			return nil
		},
	}
}
