package win_os

import (
	"fmt"
	"strconv"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	commonwin "github.com/yinstall/internal/common/win_os"
	"github.com/yinstall/internal/runner"
)

func stepSpnVerifyRegister() *runner.Step {
	return &runner.Step{
		Name:        "SPN Verify/Register",
		Description: "Verify or register MSSQLSvc SPNs in Active Directory",
		Tags:        []string{"win-os", "win-os-post-instance", "spn"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonwin.ShouldRunSPN(ctx) {
				return runner.NewStepSkippedError("SPN not applicable (workgroup or skip)")
			}
			if ctx.GetParamString("mssql_service_name", "") == "" && ctx.GetParamString("mssql_instance", "MSSQLSERVER") != "" {
				// allow when instance installed; MS-008 sets mssql_service_name
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-014 SPN")
			fqdn := ctx.GetParamString("fqdn", "")
			if fqdn == "" {
				if v, ok := ctx.Results["fqdn"].(string); ok {
					fqdn = v
				}
			}
			port := strconv.Itoa(commonmssql.ResolvedListenPort(ctx))
			instance := ctx.GetParamString("mssql_instance", "MSSQLSERVER")
			svcAcct := ctx.GetParamString("mssql_sqlsvc_account", "")
			if svcAcct == "" {
				svcAcct = ctx.GetParamString("os_service_account", "")
			}
			if svcAcct == "" {
				svcAcct = "NT Service\\MSSQLSERVER"
				if !strings.EqualFold(instance, "MSSQLSERVER") {
					svcAcct = "NT Service\\MSSQL$" + instance
				}
			}
			missing, err := commonwin.VerifySPN(ctx, svcAcct, fqdn, port, instance)
			if err != nil {
				return err
			}
			if len(missing) == 0 {
				ctx.SetResult("os_spn_ok", true)
				return nil
			}
			mode := commonwin.SpnMode(ctx)
			if mode == "register" {
				reg, err := commonwin.RegisterSPN(ctx, svcAcct, missing)
				if err != nil {
					return err
				}
				ctx.SetResult("os_spn_registered", reg)
				ctx.SetResult("os_spn_ok", true)
				return nil
			}
			ctx.SetResult("os_spn_missing", missing)
			topology := ctx.GetParamString("mssql_topology", "standalone")
			if topology == "ag_wsfc" {
				return fmt.Errorf("missing SPNs for AG: %v", missing)
			}
			ctx.Logger.Warn("SPN verify failed (non-fatal for standalone): %v", missing)
			return nil
		},
	}
}
