package db_test

import (
	"testing"

	"github.com/yinstall/internal/steps/db"
)

func TestNormalizeYACVIPHost(t *testing.T) {
	if got := db.NormalizeYACVIPHost("10.10.10.184/24"); got != "10.10.10.184" {
		t.Fatalf("got %q", got)
	}
	if got := db.NormalizeYACVIPHost(" 10.10.10.185 "); got != "10.10.10.185" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatVIPListForCeGen(t *testing.T) {
	got := db.FormatVIPListForCeGen(
		[]string{"10.10.10.184/24", "10.10.10.185"},
		"10.10.10.0/24",
		"10.10.234.0/24",
	)
	want := "10.10.10.184/24,10.10.10.185/24"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestValidateYACVIPList(t *testing.T) {
	if err := db.ValidateYACVIPList([]string{"10.10.10.184/24", "10.10.10.185"}, 2); err != nil {
		t.Fatal(err)
	}
	if err := db.ValidateYACVIPList([]string{"10.10.10.184"}, 2); err == nil {
		t.Fatal("want count mismatch")
	}
	if err := db.ValidateYACVIPList([]string{"not-an-ip"}, 1); err == nil {
		t.Fatal("want invalid ip")
	}
}
