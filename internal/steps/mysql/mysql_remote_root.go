package mysql

import (
	"fmt"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepRemoteRoot optionally enables root remote login.
func stepRemoteRoot() *runner.Step {
	return &runner.Step{
		Name:        "Configure Remote Root",
		Description: "Optional root@% access",
		Tags:        []string{"mysql", "security", "mysql-instance"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("mysql_remote_root", false) {
				return fmt.Errorf("mysql_remote_root disabled")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			layout, _ := layoutFromCtx(ctx)
			password := ctx.GetParamString("mysql_root_password", "")
			sql := fmt.Sprintf(`CREATE USER IF NOT EXISTS 'root'@'%%' IDENTIFIED BY '%s';
GRANT ALL PRIVILEGES ON *.* TO 'root'@'%%' WITH GRANT OPTION;
FLUSH PRIVILEGES;`, commonsql.EscapeSQLString(password))
			return commonsql.ExecuteMysqlSQL(ctx, layout, password, sql)
		},
	}
}
