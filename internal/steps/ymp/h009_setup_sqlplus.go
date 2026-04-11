// h009_setup_sqlplus.go - 解压 sqlplus 并配置环境变量
// H-009: 解压 Oracle instantclient sqlplus + 写入 env 文件（可选）

package ymp

import (
	"fmt"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

// StepH009SetupSQLPlus 解压 sqlplus 并写入环境变量
func StepH009SetupSQLPlus() *runner.Step {
	return &runner.Step{
		ID:          "H-009",
		Name:        "Setup SQLPlus and Env",
		Description: "Extract Oracle sqlplus and write PATH/LD_LIBRARY_PATH env file",
		Tags:        []string{"ymp", "oracle", "sqlplus"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			sqlplusPkg := ctx.GetParamString("ymp_instantclient_sqlplus", "")
			if sqlplusPkg == "" {
				ctx.Logger.Info("ymp_instantclient_sqlplus not specified, searching for latest instantclient-sqlplus package...")
				remoteDir := ctx.RemoteSoftwareDir
				if remoteDir == "" {
					remoteDir = "/data/yashan/soft"
				}
				latestPkg, err := commonfile.FindLatestInstantclientSQLPlusPackage(ctx, ctx.LocalSoftwareDirs, remoteDir)
				if err != nil {
					return fmt.Errorf("instantclient-sqlplus not specified and auto-search found nothing, skipping")
				}
				ctx.Logger.Info("Found latest instantclient-sqlplus package: %s", latestPkg)
				ctx.Params["ymp_instantclient_sqlplus"] = latestPkg
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			sqlplusPkg := ctx.GetParamString("ymp_instantclient_sqlplus", "")
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			ympUser := ctx.GetParamString("ymp_user", "ymp")
			envFile := ctx.GetParamString("ymp_oracle_env_file", fmt.Sprintf("/home/%s/.oracle", ympUser))

			// 解压 sqlplus 包
			ctx.Logger.Info("Looking for sqlplus package: %s", sqlplusPkg)
			fullPath, err := commonfile.FindAndDistribute(
				ctx,
				sqlplusPkg,
				ctx.LocalSoftwareDirs,
				ctx.RemoteSoftwareDir,
			)
			if err != nil {
				return fmt.Errorf("sqlplus package not found: %w", err)
			}

			ctx.Execute(fmt.Sprintf("chown %s:%s %s", ympUser, ympUser, fullPath), true)

			ctx.Logger.Info("Extracting sqlplus: %s -> %s", fullPath, installDir)
			cmd := fmt.Sprintf("unzip -o %s -d %s", fullPath, installDir)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to extract sqlplus: %w", err)
			}

			// 获取 instantclient 实际目录名
			result, _ := ctx.Execute(fmt.Sprintf("ls -d %s/instantclient_* 2>/dev/null | head -1", installDir), false)
			icDir := ""
			if result != nil {
				icDir = strings.TrimSpace(result.GetStdout())
			}
			if icDir == "" {
				icDir = fmt.Sprintf("%s/instantclient_19_29", installDir) // fallback
			}

			// 写入环境变量文件
			ctx.Logger.Info("Writing Oracle env to: %s", envFile)
			envContent := fmt.Sprintf("export PATH=%s:$PATH\nexport LD_LIBRARY_PATH=%s:$LD_LIBRARY_PATH", icDir, icDir)
			cmd = fmt.Sprintf("cat > %s << 'HTZ'\n%s\nHTZ", envFile, envContent)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to write env file: %w", err)
			}

			ctx.Execute(fmt.Sprintf("chown %s:%s %s", ympUser, ympUser, envFile), true)

			ctx.Logger.Info("SQLPlus and environment configured successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			ympUser := ctx.GetParamString("ymp_user", "ymp")
			envFile := ctx.GetParamString("ymp_oracle_env_file", fmt.Sprintf("/home/%s/.oracle", ympUser))

			result, _ := ctx.Execute(fmt.Sprintf("test -f %s", envFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("env file not found: %s", envFile)
			}

			result, _ = ctx.Execute(fmt.Sprintf("grep 'LD_LIBRARY_PATH' %s", envFile), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("LD_LIBRARY_PATH not found in %s", envFile)
			}

			ctx.Logger.Info("✓ Oracle env file verified: %s", envFile)
			return nil
		},
	}
}
