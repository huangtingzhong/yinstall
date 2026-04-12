package cli

import (
	"testing"

	"github.com/yinstall/internal/runner"
)

func TestFilterStepsExcludeWinsOverInclude(t *testing.T) {
	all := []*runner.Step{
		{ID: "A-001", Name: "one", Tags: []string{"x"}},
		{ID: "A-002", Name: "two", Tags: []string{"y"}},
		{ID: "A-003", Name: "three", Tags: []string{"z"}},
	}
	flags := GlobalFlags{
		IncludeSteps: []string{"A-001", "A-002", "A-003"},
		ExcludeSteps: []string{"A-002"},
	}
	out := filterSteps(all, flags)
	if len(out) != 2 {
		t.Fatalf("want 2 steps, got %d: %+v", len(out), ids(out))
	}
	if ids(out)[0] != "A-001" || ids(out)[1] != "A-003" {
		t.Fatalf("unexpected order/ids: %+v", ids(out))
	}
}

func TestFilterStepsExcludeOnly(t *testing.T) {
	all := []*runner.Step{
		{ID: "B-001", Name: "a"},
		{ID: "B-002", Name: "b"},
	}
	flags := GlobalFlags{ExcludeSteps: []string{"B-001"}}
	out := filterSteps(all, flags)
	if len(out) != 1 || out[0].ID != "B-002" {
		t.Fatalf("got %+v", ids(out))
	}
}

func TestFilterStepsIncludeOnly(t *testing.T) {
	all := []*runner.Step{
		{ID: "C-001", Name: "a"},
		{ID: "C-002", Name: "b"},
	}
	flags := GlobalFlags{IncludeSteps: []string{"C-002"}}
	out := filterSteps(all, flags)
	if len(out) != 1 || out[0].ID != "C-002" {
		t.Fatalf("got %+v", ids(out))
	}
}

func ids(steps []*runner.Step) []string {
	var s []string
	for _, x := range steps {
		s = append(s, x.ID)
	}
	return s
}
