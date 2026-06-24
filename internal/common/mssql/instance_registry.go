package mssql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

const (
	// InstanceAuto selects instance by TCP port via registry reverse lookup.
	InstanceAuto = "auto"

	registryEntryResultKey = "mssql_registry_entry"
)

// RegistryEntryResultKey returns the Results key for a host-specific registry entry.
func RegistryEntryResultKey(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return registryEntryResultKey
	}
	return registryEntryResultKey + ":" + HAHostKey(host)
}

var internalIDMajorRE = regexp.MustCompile(`(?i)^MSSQL(\d+)`)

// InstanceRegistryEntry holds SQL Server instance metadata from registry only.
type InstanceRegistryEntry struct {
	Name         string
	InternalID   string
	TcpPort      int
	DynamicPort  int
	ListenPort   int
	SQLPath      string
	SQLBinRoot   string
	DataRoot     string
	BackupDir    string
	Version      string
	Edition      string
	PatchLevel   string
	ProductMajor int
	ToolsRegKey  string
	SqlcmdPath   string
	ServiceName  string
}

// RegistryEntryFromResults reads a resolved registry entry from step results (legacy global key).
func RegistryEntryFromResults(results map[string]interface{}) (InstanceRegistryEntry, bool) {
	if results == nil {
		return InstanceRegistryEntry{}, false
	}
	v, ok := results[registryEntryResultKey].(InstanceRegistryEntry)
	return v, ok && strings.TrimSpace(v.Name) != ""
}

// RegistryEntryFromContext reads the registry entry for the current executor host.
func RegistryEntryFromContext(ctx *runner.StepContext) (InstanceRegistryEntry, bool) {
	if ctx == nil || ctx.Results == nil {
		return InstanceRegistryEntry{}, false
	}
	if host := TargetHost(ctx); host != "" {
		if v, ok := ctx.Results[RegistryEntryResultKey(host)].(InstanceRegistryEntry); ok && strings.TrimSpace(v.Name) != "" {
			return v, true
		}
	}
	return RegistryEntryFromResults(ctx.Results)
}

// ProductMajorFromInternalID parses MSSQL13.MSSQLSERVER -> 13.
func ProductMajorFromInternalID(internalID string) int {
	m := internalIDMajorRE.FindStringSubmatch(strings.TrimSpace(internalID))
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// ToolsRegKeyFromMajor maps product major 13 -> registry tools folder 130.
func ToolsRegKeyFromMajor(major int) string {
	if major <= 0 {
		return ""
	}
	return strconv.Itoa(major * 10)
}

// ServiceNameForInstance returns Windows service name for an instance.
func ServiceNameForInstance(instance string) string {
	instance = strings.TrimSpace(instance)
	if instance == "" || strings.EqualFold(instance, DefaultInstance) {
		return "MSSQLSERVER"
	}
	return "MSSQL$" + instance
}

// SQLServiceAccountName returns the Windows virtual service account name for
// the SQL Server service of the given instance: "NT SERVICE\MSSQLSERVER" for
// the default instance or "NT SERVICE\MSSQL$<name>" for a named instance.
// Used for icacls ACL grants on cert/backup directories.
func SQLServiceAccountName(instanceName string) string {
	return `NT SERVICE\` + ServiceNameForInstance(instanceName)
}

// EffectiveListenPort returns static TcpPort when set, otherwise DynamicPort.
func EffectiveListenPort(tcpPort, dynamicPort int) int {
	if tcpPort > 0 {
		return tcpPort
	}
	return dynamicPort
}

// JoinSqlcmdPath appends sqlcmd.exe to a ClientSetup directory path.
func JoinSqlcmdPath(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	dir = strings.TrimRight(normalizeWinPath(dir), `\`)
	if strings.HasSuffix(strings.ToLower(dir), `\sqlcmd.exe`) {
		return dir
	}
	return joinWinPath(dir, "sqlcmd.exe")
}

// ParseInstanceRegistryLine parses one pipe-delimited registry row from ListInstanceRegistryPS.
func ParseInstanceRegistryLine(line string) (InstanceRegistryEntry, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return InstanceRegistryEntry{}, fmt.Errorf("empty registry line")
	}
	parts := strings.Split(line, "|")
	if len(parts) < 14 {
		return InstanceRegistryEntry{}, fmt.Errorf("invalid registry line: %q", line)
	}
	tcpPort, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
	dynPort, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
	major, _ := strconv.Atoi(strings.TrimSpace(parts[10]))
	entry := InstanceRegistryEntry{
		Name:         strings.TrimSpace(parts[0]),
		InternalID:   strings.TrimSpace(parts[1]),
		TcpPort:      tcpPort,
		DynamicPort:  dynPort,
		ListenPort:   EffectiveListenPort(tcpPort, dynPort),
		SQLPath:      normalizeWinPath(strings.TrimSpace(parts[4])),
		SQLBinRoot:   normalizeWinPath(strings.TrimSpace(parts[5])),
		DataRoot:     normalizeWinPath(strings.TrimSpace(parts[6])),
		BackupDir:    normalizeWinPath(strings.TrimSpace(parts[7])),
		Version:      strings.TrimSpace(parts[8]),
		Edition:      strings.TrimSpace(parts[9]),
		PatchLevel:   strings.TrimSpace(parts[11]),
		ProductMajor: major,
		ToolsRegKey:  strings.TrimSpace(parts[12]),
		SqlcmdPath:   normalizeWinPath(strings.TrimSpace(parts[13])),
	}
	if entry.ProductMajor == 0 {
		entry.ProductMajor = ProductMajorFromInternalID(entry.InternalID)
	}
	if entry.ToolsRegKey == "" {
		entry.ToolsRegKey = ToolsRegKeyFromMajor(entry.ProductMajor)
	}
	entry.ServiceName = ServiceNameForInstance(entry.Name)
	return entry, nil
}

// ParseInstanceRegistryOutput parses multiline stdout from listInstanceRegistryPS.
func ParseInstanceRegistryOutput(stdout string) ([]InstanceRegistryEntry, error) {
	var out []InstanceRegistryEntry
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry, err := ParseInstanceRegistryLine(line)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

// FindInstanceByPort returns entries whose ListenPort matches port.
func FindInstanceByPort(entries []InstanceRegistryEntry, port int) []InstanceRegistryEntry {
	var out []InstanceRegistryEntry
	for _, e := range entries {
		if e.ListenPort == port {
			out = append(out, e)
		}
	}
	return out
}

// FindInstanceByName returns the entry for instance name (case-insensitive).
func FindInstanceByName(entries []InstanceRegistryEntry, name string) (InstanceRegistryEntry, bool) {
	name = strings.TrimSpace(name)
	for _, e := range entries {
		if strings.EqualFold(e.Name, name) {
			return e, true
		}
	}
	return InstanceRegistryEntry{}, false
}

const listInstanceRegistryPS = `$names=Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\Instance Names\SQL' -EA SilentlyContinue; if (-not $names) { exit 0 }; function Get-SqlcmdFromClientSetup([string]$toolsKey) { if (-not $toolsKey) { return '' }; foreach ($root in @('HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server','HKLM:\SOFTWARE\Wow6432Node\Microsoft\Microsoft SQL Server')) { $cs=Get-ItemProperty (Join-Path $root ($toolsKey+'\Tools\ClientSetup')) -EA SilentlyContinue; if (-not $cs) { continue }; if ($cs.ODBCToolsPath) { return ($cs.ODBCToolsPath.TrimEnd('\')+'\sqlcmd.exe') }; if ($cs.Path) { return ($cs.Path.TrimEnd('\')+'\sqlcmd.exe') } }; return '' }; foreach ($prop in $names.PSObject.Properties) { if ($prop.Name -match '^PS') { continue }; $id=[string]$prop.Value; if (-not $id) { continue }; $setup=Get-ItemProperty ('HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\'+$id+'\Setup') -EA SilentlyContinue; $tcp=Get-ItemProperty ('HKLM:\SOFTWARE\Microsoft\Microsoft SQL Server\'+$id+'\MSSQLServer\SuperSocketNetLib\Tcp\IPAll') -EA SilentlyContinue; $tcpPort=0; $dynPort=0; if ($tcp) { if ($tcp.TcpPort) { [void][int]::TryParse([string]$tcp.TcpPort,[ref]$tcpPort) }; if ($tcp.TcpDynamicPorts) { [void][int]::TryParse([string]$tcp.TcpDynamicPorts,[ref]$dynPort) } }; $major=0; if ($id -match 'MSSQL(\d+)') { $major=[int]$Matches[1] }; $toolsKey= if ($major -gt 0) { [string]($major*10) } else { '' }; $sqlcmd=Get-SqlcmdFromClientSetup $toolsKey; $ver= if ($setup) { [string]$setup.Version } else { '' }; $edition= if ($setup) { [string]$setup.Edition } else { '' }; $patch= if ($setup) { [string]$setup.PatchLevel } else { '' }; $sqlPath= if ($setup) { [string]$setup.SQLPath } else { '' }; $binRoot= if ($setup) { [string]$setup.SQLBinRoot } else { '' }; $dataRoot= if ($setup) { [string]$setup.SQLDataRoot } else { '' }; $backup= if ($setup) { [string]$setup.BackupDirectory } else { '' }; Write-Output ($prop.Name+'|'+$id+'|'+$tcpPort+'|'+$dynPort+'|'+$sqlPath+'|'+$binRoot+'|'+$dataRoot+'|'+$backup+'|'+$ver+'|'+$edition+'|'+$major+'|'+$patch+'|'+$toolsKey+'|'+$sqlcmd) }`

// ListInstanceRegistry enumerates installed SQL instances from registry (single PS call).
func ListInstanceRegistry(ctx *runner.StepContext) ([]InstanceRegistryEntry, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil context")
	}
	if ctx.DryRun {
		return []InstanceRegistryEntry{{
			Name: DefaultInstance, InternalID: "MSSQL15.MSSQLSERVER", ListenPort: DefaultPort,
			ProductMajor: 15, ToolsRegKey: "150", ServiceName: "MSSQLSERVER",
		}}, nil
	}
	res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+listInstanceRegistryPS+`"`, false)
	if err != nil {
		return nil, fmt.Errorf("list SQL instances from registry: %w", err)
	}
	entries, err := ParseInstanceRegistryOutput(res.GetStdout())
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// FormatInstanceRegistryTable formats registry entries for CLI output.
func FormatInstanceRegistryTable(host string, entries []InstanceRegistryEntry) string {
	host = strings.TrimSpace(host)
	if len(entries) == 0 {
		return fmt.Sprintf("host=%s: no SQL Server instances in registry", host)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "host=%s SQL instances (%d):\n", host, len(entries))
	fmt.Fprintf(&b, "  %-16s %6s %5s %-12s %s\n", "instance", "port", "major", "version", "sqlpath / data_root")
	for _, e := range entries {
		port := e.ListenPort
		if port <= 0 {
			port = 0
		}
		ver := strings.TrimSpace(e.Version)
		if ver == "" {
			ver = "-"
		}
		path := strings.TrimSpace(e.SQLPath)
		dataRoot := strings.TrimSpace(e.DataRoot)
		if path != "" && dataRoot != "" && !strings.EqualFold(path, dataRoot) {
			path = path + " | " + dataRoot
		} else if path == "" {
			path = dataRoot
		}
		if path == "" {
			path = "-"
		}
		fmt.Fprintf(&b, "  %-16s %6d %5d %-12s %s\n", e.Name, port, e.ProductMajor, ver, path)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ListInstancesOnHost prints installed instances from registry.
func ListInstancesOnHost(ctx *runner.StepContext) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	entries, err := ListInstanceRegistry(ctx)
	if err != nil {
		return err
	}
	msg := FormatInstanceRegistryTable(TargetHost(ctx), entries)
	fmt.Println(msg)
	if ctx.Logger != nil {
		ctx.Logger.Info("%s", msg)
	}
	return nil
}

// StoreRegistryEntry saves resolved entry in Results (per-host key when executor host is set).
func StoreRegistryEntry(ctx *runner.StepContext, entry InstanceRegistryEntry) {
	if ctx == nil {
		return
	}
	key := RegistryEntryResultKey(TargetHost(ctx))
	ctx.SetResult(key, entry)
	// Legacy global key for single-host install/clean paths.
	if TargetHost(ctx) == "" {
		ctx.SetResult(registryEntryResultKey, entry)
	}
	if strings.TrimSpace(entry.SqlcmdPath) != "" {
		ctx.SetResult("mssql_sqlcmd_path", entry.SqlcmdPath)
	}
}
