package db

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepC022ConfigureTPCC Configure TPCC optimization parameters
// This step runs after C-021 (Deploy Database) to configure database parameters for TPCC workload.
func StepC022ConfigureTPCC() *runner.Step {
	return &runner.Step{
		ID:          "C-022",
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
				return fmt.Errorf("yasboot not found at %s, database may not be deployed yet", yasbootPath)
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			user := ctx.GetParamString("os_user", "yashan")
			sysPassword := ctx.GetParamString("db_admin_password", "Yashan1!")

			ctx.Logger.Info("Configuring TPCC optimization parameters...")

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
			yasqlPath := path.Join(stageDir, "bin", "yasql")
			quotedPwd := commonos.YasqlQuotePassword(sysPassword)
			yasqlCmd := fmt.Sprintf("%s sys/%s -c %s <<'TPCC_EOF'\n%s\nTPCC_EOF", yasqlPath, quotedPwd, clusterName, allSQL)

			ctx.Logger.Info("Executing %d TPCC optimization SQLs via yasql...", len(tpccSQLs))
			result, err := commonos.ExecuteAsUser(ctx, user, yasqlCmd, false)
			if err != nil {
				ctx.Logger.Warn("TPCC SQL execution error: %v", err)
			} else if result.GetExitCode() != 0 {
				ctx.Logger.Warn("TPCC SQL execution returned exit code %d: %s", result.GetExitCode(), result.GetStderr())
			} else {
				ctx.Logger.Info("All TPCC optimization SQLs executed successfully")
			}

			ctx.Logger.Info("TPCC parameter optimization completed")
			ctx.Logger.Info("Note: Parameter changes require database restart to take effect")

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
