package db

import (
	"fmt"
	"path"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// StepC025ConfigureDefaultProfile 安装后 profile 与 date_format 配置（installer.md §5.4 等）。
// 须在 C-023（环境变量）之后执行，以便 source env_file 后使用 yasql / as sysdba。
func StepC025ConfigureDefaultProfile() *runner.Step {
	return &runner.Step{
		ID:          "C-025",
		Name:        "Configure Default Profile",
		Description: "Configure DEFAULT profile and date_format (SPFILE)",
		Tags:        []string{"db", "profile", "security"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			yasbootPath := path.Join(stageDir, "bin", "yasboot")
			result, err := ctx.Execute(fmt.Sprintf("test -f %s", yasbootPath), false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("yasboot not found at %s, database may not be deployed yet", yasbootPath))
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", fmt.Sprintf("C-025: Configure Default Profile in CDB$ROOT (profile+date_format)"))
			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)

			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile := resolveDBEnvFile(ctx, hctx)

			profileSkipped := false
			if StepContextHasEnableBranch(hctx) {
				profileSkipped = true
				dbLogPhase(hctx, "query-skip", "label=default-profile reason=enable-branch")
				hctx.Logger.Info("Skipping ALTER PROFILE on MASTER: yasboot command/args contain enable-branch (local profile object)")
			} else {
				hctx.Logger.Info("Executing ALTER PROFILE via yasql (/ as sysdba)...")
				res, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "default-profile", c025SQLProfile, true)
				if err != nil {
					if isC025ProfileAlterSkippable(err, res) {
						profileSkipped = true
						dbLogPhase(hctx, "query-skip", "label=default-profile reason=local-object-on-master")
						hctx.Logger.Warn("ALTER PROFILE skipped: %v (apply in user/branch container if required)", err)
					} else {
						return fmt.Errorf("ALTER PROFILE failed: %w", err)
					}
				}
			}
			ctx.SetResult(c025ResultProfileSkipped, profileSkipped)

			hctx.Logger.Info("Executing ALTER SYSTEM date_format (SPFILE) via yasql...")
			if _, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "date-format-spfile", c025SQLDateFormat, true); err != nil {
				return fmt.Errorf("ALTER SYSTEM date_format failed: %w", err)
			}

			hctx.Logger.Info("Post-install SQL completed (profile_skipped=%v)", profileSkipped)
			hctx.Logger.Info("Note: date_format (SPFILE) takes effect after C-030 cluster restart")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			if shouldSkipC025ProfilePostCheck(ctx) {
				dbLogPhase(ctx, "query-skip", "label=verify-failed-login-attempts reason=profile-skipped-or-enable-branch")
				ctx.Logger.Info("Skipping DEFAULT profile verification (enable-branch or profile alter skipped on MASTER)")
				return nil
			}

			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)

			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile := resolveDBEnvFile(ctx, hctx)

			checkSQL := `SELECT limit FROM dba_profiles WHERE profile='DEFAULT' AND resource_name='FAILED_LOGIN_ATTEMPTS'`
			res, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "verify-failed-login-attempts", checkSQL, false)
			if err != nil {
				if res != nil && !runner.CommandExitLogged(err) {
					commonsql.ReportSQLFailure(hctx, checkSQL, res)
				}
				return fmt.Errorf("profile verification query failed: %w", err)
			}

			out := ""
			if res != nil {
				out = strings.ToUpper(res.Stdout)
			}
			if !strings.Contains(out, "UNLIMITED") {
				return fmt.Errorf("FAILED_LOGIN_ATTEMPTS is not UNLIMITED after C-025; query output: %s", strings.TrimSpace(res.Stdout))
			}

			hctx.Logger.Info("Verified DEFAULT profile FAILED_LOGIN_ATTEMPTS = UNLIMITED")
			return nil
		},
	}
}
