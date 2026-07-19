package mssql

import (
	"fmt"
	"strings"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepConfigureTcpPort() *runner.Step {
	return &runner.Step{
		Name:     "Configure TCP Port",
		Tags:     []string{"mssql", "mssql-instance"},
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			port := configuredTCPPort(ctx)
			if port == commonmssql.DefaultPort {
				return runner.NewStepSkippedError("default port 1433 from ini")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			port := configuredTCPPort(ctx)
			inst := commonmssql.ResolvedInstanceName(ctx)
			script := fmt.Sprintf(`
$instName = '%s'
$port = '%d'
$names = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\Instance Names\SQL' -ErrorAction Stop
$instId = $names.$instName
if (-not $instId) { throw "instance $instName not found in registry" }
$tcp = "HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\$instId\MSSQLServer\SuperSocketNetLib\Tcp\IPAll"
Set-ItemProperty -Path $tcp -Name TcpPort -Value $port -ErrorAction Stop
Set-ItemProperty -Path $tcp -Name TcpDynamicPorts -Value '' -ErrorAction Stop
$svc = if ($instName -eq 'MSSQLSERVER') { 'MSSQLSERVER' } else { 'MSSQL$' + $instName }
Restart-Service -Name $svc -Force -ErrorAction Stop
`, strings.ReplaceAll(inst, "'", "''"), port)
			if ctx.Precheck {
				ctx.LogScriptPreview("powershell", "MS-009 TCP port", script)
				return nil
			}
			return commonmssql.RunHAPowerShellScript(ctx, "MS-009 TCP port", script)
		},
	}
}

// configuredTCPPort prefers explicit --mssql-port over registry listen port
// so MS-009 can apply the requested static port after setup leaves a dynamic port.
func configuredTCPPort(ctx *runner.StepContext) int {
	if p := commonmssql.PortParamInt(ctx); p > 0 {
		return p
	}
	return commonmssql.ResolvedListenPort(ctx)
}
