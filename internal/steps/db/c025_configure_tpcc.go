package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// StepC025ConfigureTPCC 配置 TPCC 相关数据库参数
// 须在 C-024（环境变量）之后执行，以便 source env_file 后使用部署实例的 yasql 与 / as sysdba。
func StepC025ConfigureTPCC() *runner.Step {
	return &runner.Step{
		ID:          "C-025",
		Name:        "Configure TPCC Parameters",
		Description: "Configure database parameters for TPCC workload optimization",
		Tags:        []string{"db", "config", "tpcc", "optimization"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			tpccEnabled := ctx.GetParamBool("db_tpcc", false)
			if !tpccEnabled {
				return fmt.Errorf("TPCC optimization not enabled (--db-tpcc=false), skipping")
			}

			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			yasbootPath := path.Join(stageDir, "bin", "yasboot")
			result, err := ctx.Execute(fmt.Sprintf("test -f %s", yasbootPath), false)
			if err != nil || result == nil || result.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("yasboot not found at %s, database may not be deployed yet", yasbootPath))
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			firstHost := ctx.HostsToRun()[0]
			hctx := ctx.ForHost(firstHost)

			user := hctx.GetParamString("os_user", "yashan")
			clusterName := hctx.GetParamString("db_cluster_name", "yashandb")

			envFile := ""
			if envFileVal, ok := ctx.Results["env_file"]; ok {
				if envFileStr, ok := envFileVal.(string); ok {
					envFile = envFileStr
				}
			}
			if envFile == "" {
				beginPort := hctx.GetParamInt("db_begin_port", 1688)
				homeDir, err := commonos.GetUserHomeDir(hctx, user)
				if err != nil {
					homeDir = fmt.Sprintf("/home/%s", user)
				}
				envFile = commonos.DetermineEnvFile(homeDir, beginPort)
			}

			hctx.Logger.Info("Configuring TPCC optimization parameters...")

			tpccSQLs := []string{
				"ALTER SYSTEM SET UNDO_RETENTION = 30 SCOPE=SPFILE",
				"ALTER SYSTEM SET UNDO_SHRINK_ENABLED = FALSE SCOPE=SPFILE",
				"ALTER SYSTEM SET STATISTICS_LEVEL = 'BASIC' SCOPE=SPFILE",
				"ALTER SYSTEM SET OPEN_CURSORS = 4096 SCOPE=SPFILE",
				"ALTER SYSTEM SET _DATA_BUFFER_PARTS = 8 SCOPE=SPFILE",
				"ALTER SYSTEM SET VM_BUFFER_PARTS = 8 SCOPE=SPFILE",
				"ALTER SYSTEM SET _REPLICATION_BUFFER_SIZE = '128M' SCOPE=SPFILE",
				"ALTER SYSTEM SET REDO_BUFFER_SIZE = '128M' SCOPE=SPFILE",
				"ALTER SYSTEM SET REDO_BUFFER_PARTS = 8 SCOPE=SPFILE",
				"ALTER SYSTEM SET _SESSION_RESERVED_CURSORS = 64 SCOPE=SPFILE",
				"ALTER SYSTEM SET WORK_AREA_HEAP_SIZE = '2M' SCOPE=SPFILE",
				"ALTER SYSTEM SET CHECKPOINT_TIMEOUT = 3600 SCOPE=SPFILE",
				"ALTER SYSTEM SET CHECKPOINT_INTERVAL = '10G' SCOPE=SPFILE",
				"ALTER SYSTEM SET REDOFILE_IO_MODE = 'DIRECT' SCOPE=SPFILE",
				"ALTER SYSTEM SET DB_BLOCK_CHECKSUM = OFF SCOPE=SPFILE",
				"ALTER SYSTEM SET DOUBLE_WRITE_ENABLED = FALSE SCOPE=SPFILE",
				"ALTER SYSTEM SET OPTIMIZER_REAL_TIME_STATISTICS = FALSE SCOPE=SPFILE",
				"ALTER SYSTEM SET COMMIT_LOGGING = BATCH SCOPE=SPFILE",
				"ALTER SYSTEM SET COMMIT_WAIT = NOWAIT SCOPE=SPFILE",
				"ALTER SYSTEM SET DBWR_BUFFER_SIZE = '16M' SCOPE=SPFILE",
				"ALTER SYSTEM SET DBWR_COUNT = 8 SCOPE=SPFILE",
			}

			allSQL := strings.Join(tpccSQLs, ";\n") + ";"

			hctx.Logger.Info("Executing %d TPCC optimization SQLs via yasql (/ as sysdba)...", len(tpccSQLs))
			_, err := commonsql.ExecuteSQLAsSysdbaCtx(hctx, user, envFile, clusterName, allSQL, true)
			if err != nil {
				return fmt.Errorf("TPCC SQL execution failed: %w", err)
			}

			hctx.Logger.Info("All TPCC optimization SQLs executed successfully")
			hctx.Logger.Info("TPCC parameter optimization completed")
			hctx.Logger.Info("Note: Parameter changes require database restart to take effect")

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
