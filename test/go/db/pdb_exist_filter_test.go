package db_test

import (
	"testing"

	commonsql "github.com/yinstall/internal/common/sql"
	dbsteps "github.com/yinstall/internal/steps/db"
)

func TestParseYasqlOutput_PDBStatusRows(t *testing.T) {
	out := `
NAME                                                             STATUS
---------------------------------------------------------------- -----------------
PDB1                                                             OPEN
PDB2                                                             MOUNTED

2 rows fetched.
`
	m := commonsql.ParseYasqlOutput(out)
	if m["PDB1"] != "OPEN" {
		t.Fatalf("PDB1=%q want OPEN; map=%v", m["PDB1"], m)
	}
	if m["PDB2"] != "MOUNTED" {
		t.Fatalf("PDB2=%q want MOUNTED", m["PDB2"])
	}
	// PDBNamesNeedingOpen: OPEN skipped, MOUNTED needs open
	need := dbsteps.PDBNamesNeedingOpen([]string{"PDB1", "PDB2"}, m)
	if len(need) != 1 || need[0] != "PDB2" {
		t.Fatalf("need=%v want [PDB2]", need)
	}
}
