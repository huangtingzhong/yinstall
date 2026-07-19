package db

import (
	"fmt"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// stepCreateArchdg 部署完成后创建独立的归档磁盘组（ArchDG）
func stepCreateArchdg() *runner.Step {
	return &runner.Step{
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

			archDisks := DiskPathListFromYACDG(archdgStr)
			if len(archDisks) == 0 {
				return fmt.Errorf("no archdg disks configured, skipping ArchDG creation")
			}

			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "sqls=2 op=create-archdg+set-dest node=first")
			archdgStr := ctx.GetParamString("yac_archdg", "")
			user := ctx.GetParamString("os_user", "yashan")
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			dataPath := ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")

			archDisks := DiskPathListFromYACDG(archdgStr)

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

			res, err := dbRunSQLInstallLayoutPhase(firstCtx, user, installPath, dataPath, "create-archdg", createSQL, true)
			if err != nil {
				stdout := ""
				if res != nil {
					stdout = res.Stdout
				}
				if strings.Contains(stdout, "already exists") {
					ctx.Logger.Info("Archive diskgroup ARCH already exists, skipping creation")
					return nil
				}
				if res != nil {
					commonsql.ReportSQLFailure(firstCtx, createSQL, res)
				}
				return fmt.Errorf("failed to create archive diskgroup: %w\nOutput: %s", err, stdout)
			}

			ctx.Logger.Info("Archive diskgroup ARCH created successfully")

			alterSQL := "ALTER DATABASE SET ARCHIVELOG DEST '+ARCH';"
			ctx.Logger.Info("Setting archive destination to +ARCH...")

			if _, err := dbRunSQLInstallLayoutPhase(firstCtx, user, installPath, dataPath, "set-archive-dest", alterSQL, true); err != nil {
				return fmt.Errorf("failed to set archive destination: %w", err)
			}
			ctx.Logger.Info("Archive destination set to +ARCH")

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			installPath := ctx.GetParamString("db_install_path", "/data/yashan/yasdb_home")
			dataPath := ctx.GetParamString("db_data_path", "/data/yashan/yasdb_data")

			hosts := ctx.HostsToRun()
			firstCtx := ctx.ForHost(hosts[0])

			checkSQL := "SELECT name, state, total_mb, free_mb FROM v\\$yfs_diskgroup WHERE name = 'ARCH';"
			res, err := dbRunSQLInstallLayoutPhase(firstCtx, user, installPath, dataPath, "verify-archdg", checkSQL, true)
			if err != nil {
				return fmt.Errorf("archive diskgroup verification query failed: %w", err)
			}

			if res != nil && strings.Contains(strings.ToUpper(res.Stdout), "ARCH") {
				ctx.Logger.Info("Archive diskgroup ARCH verified:")
				for _, line := range strings.Split(res.Stdout, "\n") {
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
