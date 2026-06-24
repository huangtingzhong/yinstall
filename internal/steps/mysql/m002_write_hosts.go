package mysql

import (
	"fmt"

	"github.com/yinstall/internal/runner"
	ossteps "github.com/yinstall/internal/steps/os"
)

// StepM002WriteHosts sets hostname and managed /etc/hosts block via OS step B-023.
// Runs when --skip-os (B-023 is in OS baseline otherwise).
func StepM002WriteHosts() *runner.Step {
	b023 := ossteps.StepB023SetHostname()
	return &runner.Step{
		ID:          "M-002",
		Name:        "Set Hostname and Hosts",
		Description: "Set system hostname and yinstall managed /etc/hosts block (B-023)",
		Tags:        []string{"mysql", "hosts", "mysql-instance"},
		Optional:    true,
		Global:      true,
		PreCheck: func(ctx *runner.StepContext) error {
			if ctx.GetTargetPlatform() == PlatformWindows {
				return fmt.Errorf("hostname/hosts not supported on windows")
			}
			if !ctx.GetParamBool("mysql_skip_os", true) {
				return fmt.Errorf("hostname/hosts handled by B-023 in OS baseline")
			}
			ensureMysqlHostnameDefault(ctx)
			if b023.PreCheck != nil {
				return b023.PreCheck(ctx)
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mysqlLogPhase(ctx, "plan", "M-002 delegate B-023")
			ensureMysqlHostnameDefault(ctx)
			if b023.Action == nil {
				return nil
			}
			return b023.Action(ctx)
		},
		PostCheck: b023.PostCheck,
	}
}

func ensureMysqlHostnameDefault(ctx *runner.StepContext) {
	if ctx.Params == nil {
		ctx.Params = map[string]interface{}{}
	}
	ctx.Params["os_hostname_default_prefix"] = "mysql"
}
