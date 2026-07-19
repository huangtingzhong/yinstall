package db

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// stepApplySpfileParams 按 --db-spfile-params 执行 ALTER SYSTEM SET ... SCOPE=SPFILE（无 PreCheck/PostCheck）。
// 须在 C-023（环境变量）之后执行；参数为空时本步直接成功跳过。
func stepApplySpfileParams() *runner.Step {
	return &runner.Step{
		Name:        "Apply SPFILE Parameters",
		Description: "Apply custom database parameters to SPFILE via --db-spfile-params",
		Tags:        []string{"db", "config", "spfile"},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-026: Apply SPFILE Parameters")
			spec := strings.TrimSpace(ctx.GetParamString("db_spfile_params", ""))
			if spec == "" {
				ctx.Logger.Info("No --db-spfile-params specified, skipping SPFILE parameter changes")
				return nil
			}

			params, err := ParseSpfileParams(spec)
			if err != nil {
				return fmt.Errorf("invalid --db-spfile-params: %w", err)
			}

			sqls := BuildAlterSystemSpfileSQLs(params)
			allSQL := strings.Join(sqls, ";\n") + ";"

			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)

			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")
			envFile, err := resolveDBEnvFile(ctx, hctx)
			if err != nil {
				return err
			}

			hctx.Logger.Info("Applying %d SPFILE parameter(s) in CDB$ROOT via yasql (/ as sysdba)...", len(params))
			for _, p := range params {
				hctx.Logger.Info("  %s = %s", p.Name, p.Value)
			}
			for _, s := range sqls {
				hctx.Logger.Info("  SQL: %s", s)
			}

			if _, err := dbRunSQLPhase(hctx, user, envFile, clusterName, "spfile-params", allSQL, true); err != nil {
				return fmt.Errorf("SPFILE parameter SQL execution failed: %w", err)
			}

			hctx.Logger.Info("SPFILE parameters applied successfully")
			hctx.Logger.Info("Note: SPFILE changes take effect after C-030 cluster restart")
			return nil
		},
	}
}
