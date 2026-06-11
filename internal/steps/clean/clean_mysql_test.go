package clean

import "testing"

func TestMysqlWindowsCollectAllPIDs(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		wantAll  bool
	}{
		{"kill all bare", []string{"mysqld.exe"}, true},
		{"kill all empty", nil, true},
		{"instance port", []string{"--port=4066", "mysqld.exe"}, false},
		{"instance datadir", []string{"--datadir=/data/4066", "mysqld.exe"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mysqlWindowsCollectAllPIDs(tt.patterns); got != tt.wantAll {
				t.Fatalf("patterns=%v got=%v want=%v", tt.patterns, got, tt.wantAll)
			}
		})
	}
}
