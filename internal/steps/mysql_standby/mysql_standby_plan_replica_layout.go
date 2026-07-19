package mysql_standby

import (
	"fmt"

	"github.com/yinstall/internal/runner"
)

// stepPlanReplicaLayout validates replica version and plans server_id.
func stepPlanReplicaLayout() *runner.Step {
	return &runner.Step{
		Name:        "Plan Replica Layout",
		Description: "Resolve replica software (match primary version), validate version, plan server_id",
		Tags:        []string{"mysql-standby", "replica"},
		PreCheck: func(ctx *runner.StepContext) error {
			if replicaPort(ctx) <= 0 {
				return fmt.Errorf("replica_port is required")
			}
			if replicaPort(ctx) == primaryPort(ctx) && primaryHost(ctx) == ctx.Executor.Host() {
				return fmt.Errorf("replica port must differ from primary on same host")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "version-check", "MR-007 plan replica layout")
			primaryVer, _ := ctx.Params["primary_mysql_version"].(string)
			if primaryVer == "" {
				if v, ok := ctx.Results["primary_mysql_version"].(string); ok {
					primaryVer = v
				}
			}

			if !ctx.DryRun && !ctx.Precheck {
				plan, err := resolveReplicaSoftware(ctx, primaryVer)
				if err != nil {
					return err
				}
				applyReplicaSoftwarePlan(ctx, plan)
				if plan.Version != "" {
					ctx.Logger.Info("Replica software: version=%s home=%s source=%s package=%s",
						plan.Version, plan.Home, plan.Source, plan.Package)
				}
			} else if rv := ctx.GetParamString("mysql_version", ""); rv != "" {
				ctx.SetResult("replica_mysql_version", rv)
			}

			primarySID := ctx.GetParamInt("primary_server_id", 0)
			if v, ok := ctx.Results["primary_server_id"].(int); ok && primarySID == 0 {
				primarySID = v
			}
			if v, ok := ctx.Results["primary_server_id"].(float64); ok && primarySID == 0 {
				primarySID = int(v)
			}
			replicaSID := ctx.GetParamInt("mysql_server_id", 0)
			if replicaSID == 0 && primarySID > 0 {
				replicaSID = primarySID + 1
			}
			if replicaSID == primarySID && primarySID > 0 {
				return fmt.Errorf("replica server_id must differ from primary (%d)", primarySID)
			}
			ctx.SetResult("replica_server_id", replicaSID)
			ctx.Logger.Info("Replica server_id=%d (primary=%d)", replicaSID, primarySID)
			return nil
		},
	}
}
