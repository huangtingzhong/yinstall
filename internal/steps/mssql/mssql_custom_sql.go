package mssql

import (
	"fmt"
	"os"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepCustomSql() *runner.Step {
	return &runner.Step{
		Name:     "Custom SQL",
		Tags:     []string{"mssql", "mssql-instance"},
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetParamString("mssql_custom_sql_script", "") == "" {
				return runner.NewStepSkippedError("no custom SQL")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			scriptPath := strings.TrimSpace(ctx.GetParamString("mssql_custom_sql_script", ""))
			if scriptPath == "" {
				return fmt.Errorf("mssql_custom_sql_script not set")
			}
			layout := commonmssql.ResolveLayoutFromContext(ctx)
			remoteScript := layout.AdminBase + `\custom_install.sql`
			if ctx.DryRun || ctx.Precheck {
				ctx.LogScriptPreview("sql", "MS-017 custom SQL", scriptPath)
				return nil
			}
			content, err := os.ReadFile(scriptPath)
			if err != nil {
				return fmt.Errorf("read custom SQL script: %w", err)
			}
			if err := commonfile.RemoteWriteTextFile(ctx, remoteScript, string(content), false); err != nil {
				return fmt.Errorf("upload custom SQL: %w", err)
			}
			cmd := commonmssql.SqlcmdInputFileCommand(ctx, remoteScript)
			ctx.LogScriptPreview("sqlcmd", "MS-017 custom SQL", cmd)
			_, err = ctx.ExecuteWithCheck(cmd, false)
			if err != nil {
				return err
			}
			return printMssqlInstallSummary(ctx, ctx.CurrentStepID)
		},
	}
}
