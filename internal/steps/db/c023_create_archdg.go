package db

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StepC023CreateArchDG Create independent archive diskgroup after deployment
func StepC023CreateArchDG() *runner.Step {
	return &runner.Step{
		ID:          "C-023",
		Name:        "Create Archive Diskgroup",
		Description: "Create independent archive diskgroup on shared storage (optional)",
		Tags:        []string{"db", "yac", "archdg"},
		Optional:    true,
		Global:      true,

		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("yac_archdg_enable", false) {
				return fmt.Errorf("independent ArchDG not enabled (use --yac-archdg-enable)")
			}

			archdgStr := ctx.GetParamString("yac_archdg", "")
			if archdgStr == "" {
				return fmt.Errorf("no archdg disks configured, skipping ArchDG creation")
			}

			parts := strings.SplitN(archdgStr, ":", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
				return fmt.Errorf("no archdg disks configured, skipping ArchDG creation")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			archdgStr := ctx.GetParamString("yac_archdg", "")
			user := ctx.GetParamString("os_user", "yashan")
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			dataPath := ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")

			parts := strings.SplitN(archdgStr, ":", 2)
			var archDisks []string
			for _, d := range strings.Split(parts[1], ",") {
				d = strings.TrimSpace(d)
				if d != "" {
					archDisks = append(archDisks, d)
				}
			}

			if len(archDisks) == 0 {
				ctx.Logger.Info("No archive disks found, skipping")
				return nil
			}

			var diskParts []string
			for _, disk := range archDisks {
				diskParts = append(diskParts, fmt.Sprintf("'%s' FORCE", disk))
			}
			createSQL := fmt.Sprintf(
				"CREATE DISKGROUP ARCH EXTERNAL REDUNDANCY DISK %s ATTRIBUTE 'au_size'='1M';",
				strings.Join(diskParts, ","),
			)

			ctx.Logger.Info("Creating archive diskgroup ARCH on first node...")
			ctx.Logger.Info("  Disks: %v", archDisks)
			ctx.Logger.Info("  SQL: %s", createSQL)

			hosts := ctx.HostsToRun()
			firstHost := hosts[0]
			firstCtx := ctx.ForHost(firstHost)

			yasqlCmd := buildYasqlCmd(installPath, clusterName, dataPath, createSQL)
			cmd := fmt.Sprintf("su - %s -c '%s'", user, yasqlCmd)
			result, err := firstCtx.ExecuteWithCheck(cmd, true)
			if err != nil {
				stdout := ""
				if result != nil {
					stdout = result.GetStdout()
				}
				if strings.Contains(stdout, "already exists") {
					ctx.Logger.Info("Archive diskgroup ARCH already exists, skipping creation")
					return nil
				}
				return fmt.Errorf("failed to create archive diskgroup: %w\nOutput: %s", err, stdout)
			}

			ctx.Logger.Info("Archive diskgroup ARCH created successfully")

			alterSQL := "ALTER DATABASE SET ARCHIVELOG DEST '+ARCH';"
			ctx.Logger.Info("Setting archive destination to +ARCH...")

			alterCmd := buildYasqlCmd(installPath, clusterName, dataPath, alterSQL)
			cmd = fmt.Sprintf("su - %s -c '%s'", user, alterCmd)
			result, err = firstCtx.Execute(cmd, true)
			if err != nil || (result != nil && result.GetExitCode() != 0) {
				ctx.Logger.Warn("Failed to set archive destination (can be set manually later): %s", alterSQL)
			} else {
				ctx.Logger.Info("Archive destination set to +ARCH")
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			dataPath := ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")

			hosts := ctx.HostsToRun()
			firstCtx := ctx.ForHost(hosts[0])

			checkSQL := "SELECT name, state, total_mb, free_mb FROM v\\$yfs_diskgroup WHERE name = 'ARCH';"
			yasqlCmd := buildYasqlCmd(installPath, clusterName, dataPath, checkSQL)
			cmd := fmt.Sprintf("su - %s -c '%s'", user, yasqlCmd)
			result, _ := firstCtx.Execute(cmd, true)

			if result != nil && strings.Contains(strings.ToUpper(result.GetStdout()), "ARCH") {
				ctx.Logger.Info("Archive diskgroup ARCH verified:")
				for _, line := range strings.Split(result.GetStdout(), "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						ctx.Logger.Info("  %s", line)
					}
				}
			}

			return nil
		},
	}
}

// buildYasqlCmd builds a yasql command with explicit PATH using install path.
// This avoids relying on .bashrc which may not be configured yet at C-023 time.
func buildYasqlCmd(installPath, clusterName, dataPath, sql string) string {
	escapedSQL := strings.ReplaceAll(sql, "$", "\\$")
	return fmt.Sprintf(
		`export YASDB_HOME=%s/$(ls %s/ 2>/dev/null | head -1) && `+
			`export YASCS_HOME=%s/ycs/ce-1-1 && `+
			`export PATH=$YASDB_HOME/bin:$PATH && `+
			`export LD_LIBRARY_PATH=$YASDB_HOME/lib:$LD_LIBRARY_PATH && `+
			`yasql -S / as sysdba <<EOF
%s
EOF`,
		installPath, installPath, dataPath, escapedSQL)
}
