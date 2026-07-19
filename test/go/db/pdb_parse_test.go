package db_test

import (
	"testing"

	dbsteps "github.com/yinstall/internal/steps/db"
)

func TestParsePDBSpecsCommaKeyValue(t *testing.T) {
	// 模拟 StringArray：整段 key=value 作为一条 entry（含逗号）
	specs, err := dbsteps.ParsePDBSpecs([]string{
		"name=PDB1,user=pdbadmin,password=PdbAdmin1!,size=512M",
	})
	if err != nil {
		t.Fatalf("ParsePDBSpecs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("len=%d want 1", len(specs))
	}
	if specs[0].Name != "PDB1" {
		t.Fatalf("Name=%q", specs[0].Name)
	}
	if specs[0].AdminUser != "pdbadmin" {
		t.Fatalf("AdminUser=%q", specs[0].AdminUser)
	}
}

func TestParsePDBSpecsStringSliceMistake(t *testing.T) {
	// StringSlice 按逗号切开后的失败形态：应报 name required
	_, err := dbsteps.ParsePDBSpecs([]string{"name=PDB1", "user=pdbadmin", "password=x"})
	if err == nil {
		t.Fatal("expected error when commas split into separate entries")
	}
}

func TestParsePDBSpecsMultiBare(t *testing.T) {
	specs, err := dbsteps.ParsePDBSpecs([]string{"PDB1", "PDB2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("len=%d", len(specs))
	}
}
