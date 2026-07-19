package mssql

import (
	"fmt"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepConfigureMaxMemory() *runner.Step {
	return &runner.Step{
		Name:     "Configure Max Server Memory",
		Tags:     []string{"mssql", "mssql-instance", "memory"},
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			explicit := ctx.GetParamInt("mssql_max_memory_mb", 0)
			pct := ctx.GetParamInt("mssql_memory_percent", 90)
			if explicit <= 0 && pct <= 0 {
				return runner.NewStepSkippedError("mssql memory not configured")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			explicit := ctx.GetParamInt("mssql_max_memory_mb", 0)
			pct := ctx.GetParamInt("mssql_memory_percent", 90)
			var totalRAM uint64
			var err error
			if ctx.DryRun || ctx.Precheck {
				totalRAM = 16 * 1024 * 1024 * 1024
			} else {
				if err := commonmssql.PrepareSqlcmdSession(ctx); err != nil {
					return err
				}
				totalRAM, err = commonmssql.WindowsTotalMemoryBytes(ctx)
				if err != nil {
					return err
				}
			}
			maxMB, ok, err := commonmssql.ComputeMaxServerMemoryMB(totalRAM, explicit, pct)
			if err != nil {
				return err
			}
			if !ok {
				return runner.NewStepSkippedError("mssql memory not configured")
			}
			ctx.Logger.Info("MS-018: set max server memory to %d MB (explicit=%d, percent=%d, total_ram_bytes=%d)",
				maxMB, explicit, pct, totalRAM)
			ctx.SetResult("mssql_max_server_memory_mb", maxMB)
			query := commonmssql.ConfigureMaxMemorySQL(maxMB)
			cmd := commonmssql.SqlcmdQueryCommand(ctx, query)
			ctx.LogScriptPreview("sqlcmd", "MS-018 max server memory", cmd)
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			_, err = ctx.ExecuteWithCheck(cmd, false)
			if err != nil {
				return fmt.Errorf("configure max server memory: %w", err)
			}
			return nil
		},
	}
}
