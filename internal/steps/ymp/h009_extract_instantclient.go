// h008_extract_instantclient.go - 解压 Oracle instantclient (basic)
// H-009: 解压 instantclient basic 包到安装目录

package ymp

import (
	"fmt"

	commonfile "github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

// StepH009ExtractInstantclient 解压 Oracle instantclient (basic)
func StepH009ExtractInstantclient() *runner.Step {
	return &runner.Step{
		ID:          "H-009",
		Name:        "Extract Oracle Instantclient",
		Description: "Extract Oracle instantclient basic package",
		Tags:        []string{"ymp", "oracle"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			pkg := ctx.GetParamString("ymp_instantclient_basic", "")
			if pkg == "" {
				ctx.Logger.Info("ymp_instantclient_basic not specified, searching for latest instantclient-basic package...")
				remoteDir := ctx.RemoteSoftwareDir
				if remoteDir == "" {
					remoteDir = "/data/yashan/soft"
				}
				latestPkg, err := commonfile.FindLatestInstantclientBasicPackage(ctx, ctx.LocalSoftwareDirs, remoteDir)
				if err != nil {
					return fmt.Errorf("ymp_instantclient_basic not specified and auto-search failed: %w", err)
				}
				ctx.Logger.Info("Found latest instantclient-basic package: %s", latestPkg)
				ctx.Params["ymp_instantclient_basic"] = latestPkg
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			pkg := ctx.GetParamString("ymp_instantclient_basic", "")
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")
			ympUser := ctx.GetParamString("ymp_user", "ymp")

			ctx.Logger.Info("Looking for instantclient basic package: %s", pkg)

			fullPath, err := commonfile.FindAndDistribute(
				ctx,
				pkg,
				ctx.LocalSoftwareDirs,
				ctx.RemoteSoftwareDir,
			)
			if err != nil {
				return fmt.Errorf("instantclient basic package not found: %w", err)
			}

			// 设置包属主
			ctx.Execute(fmt.Sprintf("chown %s:%s %s", ympUser, ympUser, fullPath), true)

			// 解压
			ctx.Logger.Info("Extracting instantclient: %s -> %s", fullPath, installDir)
			cmd := fmt.Sprintf("unzip -o %s -d %s", fullPath, installDir)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to extract instantclient: %w", err)
			}

			// 设置目录属主
			ctx.Execute(fmt.Sprintf("chown -R %s:%s %s/instantclient_*", ympUser, ympUser, installDir), true)

			ctx.Logger.Info("Instantclient basic extracted successfully")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			installDir := ctx.GetParamString("ymp_install_dir", "/opt/ymp")

			// 检查 instantclient 目录是否存在
			result, _ := ctx.Execute(fmt.Sprintf("ls -d %s/instantclient_* 2>/dev/null | head -1", installDir), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("instantclient directory not found in %s", installDir)
			}
			ctx.Logger.Info("✓ Instantclient directory found")
			return nil
		},
	}
}
