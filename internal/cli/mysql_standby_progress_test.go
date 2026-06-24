package cli

import (
	"testing"

	commonmysql "github.com/yinstall/internal/common/mysql"
	mysqlstandby "github.com/yinstall/internal/steps/mysql_standby"
)

func TestMysqlStandbyProgressPlan(t *testing.T) {
	all := mysqlstandby.GetAllSteps()

	t.Run("all_clone", func(t *testing.T) {
		plan := buildMysqlStandbyExecPlan(all, commonmysql.StageAll, "clone", false)
		got := countMysqlStandbyProgressSteps(plan)
		if got != 13 {
			t.Fatalf("required count = %d, want 13", got)
		}
		opt := 0
		for _, e := range plan {
			if e.optional {
				opt++
			}
		}
		if opt != 4 {
			t.Fatalf("optional slots = %d, want 4 (MR-018,005,010,011)", opt)
		}
	})

	t.Run("software_only", func(t *testing.T) {
		plan := buildMysqlStandbyExecPlan(all, commonmysql.StageSoftware, "clone", false)
		if countMysqlStandbyProgressSteps(plan) != 4 {
			t.Fatalf("required count = %d, want 4", countMysqlStandbyProgressSteps(plan))
		}
	})

	t.Run("instance_dump", func(t *testing.T) {
		plan := buildMysqlStandbyExecPlan(all, commonmysql.StageInstance, "dump", false)
		if countMysqlStandbyProgressSteps(plan) != 13 {
			t.Fatalf("required count = %d, want 13", countMysqlStandbyProgressSteps(plan))
		}
		for _, e := range plan {
			if e.stepID == "MR-005" || e.stepID == "MR-010" {
				t.Fatalf("clone-only step %s should not be in dump plan", e.stepID)
			}
		}
	})

	t.Run("semi_sync", func(t *testing.T) {
		allSemi := append(all, mysqlstandby.SemiSyncSteps()...)
		plan := buildMysqlStandbyExecPlan(allSemi, commonmysql.StageAll, "clone", true)
		seen := 0
		for _, e := range plan {
			if e.stepID == "MR-016" {
				seen++
			}
		}
		if seen != 2 {
			t.Fatalf("MR-016 slots = %d, want 2 (primary+replica)", seen)
		}
	})

	t.Run("exclude_reasons", func(t *testing.T) {
		if r := mysqlStandbyExcludeReason("MR-017", commonmysql.StageAll, "clone", false); r == "" {
			t.Fatal("MR-017 should have exclude reason")
		}
		if r := mysqlStandbyExcludeReason("MR-005", commonmysql.StageAll, "dump", false); r == "" {
			t.Fatal("MR-005 should be excluded for dump")
		}
		if r := mysqlStandbyExcludeReason("MR-018", commonmysql.StageInstance, "clone", false); r == "" {
			t.Fatal("MR-018 should be excluded for instance stage")
		}
	})
}
