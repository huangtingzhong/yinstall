package mssql

import (
	"fmt"
	"strings"
)

// INIParams holds Configuration.ini template fields.
type INIParams struct {
	Instance       string
	Features       string
	Collation      string
	SecurityMode   string
	SAPassword     string
	OmitSAPassword bool // WinRM: SAPWD in INI triggers DPAPI CryptographicException; pass via setup.exe /SAPWD instead
	TCPEnabled     string
	DataDir        string
	LogDir         string
	BackupDir      string
	InstallDataDir string
	InstanceDir    string
	SharedDir      string
	SQLSvcAccount  string
}

// DefaultSQLSvcAccount returns NT Service account for instance.
func DefaultSQLSvcAccount(instance string) string {
	instance = strings.TrimSpace(instance)
	if instance == "" || strings.EqualFold(instance, DefaultInstance) {
		return `NT Service\MSSQLSERVER`
	}
	return `NT Service\MSSQL$` + instance
}

// RenderConfigurationINI generates setup Configuration.ini content.
// Path keys are omitted when empty so setup.exe uses SQL Server default Program Files layout.
func RenderConfigurationINI(p INIParams) (string, error) {
	if p.Features == "" {
		p.Features = "SQLENGINE"
	}
	if p.Collation == "" {
		p.Collation = "Chinese_PRC_CI_AS"
	}
	if p.SecurityMode == "" {
		p.SecurityMode = "SQL"
	}
	if p.TCPEnabled == "" {
		p.TCPEnabled = "1"
	}
	if p.SQLSvcAccount == "" {
		p.SQLSvcAccount = DefaultSQLSvcAccount(p.Instance)
	}
	var lines []string
	lines = append(lines,
		"[OPTIONS]",
		"ACTION=Install",
		"FEATURES="+p.Features,
		"INSTANCENAME="+p.Instance,
		fmt.Sprintf(`SQLSVCACCOUNT="%s"`, p.SQLSvcAccount),
		"SQLSVCSTARTUPTYPE=Automatic",
		`SQLSYSADMINACCOUNTS="BUILTIN\Administrators"`,
		"SECURITYMODE="+p.SecurityMode,
	)
	if !p.OmitSAPassword && strings.TrimSpace(p.SAPassword) != "" {
		lines = append(lines, "SAPWD=\""+strings.ReplaceAll(p.SAPassword, `"`, `\"`)+"\"")
	}
	lines = append(lines,
		"TCPENABLED="+p.TCPEnabled,
		"SQLCOLLATION="+p.Collation,
	)
	if s := strings.TrimSpace(p.InstallDataDir); s != "" {
		lines = append(lines, "INSTALLSQLDATADIR="+s)
	}
	if s := strings.TrimSpace(p.DataDir); s != "" {
		lines = append(lines, "SQLUSERDBDIR="+s)
	}
	if s := strings.TrimSpace(p.LogDir); s != "" {
		lines = append(lines, "SQLUSERDBLOGDIR="+s)
	}
	if s := strings.TrimSpace(p.BackupDir); s != "" {
		lines = append(lines, "SQLBACKUPDIR="+s)
	}
	if s := strings.TrimSpace(p.InstanceDir); s != "" {
		lines = append(lines, "INSTANCEDIR="+s)
	}
	if s := strings.TrimSpace(p.SharedDir); s != "" {
		lines = append(lines, "INSTALLSHAREDDIR="+s)
	}
	lines = append(lines,
		"SUPPRESSPRIVACYSTATEMENTNOTICE=True",
		"UpdateEnabled=0",
		"USEMICROSOFTUPDATE=false",
		"")
	return strings.Join(lines, "\n"), nil
}

// SetupCommand builds setup.exe command line.
// Uses /Q (fully silent) for unattended remote/SSH installs; /QS still shows UI on errors.
func SetupCommand(setupExe, iniPath string, quiet bool) string {
	flags := "/ConfigurationFile=" + iniPath
	if quiet {
		flags += " /Q"
	}
	return fmt.Sprintf(`"%s" %s /IACCEPTSQLSERVERLICENSETERMS`, setupExe, flags)
}
