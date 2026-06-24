package mssql

import (
	"fmt"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func StepMS007GenerateConfigurationINI() *runner.Step {
	return &runner.Step{
		ID:   "MS-007",
		Name: "Generate Configuration.ini",
		Tags: []string{"mssql", "mssql-instance"},
		Action: func(ctx *runner.StepContext) error {
			layout := commonmssql.ResolveLayoutFromContext(ctx)
			inst := layout.Instance
			var installData, dataDir, logDir, backupDir string
			if !layout.UseSQLDefaults {
				installData = layout.Base
				dataDir = layout.DataDir
				logDir = layout.LogDir
				backupDir = layout.BackupDir
			}
			saPwd := ctx.GetParamString("mssql_sa_password", "")
			sharedDir := layout.SharedDir
			if layout.SetupProductMajor > 0 && strings.TrimSpace(sharedDir) != "" {
				if omit, err := commonmssql.ShouldOmitInstallSharedDir(ctx, layout.SetupProductMajor, layout.Instance); err != nil {
					return err
				} else if omit {
					ctx.Logger.Info("MS-007: omit INSTALLSHAREDDIR (existing SQL major %d on host; shared components reused)",
						layout.SetupProductMajor)
					sharedDir = ""
				}
			}
			ini, err := commonmssql.RenderConfigurationINI(commonmssql.INIParams{
				Instance:       inst,
				Collation:      ctx.GetParamString("mssql_collation", "Chinese_PRC_CI_AS"),
				SAPassword:     saPwd,
				OmitSAPassword: saPwd != "",
				InstallDataDir: installData,
				DataDir:        dataDir,
				LogDir:         logDir,
				BackupDir:      backupDir,
				InstanceDir:    layout.InstanceDir,
				SharedDir:      sharedDir,
			})
			if err != nil {
				return err
			}
			remotePath := layout.AdminBase + `\Configuration.ini`
			ctx.LogScriptPreview("ini", "MS-007 Configuration.ini", ini)
			if ctx.DryRun || ctx.Precheck {
				ctx.SetResult("mssql_configuration_ini", ini)
				ctx.SetResult("mssql_configuration_ini_path", remotePath)
				return nil
			}
			if err := commonfile.RemoteWriteTextFile(ctx, remotePath, ini, false); err != nil {
				return fmt.Errorf("write Configuration.ini: %w", err)
			}
			ctx.SetResult("mssql_configuration_ini", ini)
			ctx.SetResult("mssql_configuration_ini_path", remotePath)
			return nil
		},
		PostCheck: func(ctx *runner.StepContext) error {
			if ctx.DryRun || ctx.Precheck {
				return nil
			}
			remotePath, _ := ctx.Results["mssql_configuration_ini_path"].(string)
			if remotePath == "" {
				return fmt.Errorf("mssql_configuration_ini_path not set")
			}
			q := strings.ReplaceAll(remotePath, `'`, `''`)
			res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "if (Test-Path -LiteralPath '`+q+`') { 'ok' } else { 'missing' }"`, false)
			if err != nil {
				return err
			}
			if res != nil && !strings.Contains(res.GetStdout(), "ok") {
				return fmt.Errorf("Configuration.ini missing at %s", remotePath)
			}
			return nil
		},
	}
}
