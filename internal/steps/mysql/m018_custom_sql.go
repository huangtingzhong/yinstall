package mysql

import (
	"fmt"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// StepM018CustomSQL runs optional custom SQL script.
func StepM018CustomSQL() *runner.Step {
	return &runner.Step{
		ID:          "M-018",
		Name:        "Execute Custom SQL",
		Description: "Run user-provided SQL script",
		Tags:        []string{"mysql", "sql", "mysql-instance"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetParamString("mysql_custom_sql_script", "") == "" {
				return fmt.Errorf("mysql_custom_sql_script not set")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			layout, _ := layoutFromCtx(ctx)
			script := ctx.GetParamString("mysql_custom_sql_script", "")
			password := ctx.GetParamString("mysql_root_password", "")
			return commonsql.ExecuteMysqlScript(ctx, layout, password, script)
		},
	}
}
