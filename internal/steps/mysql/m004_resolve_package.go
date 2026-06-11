package mysql

import (
	"fmt"

	"github.com/yinstall/internal/common/file"
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

// StepM004ResolvePackage resolves mysql package path, version, and install mode (binary|source).
func StepM004ResolvePackage() *runner.Step {
	resolve := func(ctx *runner.StepContext) error {
		pkg := ctx.GetParamString("mysql_package", "")
		platform := ctx.GetTargetPlatform()
		arch := ctx.GetParamString("mysql_target_arch", "")

		remoteDir := ctx.RemoteSoftwareDir
		if remoteDir == "" {
			remoteDir = commonmysql.DefaultRemoteSoftwareDir(platform)
		}

		selected, mode, err := file.ResolveMysqlPackage(ctx, ctx.LocalSoftwareDirs, remoteDir, platform, arch, pkg)
		if err != nil {
			return fmt.Errorf("mysql package resolution failed: %w", err)
		}

		version, err := file.ParseMysqlVersionFromPackage(selected)
		if err != nil {
			return err
		}

		ctx.Params["mysql_package"] = selected
		ctx.Params["mysql_version"] = version
		ctx.Params["mysql_install_mode"] = mode
		ctx.SetResult("mysql_package", selected)
		ctx.SetResult("mysql_version", version)
		ctx.SetResult("mysql_install_mode", mode)

		layout, err := ResolveLayout(ctx.Params)
		if err != nil {
			return err
		}
		ctx.SetResult("mysql_layout", layout)
		ctx.Logger.Info("M-004: package=%s VERSION=%s mode=%s MYSQL_HOME=%s", selected, version, mode, layout.Home)
		return nil
	}

	return &runner.Step{
		ID:          "M-004",
		Name:        "Resolve Package",
		Description: "Find mysql binary or source package and parse VERSION from filename",
		Tags:        []string{"mysql", "package", "mysql-software"},
		PreCheck:    resolve,
		Action: func(ctx *runner.StepContext) error {
			mysqlLogPhase(ctx, "plan", "M-004 resolve package")
			return resolve(ctx)
		},
	}
}
