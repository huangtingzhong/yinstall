package mssql

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// SqlWmiInstanceName returns the SQL WMI service key (MSSQLSERVER or instance name).
func SqlWmiInstanceName(instance string) string {
	inst := strings.TrimSpace(instance)
	if inst == "" || strings.EqualFold(inst, DefaultInstance) {
		return DefaultInstance
	}
	return inst
}

// SqlEngineAndAgentServiceNames returns Windows service names for Engine and Agent.
func SqlEngineAndAgentServiceNames(instance string) (engine, agent string) {
	inst := SqlWmiInstanceName(instance)
	if inst == DefaultInstance {
		return "MSSQLSERVER", "SQLSERVERAGENT"
	}
	return "MSSQL$" + inst, "SQLAgent$" + inst
}

// sqlWmiBootstrapPS mirrors dbatools Invoke-ManagedComputerCommand setup:
// LoadWithPartialName('Microsoft.SqlServer.SqlWmiManagement'), ManagedComputer($ComputerName), Initialize().
const sqlWmiBootstrapPS = `
function Import-SqlWmiManagement {
  try {
    $null = [System.Reflection.Assembly]::LoadWithPartialName('Microsoft.SqlServer.SqlWmiManagement')
    if ([Microsoft.SqlServer.Management.Smo.Wmi.ManagedComputer]) { return }
  } catch {}
  $gacRoots = @(
    (Join-Path $env:windir 'Microsoft.NET\assembly\GAC_MSIL\Microsoft.SqlServer.SqlWmiManagement'),
    (Join-Path $env:windir 'assembly\GAC_MSIL\Microsoft.SqlServer.SqlWmiManagement')
  )
  foreach ($root in $gacRoots) {
    if (-not (Test-Path -LiteralPath $root)) { continue }
    $dll = Get-ChildItem -LiteralPath $root -Recurse -Filter 'Microsoft.SqlServer.SqlWmiManagement.dll' -ErrorAction SilentlyContinue |
      Sort-Object FullName -Descending | Select-Object -First 1
    if ($dll) {
      Add-Type -Path $dll.FullName -ErrorAction Stop
      return
    }
  }
  $pfPatterns = @(
    (Join-Path $env:ProgramFiles 'Microsoft SQL Server\*\Shared\Microsoft.SqlServer.SqlWmiManagement.dll'),
    (Join-Path ${env:ProgramFiles(x86)} 'Microsoft SQL Server\*\Shared\Microsoft.SqlServer.SqlWmiManagement.dll')
  )
  foreach ($pattern in $pfPatterns) {
    $dll = Get-Item -Path $pattern -ErrorAction SilentlyContinue | Sort-Object FullName -Descending | Select-Object -First 1
    if ($dll) {
      Add-Type -Path $dll.FullName -ErrorAction Stop
      return
    }
  }
  throw 'Microsoft.SqlServer.SqlWmiManagement not found (install SQL Server client tools or run on SQL host)'
}
function Get-SqlWmiManagedComputer {
  param([string]$ComputerName = $env:COMPUTERNAME)
  Import-SqlWmiManagement
  $mc = New-Object Microsoft.SqlServer.Management.Smo.Wmi.ManagedComputer $ComputerName
  $null = $mc.Initialize()
  return $mc
}
function Get-SqlWmiService {
  param([Parameter(Mandatory)][string]$InstanceName)
  $mc = Get-SqlWmiManagedComputer
  $svc = $mc.Services[$InstanceName]
  if (-not $svc) {
    $display = "SQL Server ($InstanceName)"
    $svc = $mc.Services | Where-Object { $_.DisplayName -eq $display } | Select-Object -First 1
  }
  if (-not $svc) { throw "SQL WMI service not found for instance $InstanceName" }
  return $svc
}
`

// GetHadrEnabledWmiPS returns a script that writes 1 or 0 (dbatools Get-WmiHadr).
func GetHadrEnabledWmiPS(instance string) string {
	inst := strings.ReplaceAll(SqlWmiInstanceName(instance), "'", "''")
	return sqlWmiBootstrapPS + fmt.Sprintf(`
$instName = '%s'
$svc = Get-SqlWmiService -InstanceName $instName
if ($null -eq $svc.IsHadrEnabled -or -not $svc.IsHadrEnabled) { '0' } else { '1' }
`, inst)
}

// EnableDbaAgHadrForcePS enables HADR via WMI ChangeHadrServiceSetting(1) and restarts
// Database Engine + SQL Agent (equivalent to Enable-DbaAgHadr -Force).
func EnableDbaAgHadrForcePS(instance string) string {
	inst := strings.ReplaceAll(SqlWmiInstanceName(instance), "'", "''")
	engine, agent := SqlEngineAndAgentServiceNames(instance)
	engine = strings.ReplaceAll(engine, "'", "''")
	agent = strings.ReplaceAll(agent, "'", "''")
	return sqlWmiBootstrapPS + fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$instName = '%s'
$engineSvc = '%s'
$agentSvc = '%s'
$svc = Get-SqlWmiService -InstanceName $instName
$before = $svc.IsHadrEnabled
$svc.ChangeHadrServiceSetting(1)
foreach ($name in @($engineSvc, $agentSvc)) {
  $s = Get-Service -Name $name -ErrorAction SilentlyContinue
  if ($s -and $s.Status -eq 'Running') {
    Stop-Service -Name $name -Force -ErrorAction Stop
  }
}
Start-Service -Name $engineSvc -ErrorAction Stop
$agent = Get-Service -Name $agentSvc -ErrorAction SilentlyContinue
if ($agent) { Start-Service -Name $agentSvc -ErrorAction SilentlyContinue }
Start-Sleep -Seconds 3
$after = (Get-SqlWmiService -InstanceName $instName).IsHadrEnabled
if (-not $after) {
  throw "HADR still disabled after ChangeHadrServiceSetting(1) and service restart (was=$before)"
}
'1'
`, inst, engine, agent)
}

// HadrEnabledFromWmi queries IsHadrEnabled via SQL WMI (dbatools Get-WmiHadr).
func HadrEnabledFromWmi(ctx *runner.StepContext, instance string) (bool, error) {
	if ctx == nil {
		return false, nil
	}
	if ctx.DryRun {
		return false, nil
	}
	out, err := RunHAPowerShellScalar(ctx, "HADR WMI IsHadrEnabled", GetHadrEnabledWmiPS(instance))
	if err != nil {
		return false, err
	}
	return ParsePowerShellBoolScalar(out), nil
}

// EnableDbaAgHadr runs Enable-DbaAgHadr -Force equivalent on the target host.
func EnableDbaAgHadr(ctx *runner.StepContext, instance string) error {
	if ctx == nil {
		return nil
	}
	out, err := RunHAPowerShellScalar(ctx, "MSH-002 Enable-DbaAgHadr -Force", EnableDbaAgHadrForcePS(instance))
	if err != nil {
		return err
	}
	if !ParsePowerShellBoolScalar(out) {
		return fmt.Errorf("HADR enable script did not confirm success (output=%q)", strings.TrimSpace(out))
	}
	return nil
}

// ParsePowerShellBoolScalar parses 1/0 or True/False from PowerShell stdout.
func ParsePowerShellBoolScalar(stdout string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if line == "1" || lower == "true" {
			return true
		}
		if line == "0" || lower == "false" {
			return false
		}
	}
	return false
}
