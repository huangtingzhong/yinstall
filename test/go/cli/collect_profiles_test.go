package cli_test

import (
	"testing"

	"github.com/yinstall/internal/cli"
	"github.com/yinstall/internal/runner"
	collectsteps "github.com/yinstall/internal/steps/collect"
)

func TestFilterStepsByCategories_hardwareProfile(t *testing.T) {
	all := collectsteps.GetAllSteps()
	cats := cli.ExpandProfile("hardware")
	filtered := cli.FilterStepsByCategories(all, cats)

	names := make(map[string]bool, len(filtered))
	for _, s := range filtered {
		if s != nil {
			names[s.Name] = true
		}
	}

	if !names["Collect host identity"] {
		t.Fatalf("hardware profile should include Collect host identity, got names: %v", names)
	}
	if names["Run collect rules"] {
		t.Fatal("hardware profile should not include Run collect rules (CAT-DBSQL)")
	}
}

func TestStepCategory_byNameNotLegacyID(t *testing.T) {
	all := collectsteps.GetAllSteps()
	var hostIdentity *runner.Step
	for _, s := range all {
		if s != nil && s.Name == "Collect host identity" {
			hostIdentity = s
			break
		}
	}
	if hostIdentity == nil {
		t.Fatal("Collect host identity step not found in registry")
	}
	cat := collectsteps.StepCategory(hostIdentity)
	if cat != collectsteps.CatHW {
		t.Fatalf("StepCategory(Collect host identity) = %q, want %q", cat, collectsteps.CatHW)
	}
}
