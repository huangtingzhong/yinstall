package mysql_standby

import (
	"fmt"

	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

// StepMR009CopyPatchReplicaCnf copies primary cnf to replica and applies patch plan.
func StepMR009CopyPatchReplicaCnf() *runner.Step {
	return &runner.Step{
		ID:          "MR-009",
		Name:        "Copy and Patch Replica my.cnf",
		Description: "Copy primary cnf to replica and apply patch plan",
		Tags:        []string{"mysql-standby", "replica"},
		PreCheck: func(ctx *runner.StepContext) error {
			if err := skipUnlessStandbyReplicationStage(ctx); err != nil {
				return err
			}
			content, _ := ctx.Results["primary_cnf_content"].(string)
			if content == "" {
				if c, ok := ctx.Params["primary_cnf_content"].(string); ok {
					content = c
				}
			}
			if content == "" && !ctx.DryRun && !ctx.Precheck {
				return fmt.Errorf("primary_cnf_content missing; run MR-002 first")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			standbyLogPhase(ctx, "plan", "MR-009 copy patch cnf")
			content, _ := ctx.Results["primary_cnf_content"].(string)
			if content == "" {
				content, _ = ctx.Params["primary_cnf_content"].(string)
			}
			primarySID := 0
			if v, ok := ctx.Results["primary_server_id"].(int); ok {
				primarySID = v
			}
			replicaSID, _ := ctx.Results["replica_server_id"].(int)
			if replicaSID == 0 {
				replicaSID = ctx.GetParamInt("mysql_server_id", 0)
				if replicaSID == 0 && primarySID > 0 {
					replicaSID = primarySID + 1
				}
			}
			plan := commonmysql.PatchPlan{
				ServerID:        replicaSID,
				PrimaryPort:     primaryPort(ctx),
				PrimaryPlatform: ctx.GetParamString("primary_platform", ""),
				ReplicaPlatform: ctx.GetTargetPlatform(),
			}
			if replicaPort(ctx) != primaryPort(ctx) {
				p := replicaPort(ctx)
				plan.Port = &p
			}
			if ctx.GetParamString("mysql_read_only", "") == "on" {
				if plan.ExplicitParams == nil {
					plan.ExplicitParams = map[string]string{}
				}
				plan.ExplicitParams["read_only"] = "1"
			}
			if plan.PrimaryPlatform != "" && plan.ReplicaPlatform != plan.PrimaryPlatform {
				plan.PlatformFixups = append(plan.PlatformFixups, "drop_socket")
				base := ctx.GetParamString("mysql_base", commonmysql.DefaultBase(plan.ReplicaPlatform))
				ver := ctx.GetParamString("mysql_version", layoutVersionFromServerVersion(ctx.GetParamString("primary_mysql_version", "8.0.46")))
				layout := commonmysql.LayoutFromParams(plan.ReplicaPlatform, base, replicaPort(ctx), ver)
				plan.PathOverrides = map[string]string{
					"datadir":   layout.Data,
					"log-error": layout.Other + "/error.log",
				}
			}
			patched, err := commonmysql.PatchReplicaCnf(content, plan)
			if err != nil {
				return err
			}
			base := ctx.GetParamString("mysql_base", commonmysql.DefaultBase(ctx.GetTargetPlatform()))
			ver := ctx.GetParamString("mysql_version", "8.0.46")
			layout := commonmysql.LayoutFromParams(ctx.GetTargetPlatform(), base, replicaPort(ctx), ver)
			cnfPath := cnfPathForLayout(layout, ctx.GetTargetPlatform())
			if ctx.DryRun || ctx.Precheck {
				ctx.LogScriptPreview("file", cnfPath, patched)
				return nil
			}
			return writeRemoteFile(ctx, cnfPath, patched)
		},
	}
}
