package mysql

import (
	"fmt"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepSetRootPassword sets root password via SQL.
func stepSetRootPassword() *runner.Step {
	return &runner.Step{
		Name:        "Set Root Password",
		Description: "ALTER USER root@localhost",
		Tags:        []string{"mysql", "password", "mysql-instance"},
		Action: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return err
			}
			password := ctx.GetParamString("mysql_root_password", "")
			if password == "" {
				return fmt.Errorf("mysql_root_password required")
			}
			sql := fmt.Sprintf("ALTER USER 'root'@'localhost' IDENTIFIED BY '%s';",
				commonsql.EscapeSQLString(password))
			return commonsql.ExecuteMysqlSQL(ctx, layout, "", sql)
		},
	}
}
