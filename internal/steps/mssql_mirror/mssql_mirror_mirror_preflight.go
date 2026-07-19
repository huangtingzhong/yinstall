package mssql_mirror

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepMirrorPreflight() *runner.Step {
	return &runner.Step{
		Name:        "Mirror Preflight",
		Description: "Validate multi-node targets and sqlcmd for database mirroring",
		Tags:        []string{"mssql-ha", "mirror", "preflight"},
		PreCheck: func(ctx *runner.StepContext) error {
			replicas := commonmssql.ReplicaHosts(ctx)
			if len(replicas) == 0 && len(ctx.HostsToRun()) < 2 {
				return runner.NewStepSkippedError("mirror preflight requires primary + replica (2+ hosts)")
			}
			res, _ := ctx.Execute(`powershell -NoProfile -Command "(Get-WmiObject Win32_ComputerSystem).PartOfDomain"`, false)
			if res != nil && strings.Contains(strings.ToLower(res.GetStdout()), "false") {
				ctx.Logger.Info("M-005: workgroup mode (certificate mirroring)")
			}
			if err := commonmssql.RunSqlcmdQueries(ctx, "M-005 sqlcmd ping", []string{"SELECT 1 AS ok;"}); err != nil {
				return err
			}
			if err := collectMirrorInstanceInfo(ctx); err != nil {
				return err
			}
			if commonmssql.IsPrimaryHost(ctx) {
				if err := discoverMirrorTargetDBs(ctx); err != nil {
					return err
				}
				if err := collectMirrorDBStatuses(ctx); err != nil {
					return err
				}
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			mshLogPhase(ctx, "plan", "M-005 mirror preflight")
			if err := commonmssql.RunSqlcmdQueries(ctx, "M-005 sqlcmd ping", []string{"SELECT 1 AS ok;"}); err != nil {
				return err
			}
			if err := collectMirrorInstanceInfo(ctx); err != nil {
				return err
			}
			if err := discoverMirrorTargetDBs(ctx); err != nil {
				return err
			}
			if err := collectMirrorDBStatuses(ctx); err != nil {
				return err
			}
			if agActive, err := commonmssql.AnyAGDatabaseReplicaActive(ctx); err != nil {
				return err
			} else if agActive {
				ctx.Logger.Warn("M-005: AG database replica active on this instance; avoid --mssql-force-ha-certs -f M-008 here (use separate instance for mirror or remove AG first)")
			}
			port := commonmssql.MirrorEndpointPort(ctx)
			cmd := fmt.Sprintf(`powershell -NoProfile -Command "Test-NetConnection -ComputerName 127.0.0.1 -Port %d -WarningAction SilentlyContinue | Select-Object -ExpandProperty TcpTestSucceeded"`, port)
			res, _ := ctx.Execute(cmd, false)
			if res != nil && strings.Contains(strings.ToLower(res.GetStdout()), "true") {
				mshLogPhase(ctx, "preflight-port", fmt.Sprintf("port %d reachable locally", port))
			}
			return nil
		},
	}
}
