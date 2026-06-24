package win_os

import (
	"strings"

	"github.com/yinstall/internal/runner"
)

func StepW008ServiceAccountPrep() *runner.Step {
	return &runner.Step{
		ID:          "W-008",
		Name:        "Service Account Prep",
		Description: "Pre-check domain or local SQL service account",
		Tags:        []string{"win-os", "win-os-install", "account"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			acct := strings.TrimSpace(ctx.GetParamString("mssql_sqlsvc_account", ""))
			if acct == "" {
				acct = strings.TrimSpace(ctx.GetParamString("os_service_account", ""))
			}
			if acct == "" {
				return runner.NewStepSkippedError("mssql_sqlsvc_account not set")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-008 service account")
			acct := strings.TrimSpace(ctx.GetParamString("mssql_sqlsvc_account", ""))
			if acct == "" {
				acct = ctx.GetParamString("os_service_account", "")
			}
			ctx.Logger.Info("W-008: verify service account %s exists in AD (manual pre-create required)", acct)
			return nil
		},
	}
}
