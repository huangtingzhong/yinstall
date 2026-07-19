package runner_test

import (
	"testing"

	"github.com/yinstall/internal/runner"
)

func TestBuildSteps_sequential(t *testing.T) {
	steps := runner.BuildSteps(runner.StepSpec{
		Prefix: "C",
		Entries: []runner.StepEntry{
			{New: func() *runner.Step { return &runner.Step{Name: "a"} }},
			{New: func() *runner.Step { return &runner.Step{Name: "b"} }},
		},
	})
	if len(steps) != 2 {
		t.Fatalf("len=%d", len(steps))
	}
	if steps[0].ID != "C-001" || steps[1].ID != "C-002" {
		t.Fatalf("ids: %q %q", steps[0].ID, steps[1].ID)
	}
}

func TestBuildSteps_fixedID(t *testing.T) {
	steps := runner.BuildSteps(runner.StepSpec{
		Prefix: "CLEAN-DB",
		Entries: []runner.StepEntry{
			{New: func() *runner.Step { return &runner.Step{Name: "a"} }},
			{FixedID: "CLEAN-DB", New: func() *runner.Step { return &runner.Step{Name: "agg"} }},
			{New: func() *runner.Step { return &runner.Step{Name: "b"} }},
		},
	})
	if len(steps) != 3 {
		t.Fatalf("len=%d", len(steps))
	}
	if steps[0].ID != "CLEAN-DB-001" {
		t.Fatalf("0: %q", steps[0].ID)
	}
	if steps[1].ID != "CLEAN-DB" {
		t.Fatalf("1: %q", steps[1].ID)
	}
	if steps[2].ID != "CLEAN-DB-002" {
		t.Fatalf("2: %q", steps[2].ID)
	}
}

func TestStepMsg(t *testing.T) {
	ctx := &runner.StepContext{CurrentStepID: "C-015"}
	got := runner.StepMsg(ctx, "C-035: Set DB Timezone")
	want := "C-015: Set DB Timezone"
	if got != want {
		t.Fatalf("StepMsg = %q, want %q", got, want)
	}
}

func TestFirstStepID(t *testing.T) {
	steps := runner.BuildSteps(runner.StepSpec{
		Prefix: "B",
		Entries: []runner.StepEntry{
			{New: func() *runner.Step { return &runner.Step{Name: "x"} }},
		},
	})
	if runner.FirstStepID(steps, "B") != "B-001" {
		t.Fatalf("FirstStepID = %q", runner.FirstStepID(steps, "B"))
	}
}

func TestCloneStep(t *testing.T) {
	orig := &runner.Step{ID: "B-001", Name: "x", Tags: []string{"os"}}
	cp := runner.CloneStep(orig)
	if cp == orig {
		t.Fatal("same pointer")
	}
	cp.ID = "R-001"
	if orig.ID != "B-001" {
		t.Fatal("mutated orig")
	}
	cp.Tags[0] = "db"
	if orig.Tags[0] != "os" {
		t.Fatal("shared tags slice")
	}
}
