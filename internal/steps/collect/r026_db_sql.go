// r026_db_sql.go - 数据库 SQL 目录信息采集（可选）
// 通过 sysdba 连接执行精选 SQL，获取 V$DATABASE、V$VERSION、V$INSTANCE、
// V$PARAMETER 等核心元数据，写入 db/sql/ 目录。
// 使用 collectRunSQL（方案D SSH session 超时）代替 GNU timeout 包裹，
// 以避免 yasql heredoc 与 timeout bash -c 的 shell 解析冲突。
package collect

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// StepR026DBSQLCatalog 返回 R-026 步骤：采集数据库 SQL 目录信息（Optional）。
func StepR026DBSQLCatalog() *runner.Step {
	return &runner.Step{
		ID:       "R-026",
		Name:     "Collect DB SQL catalog",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if getCollectEnvFile(ctx) == "" {
				return fmt.Errorf("env_file not available, skipping R-026")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			osUser := getCollectOSUser(ctx)
			envFile := getCollectEnvFile(ctx)
			clusterName := getCollectClusterName(ctx)
			if clusterName == "" {
				clusterName = "yashandb"
			}
			dir := filepath.Join(collectHostDir(ctx), "db", "sql")

			queries := []struct {
				sql  string
				dest string
			}{
				{"SELECT * FROM V$DATABASE;", filepath.Join(dir, "v_database.txt")},
				{"SELECT * FROM V$VERSION;", filepath.Join(dir, "v_version.txt")},
				{"SELECT * FROM V$INSTANCE;", filepath.Join(dir, "v_instance.txt")},
				{"SELECT NAME,VALUE,DESCRIPTION FROM V$PARAMETER ORDER BY NAME;", filepath.Join(dir, "v_parameter.txt")},
				{"SELECT * FROM V$SYSSTAT WHERE NAME LIKE '%buffer%' OR NAME LIKE '%redo%' OR NAME LIKE '%session%';", filepath.Join(dir, "v_sysstat_key.txt")},
				{"SELECT STATUS, COUNT(*) FROM V$SESSION GROUP BY STATUS;", filepath.Join(dir, "v_session_summary.txt")},
				{"SELECT * FROM V$TABLESPACE;", filepath.Join(dir, "v_tablespace.txt")},
				{"SELECT * FROM V$DATAFILE;", filepath.Join(dir, "v_datafile.txt")},
				{"SELECT * FROM V$LOG;", filepath.Join(dir, "v_log.txt")},
				{"SELECT * FROM V$LOGFILE;", filepath.Join(dir, "v_logfile.txt")},
				{"SELECT * FROM V$ARCHIVE;", filepath.Join(dir, "v_archive.txt")},
			}

			sqlTimeout := collectSQLTimeout(ctx)
			collectLogPhase(ctx, "plan",
				fmt.Sprintf("queries=%d sql_timeout=%ds cluster=%s dir=db/sql",
					len(queries)+1, int(sqlTimeout.Seconds()), clusterName))

			for i, q := range queries {
				dest := collectDestLabel(ctx, q.dest)
				collectLogPhase(ctx, "query-start",
					fmt.Sprintf("%d/%d dest=%s sql=%s", i+1, len(queries), dest, collectSQLLabel(q.sql)))

				stdout, err := collectRunSQL(ctx, osUser, envFile, q.sql, sqlTimeout)
				output := stdout
				if err != nil {
					appendWarning(ctx, "R-026", fmt.Sprintf("SQL failed: %v", err))
					if output == "" {
						output = fmt.Sprintf("-- error: %v\n", err)
					}
					collectLogPhase(ctx, "query-fail",
						fmt.Sprintf("dest=%s %s err=%v", dest, collectOutputStats(stdout), err))
				} else {
					collectLogPhase(ctx, "query-done",
						fmt.Sprintf("dest=%s %s", dest, collectOutputStats(stdout)))
				}
				if werr := writeTextFile(q.dest, output); werr != nil {
					appendWarning(ctx, "R-026", fmt.Sprintf("write %s: %v", q.dest, werr))
				}
			}

			paramSQL := "SELECT NAME,VALUE FROM V$PARAMETER ORDER BY NAME;"
			collectLogPhase(ctx, "query-start",
				fmt.Sprintf("extra dest=results/sql_v_parameter sql=%s", collectSQLLabel(paramSQL)))
			paramOut, paramErr := collectRunSQL(ctx, osUser, envFile, paramSQL, sqlTimeout)
			if paramErr != nil {
				collectLogPhase(ctx, "query-fail",
					fmt.Sprintf("dest=results/sql_v_parameter %s err=%v", collectOutputStats(paramOut), paramErr))
			} else {
				collectLogPhase(ctx, "query-done",
					fmt.Sprintf("dest=results/sql_v_parameter %s", collectOutputStats(paramOut)))
			}
			if paramOut != "" {
				ctx.Results["sql_v_parameter"] = paramOut
			}

			ctx.Logger.Info("[R-026] DB SQL catalog collected to %s", dir)
			return nil
		},
	}
}
