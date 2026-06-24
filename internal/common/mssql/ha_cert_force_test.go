package mssql

import (
	"testing"

	"github.com/yinstall/internal/runner"
)

func TestShouldDropLocalCertEndpoint(t *testing.T) {
	cases := []struct {
		name      string
		forceHa   bool
		forceStep bool
		want      bool
	}{
		{"both false", false, false, false},
		{"force step only", false, true, false},
		{"force ha only", true, false, false},
		{"both true", true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &runner.StepContext{
				Params: map[string]interface{}{
					"mssql_force_ha_certs": tc.forceHa,
				},
				ForceAll: tc.forceStep,
			}
			if got := ShouldDropLocalCertEndpoint(ctx); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
