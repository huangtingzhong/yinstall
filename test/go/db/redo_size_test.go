package db_test

import (
	"testing"

	dbsteps "github.com/yinstall/internal/steps/db"
)

func TestValidateRedoFileSize(t *testing.T) {
	ok := []string{"96", "96M", "128", "128M", "1G", "100663296B"}
	for _, s := range ok {
		if err := dbsteps.ValidateRedoFileSize(s); err != nil {
			t.Errorf("ValidateRedoFileSize(%q): unexpected err: %v", s, err)
		}
	}
	bad := []string{"64", "64M", "0", "abc", ""}
	for _, s := range bad {
		if err := dbsteps.ValidateRedoFileSize(s); err == nil {
			t.Errorf("ValidateRedoFileSize(%q): want error", s)
		}
	}
}

func TestParseRedoFileSizeBytes(t *testing.T) {
	n, err := dbsteps.ParseRedoFileSizeBytes("64")
	if err != nil {
		t.Fatal(err)
	}
	if n != 64*1024*1024 {
		t.Fatalf("64 as MB: got %d", n)
	}
	n, err = dbsteps.ParseRedoFileSizeBytes("100663296B")
	if err != nil {
		t.Fatal(err)
	}
	if n != 100663296 {
		t.Fatalf("bytes literal: got %d", n)
	}
}

func TestValidateTpccRedoFileSize(t *testing.T) {
	if err := dbsteps.ValidateTpccRedoFileSize("128M"); err != nil {
		t.Fatalf("128M should pass: %v", err)
	}
	if err := dbsteps.ValidateTpccRedoFileSize("96M"); err == nil {
		t.Fatal("96M should fail for tpcc")
	}
}
