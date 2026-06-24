package win_os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// SpnMode reads os_spn_mode.
func SpnMode(ctx *runner.StepContext) string {
	return strings.ToLower(strings.TrimSpace(ctx.GetParamString("os_spn_mode", "verify")))
}

// ShouldRunSPN returns false for workgroup or skip mode.
func ShouldRunSPN(ctx *runner.StepContext) bool {
	if SpnMode(ctx) == "skip" {
		return false
	}
	if v := ctx.GetParamString("os_domain_mode", "auto"); v == "workgroup" {
		return false
	}
	if joined, ok := ctx.Results["domain_joined"]; ok {
		if b, ok := joined.(bool); ok && !b {
			return false
		}
		if b, ok := joined.(bool); ok && b {
			return true
		}
	}
	// auto without W-001 probe: treat as workgroup (safe for -s partial runs).
	if strings.ToLower(strings.TrimSpace(ctx.GetParamString("os_domain_mode", "auto"))) == "auto" {
		return false
	}
	return true
}

// VerifySPN checks MSSQLSvc SPNs via setspn -L on service account.
func VerifySPN(ctx *runner.StepContext, serviceAccount, fqdn, port, instance string) (missing []string, err error) {
	if fqdn == "" || serviceAccount == "" {
		return nil, fmt.Errorf("fqdn and service account required for SPN verify")
	}
	var expected []string
	if instance == "" || strings.EqualFold(instance, "MSSQLSERVER") {
		expected = append(expected, fmt.Sprintf("MSSQLSvc/%s:%s", fqdn, port))
	} else {
		expected = append(expected, fmt.Sprintf("MSSQLSvc/%s:%s", fqdn, port))
		expected = append(expected, fmt.Sprintf("MSSQLSvc/%s:%s", fqdn, instance))
	}

	listCmd := fmt.Sprintf(`setspn -L %s`, serviceAccount)
	ctx.LogScriptPreview("powershell", "W-014 setspn -L", listCmd)
	res, err := ctx.Execute(listCmd, false)
	if err != nil {
		return expected, err
	}
	out := strings.ToUpper(res.GetStdout())
	for _, spn := range expected {
		if !strings.Contains(out, strings.ToUpper(spn)) {
			missing = append(missing, spn)
		}
	}
	return missing, nil
}

// RegisterSPN registers missing SPNs with setspn -S.
func RegisterSPN(ctx *runner.StepContext, serviceAccount string, spns []string) ([]string, error) {
	var registered []string
	for _, spn := range spns {
		cmd := fmt.Sprintf(`setspn -S %s %s`, spn, serviceAccount)
		ctx.LogScriptPreview("powershell", "W-014 register SPN", cmd)
		if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
			return registered, err
		}
		registered = append(registered, spn)
	}
	return registered, nil
}
