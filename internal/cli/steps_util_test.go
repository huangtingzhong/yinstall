package cli

import (
	"testing"

	"github.com/yinstall/internal/runner"
	mssqlsteps "github.com/yinstall/internal/steps/mssql"
	ossteps "github.com/yinstall/internal/steps/os"
)

func TestEnsureConnectivityStepPrependsB001(t *testing.T) {
	all := []*runner.Step{
		ossteps.StepB001CheckConnectivity(),
		mssqlsteps.StepMS004ResolveSetupMedia(),
	}
	filtered := []*runner.Step{mssqlsteps.StepMS004ResolveSetupMedia()}
	out := ensureConnectivityStep(all, filtered)
	if len(out) != 2 || out[0].ID != "B-001" {
		t.Fatalf("expected B-001 prepended, got ids %v", stepIDs(out))
	}
}

func TestMssqlInstallRequiresSAPassword(t *testing.T) {
	wOnly := []*runner.Step{
		ossteps.StepB001CheckConnectivity(),
		{ID: "W-001"},
		{ID: "W-012"},
	}
	if mssqlInstallRequiresSAPassword(wOnly) {
		t.Fatal("W-* only should not require SA password")
	}
	msOnly := []*runner.Step{
		ossteps.StepB001CheckConnectivity(),
		mssqlsteps.StepMS001PlatformTransportDetect(),
	}
	if mssqlInstallRequiresSAPassword(msOnly) {
		t.Fatal("MS-001 only should not require SA password")
	}
	withInstall := []*runner.Step{mssqlsteps.StepMS004ResolveSetupMedia()}
	if !mssqlInstallRequiresSAPassword(withInstall) {
		t.Fatal("MS-002+ should require SA password")
	}
}

func stepIDs(steps []*runner.Step) []string {
	var ids []string
	for _, s := range steps {
		if s != nil {
			ids = append(ids, s.ID)
		}
	}
	return ids
}
