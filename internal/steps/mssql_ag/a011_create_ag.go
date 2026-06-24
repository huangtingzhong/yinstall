package mssql_ag

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepA011AGCreate() *runner.Step {
	return &runner.Step{
		ID:          "A-011",
		Name:        "Create Availability Group",
		Description: "Create AG on primary, or add new replicas to an existing AG",
		Tags:        []string{"mssql-ha", "ag"},
		PreCheck: func(ctx *runner.StepContext) error {
			if !commonmssql.IsPrimaryHost(ctx) {
				return runner.NewStepSkippedError("A-011 runs on primary only")
			}
			if err := requireHadrEnabledWmi(ctx, "A-011"); err != nil {
				return err
			}
			if err := validateExpectedReplicaServerNames(ctx); err != nil {
				return err
			}
			if err := validateAGReplicaSetMatchesExpected(ctx); err != nil {
				return err
			}
			// AG already contains every expected (-t) replica → nothing to do.
			if skip, reason, err := agAllExpectedReplicasPresent(ctx); err != nil {
				return err
			} else if skip {
				return runner.NewStepSkippedError("A-011: " + reason)
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			ag := commonmssql.AGName(ctx)
			primary := commonmssql.ResolvePrimaryHost(ctx)
			replicas := commonmssql.ReplicaHosts(ctx)
			sqlMajor, err := commonmssql.ResolveSQLMajor(ctx)
			if err != nil {
				return err
			}
			automatic := commonmssql.AGSeedingMode(ctx) == "automatic"

			// Query existing AG replicas (empty ⇒ AG does not exist yet).
			existing := []string{}
			if !ctx.DryRun {
				stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "A-011 existing replicas", commonmssql.AGReplicaServerNamesSQL(ag))
				if err != nil {
					return err
				}
				existing = commonmssql.ParseAGReplicaServerNames(stdout)
			}

			if len(existing) == 0 {
				// AG does not exist → CREATE with all replicas.
				var specs []commonmssql.AGReplicaSpec
				specs = append(specs, applySeeding(commonmssql.AGReplicaSpecForHost(ctx, primary, true), automatic))
				for _, r := range replicas {
					specs = append(specs, applySeeding(commonmssql.AGReplicaSpecForHost(ctx, r, false), automatic))
				}
				sql := commonmssql.CreateAvailabilityGroupSQL(ag, specs, sqlMajor)
				return commonmssql.RunSqlcmdQueries(ctx, "A-011 create AG", []string{sql})
			}

			// AG exists → ADD REPLICA only for missing -t nodes.
			var missing []commonmssql.AGReplicaSpec
			var missingNames []string
			for _, r := range replicas {
				spec := applySeeding(commonmssql.AGReplicaSpecForHost(ctx, r, false), automatic)
				if !replicaNameInList(existing, spec.ServerName) {
					missing = append(missing, spec)
					missingNames = append(missingNames, spec.ServerName)
				}
			}
			if len(missing) == 0 {
				ctx.Logger.Info("A-011: AG %s already has all expected replicas", ag)
				return nil
			}
			mshLogPhase(ctx, "ag-add-replica-start", strings.Join(missingNames, ","))
			ctx.Logger.Info("A-011: adding replicas to existing AG %s: %s", ag, strings.Join(missingNames, ", "))
			sqls := commonmssql.AlterAvailabilityGroupAddReplicaSQL(ag, missing, sqlMajor)
			if err := commonmssql.RunSqlcmdQueries(ctx, "A-011 add replica", sqls); err != nil {
				return err
			}
			mshLogPhase(ctx, "ag-add-replica-done", strings.Join(missingNames, ","))
			return nil
		},
	}
}

// agAllExpectedReplicasPresent reports whether the AG already exists AND every
// host in -t is already a replica. Returns (skip, reason, error); skip=true means
// A-011 should be skipped entirely.
func agAllExpectedReplicasPresent(ctx *runner.StepContext) (bool, string, error) {
	if ctx.DryRun {
		return false, "", nil
	}
	ag := commonmssql.AGName(ctx)
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "A-011 existing replicas", commonmssql.AGReplicaServerNamesSQL(ag))
	if err != nil {
		return false, "", err
	}
	existing := commonmssql.ParseAGReplicaServerNames(stdout)
	if len(existing) == 0 {
		return false, "", nil // AG absent → CREATE path
	}
	for _, r := range commonmssql.ReplicaHosts(ctx) {
		if !replicaNameInList(existing, commonmssql.HAReplicaServerName(ctx, r)) {
			return false, "", nil // missing node → ADD REPLICA path
		}
	}
	return true, fmt.Sprintf("AG %s already contains all expected replicas (%s)", ag, strings.Join(existing, ", ")), nil
}

func replicaNameInList(list []string, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, n := range list {
		if strings.EqualFold(strings.TrimSpace(n), name) {
			return true
		}
	}
	return false
}

func applySeeding(spec commonmssql.AGReplicaSpec, automatic bool) commonmssql.AGReplicaSpec {
	if automatic {
		spec.SeedingMode = "AUTOMATIC"
	}
	return spec
}

// validateAGReplicaSetMatchesExpected fails when an existing AG contains replicas
// outside the topology implied by --primary-host / -t / per-host instance params.
// Skipped in add-node mode (--primary-host set, not rebuild): -t lists only new
// replica(s); existing AG members (e.g. 186) are expected to remain.
func validateAGReplicaSetMatchesExpected(ctx *runner.StepContext) error {
	if ctx.DryRun || !commonmssql.IsPrimaryHost(ctx) {
		return nil
	}
	if !ctx.GetParamBool("mssql_rebuild_mode", false) {
		return nil
	}
	ag := commonmssql.AGName(ctx)
	stdout, err := commonmssql.QuerySqlcmdScalar(ctx, "A-011 validate replicas", commonmssql.AGReplicaServerNamesSQL(ag))
	if err != nil {
		return err
	}
	existing := commonmssql.ParseAGReplicaServerNames(stdout)
	if len(existing) == 0 {
		return nil
	}
	expected := make(map[string]struct{})
	primary := commonmssql.ResolvePrimaryHost(ctx)
	if name := strings.TrimSpace(commonmssql.HAReplicaServerName(ctx, primary)); name != "" {
		expected[strings.ToLower(name)] = struct{}{}
	}
	for _, r := range commonmssql.ReplicaHosts(ctx) {
		if name := strings.TrimSpace(commonmssql.HAReplicaServerName(ctx, r)); name != "" {
			expected[strings.ToLower(name)] = struct{}{}
		}
	}
	var unexpected []string
	for _, name := range existing {
		if _, ok := expected[strings.ToLower(strings.TrimSpace(name))]; !ok {
			unexpected = append(unexpected, name)
		}
	}
	if len(unexpected) == 0 {
		return nil
	}
	return fmt.Errorf("A-011: AG %s has unexpected replica(s) %s; run mssql ag remove before rebuilding with current --primary/replica-mssql-instance flags",
		ag, strings.Join(unexpected, ", "))
}

// validateExpectedReplicaServerNames ensures @@SERVERNAME is known for each topology host before CREATE/ADD REPLICA.
func validateExpectedReplicaServerNames(ctx *runner.StepContext) error {
	if ctx.DryRun || !commonmssql.IsPrimaryHost(ctx) {
		return nil
	}
	primary := commonmssql.ResolvePrimaryHost(ctx)
	var missing []string
	for _, h := range append([]string{primary}, commonmssql.ReplicaHosts(ctx)...) {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if name := strings.TrimSpace(commonmssql.HAReplicaServerName(ctx, h)); name == "" {
			missing = append(missing, h)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("A-011: cannot resolve @@SERVERNAME for host(s) %s; ensure A-005 ran on each node or use hostname in -t/--primary-host",
		strings.Join(missing, ", "))
}
