package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// PhaseB1Complete checks win_os pre-instance phase results for MS-008 gate.
func PhaseB1Complete(ctx *runner.StepContext) error {
	if ctx.GetParamBool("skip_os", false) {
		return nil
	}
	if v, ok := ctx.Results["win_os_pre_instance_failed"].(bool); ok && v {
		return fmt.Errorf("Windows OS pre-instance phase failed; fix W-* before setup")
	}
	return nil
}

// ListExistingInstances returns instance names from W-009 or registry probe.
func ListExistingInstances(ctx *runner.StepContext) []string {
	if v, ok := ctx.Results["mssql_existing_instances"].([]string); ok {
		return v
	}
	return nil
}

// InstanceConflict checks requested instance against existing.
func InstanceConflict(ctx *runner.StepContext, instance string) error {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		instance = DefaultInstance
	}
	for _, ex := range ListExistingInstances(ctx) {
		if strings.EqualFold(ex, instance) {
			return fmt.Errorf("SQL instance %q already installed", instance)
		}
	}
	return nil
}
