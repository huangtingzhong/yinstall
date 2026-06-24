package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// ValidateAutomaticSeedingUNC checks automatic seeding prerequisites.
func ValidateAutomaticSeedingUNC(ctx *runner.StepContext) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	unc := strings.TrimSpace(ctx.GetParamString("mssql_ag_seeding_unc", ""))
	if unc == "" {
		return fmt.Errorf("--mssql-ag-seeding-unc is required when --mssql-ag-seeding-mode=automatic")
	}
	user := strings.TrimSpace(ctx.GetParamString("mssql_unc_user", ""))
	if user == "" {
		user = strings.TrimSpace(ctx.GetParamString("replica_ssh_user", ""))
	}
	if user == "" {
		return fmt.Errorf("--mssql-unc-user or SSH user required for automatic seeding UNC")
	}
	return nil
}
