package mssql_ag

import (
	"fmt"
	"net"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

// stepDropAg removes replica(s) named in -t from the AG.
// Behavior:
//   - If removing the -t targets leaves only the primary replica → DROP AG
//     (single-replica AG provides no HA, so tear it down entirely)
//   - Otherwise → REMOVE REPLICA for each -t target whose server name resolves
//     to one of the existing AG replicas
//
// -t targets are typically IPs; we resolve them to Windows computer names via
// reverse DNS so they can be matched against sys.availability_replicas.
func stepDropAg() *runner.Step {
	return &runner.Step{
		Name:        "Drop Availability Group",
		Description: "REMOVE REPLICA for -t targets, or DROP AG when last secondary is removed",
		Tags:        []string{"mssql-ha", "ag", "remove"},
		Dangerous:   true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("A-052 runs on primary only")
			}
			ag := commonmssql.AGName(ctx)
			exists, err := commonmssql.AGExistsOnPrimary(ctx, ag)
			if err != nil {
				return err
			}
			if !exists {
				return runner.NewStepSkippedError("A-052: availability group " + ag + " not found on primary")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			ag := commonmssql.AGName(ctx)
			replicaHosts := commonmssql.ReplicaHosts(ctx)

			existing, err := currentAGReplicas(ctx, ag)
			if err != nil {
				return err
			}

			// "Last node" rule by count: if removing len(replicaHosts)
			// secondaries leaves only the primary, drop the entire AG.
			// This avoids needing IP→server-name resolution for the common
			// 2-node teardown case.
			nonPrimaryCount := len(existing) - 1 // primary is always a replica
			if nonPrimaryCount <= 0 {
				// AG has only primary → already in degraded state, drop it.
				mshLogPhase(ctx, "drop-ag-start", ag)
				if err := commonmssql.DropAvailabilityGroup(ctx, ag); err != nil {
					return err
				}
				mshLogPhase(ctx, "drop-ag-done", ag)
				ctx.Logger.Info("A-052: dropped AG %s (only primary replica present)", ag)
				return nil
			}
			if len(replicaHosts) >= nonPrimaryCount {
				mshLogPhase(ctx, "drop-ag-start", ag)
				if err := commonmssql.DropAvailabilityGroup(ctx, ag); err != nil {
					return err
				}
				mshLogPhase(ctx, "drop-ag-done", ag)
				ctx.Logger.Info("A-052: dropped AG %s (removing %d replica(s) leaves only primary; existing=%v)",
					ag, len(replicaHosts), existing)
				return nil
			}

			// Multi-replica path: resolve each -t IP to a Windows computer
			// name, then REMOVE REPLICA for those that match an existing replica.
			targetServers, err := resolveReplicaServerNames(ctx, replicaHosts)
			if err != nil {
				return err
			}
			mshLogPhase(ctx, "plan", fmt.Sprintf("A-052: AG %s existing=%v targets(-t)=%v resolved=%v", ag, existing, replicaHosts, targetServers))

			for _, srv := range targetServers {
				if !replicaNameInList(existing, srv) {
					ctx.Logger.Info("A-052: skip REMOVE REPLICA %s (not in AG %s; existing=%v)", srv, ag, existing)
					continue
				}
				mshLogPhase(ctx, "remove-replica-start", srv)
				if err := commonmssql.RunSqlcmdQueries(ctx, "A-052 remove replica "+srv, []string{commonmssql.RemoveReplicaFromAGSQL(ag, srv)}); err != nil {
					return err
				}
				mshLogPhase(ctx, "remove-replica-done", srv)
				ctx.Logger.Info("A-052: removed replica %s from AG %s", srv, ag)
			}
			return nil
		},
	}
}

// resolveReplicaServerNames resolves each host (IP or hostname) to its Windows
// computer name via reverse DNS lookup. Strips domain suffix to match the
// short form used by sys.availability_replicas.replica_server_name.
func resolveReplicaServerNames(ctx *runner.StepContext, hosts []string) ([]string, error) {
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		// Prefer cached result from A-005 preflight.
		if name := commonmssql.HAReplicaServerName(ctx, h); name != "" && name != h {
			out = append(out, shortHost(name))
			continue
		}
		// Fallback: reverse DNS via PowerShell Resolve-DnsName.
		cmd := fmt.Sprintf(`(Resolve-DnsName -Name %s -ErrorAction SilentlyContinue | Select-Object -First 1).NameHost`, commonmssql.PSSingleQuote(h))
		stdout, err := commonmssql.RunHAPowerShellScalar(ctx, "A-052 resolve "+h, cmd)
		if err != nil || strings.TrimSpace(stdout) == "" {
			if net.ParseIP(h) != nil {
				return nil, fmt.Errorf("A-052: cannot resolve Windows server name for replica IP %s (run ag add with A-005 first, or use hostname in -t matching replica_server_name)", h)
			}
			out = append(out, shortHost(h))
			continue
		}
		out = append(out, shortHost(strings.TrimSpace(stdout)))
	}
	return out, nil
}

// shortHost strips DNS suffix: "host.example.com" → "host".
func shortHost(name string) string {
	name = strings.TrimSpace(name)
	if idx := strings.Index(name, "."); idx > 0 {
		return name[:idx]
	}
	return name
}

// currentAGReplicas queries the AG replica_server_name list on the primary.
func currentAGReplicas(ctx *runner.StepContext, ag string) ([]string, error) {
	if ctx.DryRun {
		return nil, nil
	}
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "A-052 existing replicas", commonmssql.AGReplicaServerNamesSQL(ag))
	if err != nil {
		return nil, err
	}
	return commonmssql.ParseAGReplicaServerNames(stdout), nil
}
