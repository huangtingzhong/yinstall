package win_os

import (
	"strings"

	"github.com/yinstall/internal/runner"
)

// Profile identifies Windows OS baseline product configuration.
type Profile struct {
	Name                     string
	HostnameDefaultPrefix    string
	FirewallPorts            string
	EnablePagefileLPIM       bool
	EnableServiceAccountPrep bool
	EnablePowerPlan          bool
	EnableAvExclusions       bool
	SpnMode                  string // skip|verify|register
	Prereq                   func(ctx *runner.StepContext) error
	VerifyExtra              func(ctx *runner.StepContext) error
}

// BaseProfile returns defaults for a product name.
func BaseProfile(name, hostnamePrefix string) Profile {
	return Profile{
		Name:                  name,
		HostnameDefaultPrefix: hostnamePrefix,
		SpnMode:               "verify",
	}
}

// ProfileMssql default MSSQL Windows OS profile.
func ProfileMssql() Profile {
	p := BaseProfile("mssql", "")
	p.EnablePowerPlan = true
	p.EnablePagefileLPIM = true
	p.EnableAvExclusions = true
	return p
}

// ProfileMySQL default MySQL Windows OS profile.
func ProfileMySQL() Profile {
	p := BaseProfile("mysql", "")
	p.EnablePowerPlan = false
	p.SpnMode = "skip"
	return p
}

// FilterSteps removes profile-disabled optional steps from the list.
func FilterSteps(steps []*runner.Step, profile Profile) []*runner.Step {
	var out []*runner.Step
	for _, s := range steps {
		if s == nil {
			continue
		}
		switch s.ID {
		case "W-013":
			if !profile.EnablePowerPlan {
				continue
			}
		case "W-014":
			if profile.SpnMode == "skip" {
				continue
			}
		case "W-007":
			if !profile.EnablePagefileLPIM {
				continue
			}
		case "W-008":
			if !profile.EnableServiceAccountPrep {
				continue
			}
		case "W-010":
			if !profile.EnableAvExclusions {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// ApplyParams merges CLI params into profile behavior.
func ApplyParams(p Profile, params map[string]interface{}) Profile {
	if v, ok := params["os_power_plan"].(string); ok && strings.EqualFold(v, "skip") {
		p.EnablePowerPlan = false
	}
	if v, ok := params["os_spn_mode"].(string); ok && v != "" {
		p.SpnMode = v
	}
	if prefix, ok := params["os_hostname_default_prefix"].(string); ok && prefix != "" {
		p.HostnameDefaultPrefix = prefix
	}
	if ports, ok := params["os_firewall_ports"].(string); ok && ports != "" {
		p.FirewallPorts = ports
	}
	return p
}
