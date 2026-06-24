package mssql

import "testing"

func TestNormalizeDomainMode(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", DomainModeAuto},
		{"auto", DomainModeAuto},
		{"domain", DomainModeDomain},
		{"ad", DomainModeDomain},
		{"workgroup", DomainModeWorkgroup},
		{"wg", DomainModeWorkgroup},
	}
	for _, tt := range tests {
		if got := NormalizeDomainMode(tt.in); got != tt.want {
			t.Errorf("NormalizeDomainMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDomainModeFromParams(t *testing.T) {
	params := map[string]interface{}{
		"mssql_domain_mode": "workgroup",
	}
	if got := DomainModeFromParams(params); got != DomainModeWorkgroup {
		t.Fatalf("DomainModeFromParams() = %q, want %q", got, DomainModeWorkgroup)
	}
	if got := DomainModeFromParams(nil); got != DomainModeWorkgroup {
		t.Fatalf("DomainModeFromParams(nil) = %q, want %q", got, DomainModeWorkgroup)
	}
}

func TestDeriveSpnMode(t *testing.T) {
	tests := []struct {
		domain, topology, want string
	}{
		{"workgroup", "standalone", "skip"},
		{"domain", "standalone", "verify"},
		{"domain", string(TopologyAGWSFC), "register"},
		{"auto", "standalone", "verify"},
	}
	for _, tt := range tests {
		if got := DeriveSpnMode(tt.domain, tt.topology); got != tt.want {
			t.Errorf("DeriveSpnMode(%q, %q) = %q, want %q", tt.domain, tt.topology, got, tt.want)
		}
	}
}
