package mssql

// AG remove helpers: drop availability group on primary and detect WSFC/AG
// artifacts for A-051/A-052 and A-015 status reporting.

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// DetectWSFCClusterName returns the local WSFC cluster name or empty.
func DetectWSFCClusterName(ctx *runner.StepContext) (string, error) {
	stdout, err := runWSFCElevatedScalar(ctx, "detect WSFC cluster", WSFCClusterNamePowerShell())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout), nil
}

// WSFCClusterGroupNamePS returns cluster group name if present.
func WSFCClusterGroupNamePS(groupName string) string {
	groupName = strings.ReplaceAll(groupName, "'", "''")
	return fmt.Sprintf(`Import-Module FailoverClusters -ErrorAction SilentlyContinue
$g = Get-ClusterGroup -Name '%s' -ErrorAction SilentlyContinue
if ($g) { $g.Name }
exit 0`, groupName)
}

// AvailabilityGroupExistsSQL returns 1 when AG exists.
func AvailabilityGroupExistsSQL(agName string) string {
	agName = strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`IF EXISTS (SELECT 1 FROM sys.availability_groups WHERE name = N'%s') SELECT 1 ELSE SELECT 0;`, agName)
}

// DropAvailabilityGroupSQL removes AG databases then drops the AG on primary.
func DropAvailabilityGroupSQL(agName string) string {
	agBracket := strings.ReplaceAll(agName, "]", "]]")
	agSQL := strings.ReplaceAll(agName, "'", "''")
	return fmt.Sprintf(`IF EXISTS (SELECT 1 FROM sys.availability_groups WHERE name = N'%s')
BEGIN
  DECLARE @db sysname;
  DECLARE dbcur CURSOR LOCAL FAST_FORWARD FOR
    SELECT d.name
    FROM sys.availability_databases_cluster adc
    INNER JOIN sys.databases d ON adc.group_database_id = d.group_database_id
    INNER JOIN sys.availability_groups g ON g.group_id = adc.group_id
    WHERE g.name = N'%s';
  OPEN dbcur;
  FETCH NEXT FROM dbcur INTO @db;
  WHILE @@FETCH_STATUS = 0
  BEGIN
    DECLARE @s nvarchar(max) = N'ALTER AVAILABILITY GROUP [%s] REMOVE DATABASE [' + REPLACE(@db,']',']]') + N'];';
    EXEC(@s);
    FETCH NEXT FROM dbcur INTO @db;
  END
  CLOSE dbcur;
  DEALLOCATE dbcur;
  DROP AVAILABILITY GROUP [%s];
END`, agSQL, agSQL, agBracket, agBracket)
}

func sqlServiceRunning(ctx *runner.StepContext) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("nil context")
	}
	if entry, ok := RegistryEntryFromContext(ctx); ok {
		status, err := QueryInstanceServiceStatus(ctx, entry)
		if err == nil {
			return status == InstanceServiceRunning, nil
		}
	}
	svc := ServiceNameForInstance(ResolvedInstanceName(ctx))
	q := strings.ReplaceAll(svc, `'`, `''`)
	res, err := ctx.Execute(`powershell -NoProfile -Command "(Get-Service -Name '`+q+`' -ErrorAction SilentlyContinue).Status"`, false)
	if err != nil {
		return false, fmt.Errorf("query SQL service %s: %w", svc, err)
	}
	if res == nil {
		return false, fmt.Errorf("query SQL service %s: empty result", svc)
	}
	return strings.Contains(strings.ToLower(res.GetStdout()), "running"), nil
}

func runSqlcmdQuery(ctx *runner.StepContext, label, query string) error {
	if ctx.DryRun || ctx.Precheck {
		cmd := SqlcmdQueryCommand(ctx, query)
		ctx.LogScriptPreview("sqlcmd", label, cmd)
		return nil
	}
	if err := PrepareSqlcmdSession(ctx); err != nil {
		return err
	}
	cmd := SqlcmdQueryCommand(ctx, query)
	ctx.LogScriptPreview("sqlcmd", label, cmd)
	_, err := ctx.ExecuteWithCheck(cmd, false)
	return err
}

func querySqlcmdScalar(ctx *runner.StepContext, label, query string) (string, error) {
	if ctx.DryRun || ctx.Precheck {
		cmd := SqlcmdQueryCommand(ctx, query)
		ctx.LogScriptPreview("sqlcmd", label, cmd)
		return "", nil
	}
	if err := PrepareSqlcmdSession(ctx); err != nil {
		return "", err
	}
	cmd := SqlcmdQueryCommand(ctx, query)
	ctx.LogScriptPreview("sqlcmd", label, cmd)
	res, err := ctx.ExecuteWithCheck(cmd, false)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", fmt.Errorf("%s: empty sqlcmd result", label)
	}
	return res.GetStdout(), nil
}

func sqlcmdScalarIsOne(stdout string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if IsSqlcmdMetaLine(line) {
			continue
		}
		if line == "1" {
			return true
		}
	}
	return false
}

// AGExistsOnPrimary reports whether the AG exists in SQL (primary only).
func AGExistsOnPrimary(ctx *runner.StepContext, agName string) (bool, error) {
	if !IsPrimaryHost(ctx) {
		return false, nil
	}
	running, err := sqlServiceRunning(ctx)
	if err != nil {
		return false, err
	}
	if !running {
		return false, nil
	}
	stdout, err := querySqlcmdScalar(ctx, "AG exists", AvailabilityGroupExistsSQL(agName))
	if err != nil {
		return false, err
	}
	return sqlcmdScalarIsOne(stdout), nil
}

// WSFCClusterGroupExists reports whether AG-named cluster group exists on this node.
func WSFCClusterGroupExists(ctx *runner.StepContext, groupName string) (bool, error) {
	stdout, err := runWSFCElevatedScalar(ctx, "WSFC cluster group", WSFCClusterGroupNamePS(groupName))
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(stdout) != "", nil
}

func runWSFCElevatedScalar(ctx *runner.StepContext, label, script string) (string, error) {
	if ctx == nil {
		return "", nil
	}
	script = strings.TrimSpace(script)
	if script != "" && !strings.Contains(strings.ToLower(script), "exit ") {
		script += "; exit 0"
	}
	return runWSFCPowerShellScalar(ctx, label, script)
}

func runWSFCPowerShellScalar(ctx *runner.StepContext, label, script string) (string, error) {
	if ctx == nil {
		return "", nil
	}
	if ctx.DryRun || ctx.Precheck {
		ctx.LogScriptPreview("powershell", label, script)
		return "", nil
	}
	return runPSCommandScalar(ctx, label, script)
}

// DropAvailabilityGroup removes AG databases and drops AG on primary.
func DropAvailabilityGroup(ctx *runner.StepContext, agName string) error {
	if !IsPrimaryHost(ctx) {
		return nil
	}
	running, err := sqlServiceRunning(ctx)
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("WSFC/AG cleanup: SQL service not running for instance %s; start SQL Server before dropping availability group",
			ResolvedInstanceName(ctx))
	}
	exists, err := AGExistsOnPrimary(ctx, agName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	ctx.LogPhase("plan", "drop-ag-start")
	if err := runSqlcmdQuery(ctx, "drop AG", DropAvailabilityGroupSQL(agName)); err != nil {
		return err
	}
	ctx.LogPhase("plan", "drop-ag-done")
	return nil
}

// WSFCCleanArtifacts reports whether this host has WSFC/AG artifacts to clean.
func WSFCCleanArtifacts(ctx *runner.StepContext, agName string) (bool, error) {
	groupExists, err := WSFCClusterGroupExists(ctx, agName)
	if err != nil {
		return false, err
	}
	if groupExists {
		return true, nil
	}
	agExists, err := AGExistsOnPrimary(ctx, agName)
	if err != nil {
		return false, err
	}
	return agExists, nil
}

// WSFCClusterPresent reports whether a WSFC cluster is joined on this node.
func WSFCClusterPresent(ctx *runner.StepContext) (bool, error) {
	name, err := DetectWSFCClusterName(ctx)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(name) != "", nil
}
