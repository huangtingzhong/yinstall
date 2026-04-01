package db

import (
	"fmt"
	"path/filepath"

	"github.com/yinstall/internal/runner"
)

// StepC016BConfigureTPCC Configure TPCC optimization parameters
// This step runs after C-016 (Deploy Database) to configure database parameters for TPCC workload
func StepC016BConfigureTPCC() *runner.Step {
	return &runner.Step{
		ID:          "C-016B",
		Name:        "Configure TPCC Parameters",
		Description: "Configure database parameters for TPCC workload optimization",
		Tags:        []string{"db", "config", "tpcc", "optimization"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			// Check if TPCC optimization is enabled
			tpccEnabled := ctx.GetParamBool("db_tpcc", false)
			if !tpccEnabled {
				return fmt.Errorf("TPCC optimization not enabled (--db-tpcc=false), skipping")
			}

			// Check if database is deployed
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")

			// Check if yasboot exists
			yasbootPath := filepath.Join(stageDir, "bin", "yasboot")
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

			// TPCC optimization SQL statements
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

			// Build SQL command to execute via yasql
			for _, sql := range tpccSQLs {
				ctx.Logger.Info("Executing: %s", sql)

				// Build yasql command with credentials
				yasqlCmd := fmt.Sprintf("cd %s && su - %s -c 'echo \"%s\" | bin/yasql sys/%s -c %s'",
					stageDir, user, sql, sysPassword, clusterName)

				result, err := ctx.Execute(yasqlCmd, false)
				if err != nil {
					ctx.Logger.Warn("Failed to execute SQL (continuing): %s, error: %v", sql, err)
					continue
				}

				if result.GetExitCode() != 0 {
					ctx.Logger.Warn("SQL execution failed (continuing): %s, stderr: %s", sql, result.GetStderr())
					continue
				}

				ctx.Logger.Info("✓ Successfully executed: %s", sql)
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
