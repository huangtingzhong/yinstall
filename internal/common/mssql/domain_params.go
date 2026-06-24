package mssql

import "strings"

const (
	DomainModeAuto      = "auto"
	DomainModeDomain    = "domain"
	DomainModeWorkgroup = "workgroup"
)

// NormalizeDomainMode returns auto, domain, or workgroup.
func NormalizeDomainMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case DomainModeDomain, "ad":
		return DomainModeDomain
	case DomainModeWorkgroup, "wg":
		return DomainModeWorkgroup
	default:
		return DomainModeAuto
	}
}

// DomainModeFromParams reads mssql_domain_mode or os_domain_mode from params.
func DomainModeFromParams(params map[string]interface{}) string {
	if params == nil {
		return DomainModeWorkgroup
	}
	if s, ok := params["mssql_domain_mode"].(string); ok && strings.TrimSpace(s) != "" {
		return NormalizeDomainMode(s)
	}
	if s, ok := params["os_domain_mode"].(string); ok && strings.TrimSpace(s) != "" {
		return NormalizeDomainMode(s)
	}
	return DomainModeWorkgroup
}

// DeriveSpnMode returns os_spn_mode for W-014 (skip|verify|register).
func DeriveSpnMode(domainMode, topology string) string {
	switch NormalizeDomainMode(domainMode) {
	case DomainModeWorkgroup:
		return "skip"
	case DomainModeDomain:
		top := Topology(strings.TrimSpace(topology))
		if top == TopologyAGWSFC {
			return "register"
		}
		return "verify"
	default:
		return "verify"
	}
}
