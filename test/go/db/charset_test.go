package db_test

import (
	"testing"

	dbsteps "github.com/yinstall/internal/steps/db"
)

func TestCanonicalYashanCharacterSet(t *testing.T) {
	t.Parallel()
	ok := []string{"utf8", "UTF8", "gbk", "GB18030", "ascii", "binary", "utf8mb3"}
	for _, s := range ok {
		if _, err := dbsteps.CanonicalYashanCharacterSet(s); err != nil {
			t.Errorf("CanonicalYashanCharacterSet(%q): %v", s, err)
		}
	}
	bad := []string{"latin1", "LATIN1", "utf8mb4", "UTF8MB4", "nope", ""}
	for _, s := range bad {
		if _, err := dbsteps.CanonicalYashanCharacterSet(s); err == nil {
			t.Errorf("CanonicalYashanCharacterSet(%q): want error", s)
		}
	}
}
