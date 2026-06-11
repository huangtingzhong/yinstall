package mysql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/common/file"
	commonmysql "github.com/yinstall/internal/common/mysql"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

func mysqlInstallMode(ctx *runner.StepContext) string {
	if v := ctx.GetParamString("mysql_install_mode", ""); v != "" {
		return v
	}
	if v, ok := ctx.Results["mysql_install_mode"].(string); ok && v != "" {
		return v
	}
	return file.MysqlInstallBinary
}

// StepM008ExtractPackage distributes and extracts mysql binary package or builds from source.
func StepM008ExtractPackage() *runner.Step {
	extractLinux := func(ctx *runner.StepContext, layout Layout, pkg string) error {
		useSudo := UseSudo(ctx)
		remoteDir := ctx.RemoteSoftwareDir
		if remoteDir == "" {
			remoteDir = commonmysql.DefaultRemoteSoftwareDir(ctx.GetTargetPlatform())
		}
		distributed, err := file.FindAndDistribute(ctx, pkg, ctx.LocalSoftwareDirs, remoteDir)
		if err != nil {
			return err
		}
		mysqlLogPhase(ctx, "extract-start", distributed)
		homeQ := commonos.ShellSingleQuote(layout.Home)
		lower := strings.ToLower(distributed)
		switch {
		case strings.HasSuffix(lower, ".zip"):
			cmd := fmt.Sprintf("mkdir -p %s && unzip -o %s -d %s",
				homeQ, commonos.ShellSingleQuote(distributed), homeQ)
			if _, err := ctx.ExecuteWithCheck(cmd, useSudo); err != nil {
				return err
			}
		case strings.HasSuffix(lower, ".tar.xz"):
			cmd := fmt.Sprintf("mkdir -p %s && tar -xJf %s -C %s",
				homeQ, commonos.ShellSingleQuote(distributed), homeQ)
			if _, err := ctx.ExecuteWithCheck(cmd, useSudo); err != nil {
				return err
			}
		default:
			cmd := fmt.Sprintf("mkdir -p %s && tar -xf %s -C %s",
				homeQ, commonos.ShellSingleQuote(distributed), homeQ)
			if _, err := ctx.ExecuteWithCheck(cmd, useSudo); err != nil {
				return err
			}
		}
		inner, _ := detectInnerMysqlDir(ctx, layout, layout.Version)
		return flattenExtractDir(ctx, layout, inner)
	}

	extractWindows := func(ctx *runner.StepContext, layout Layout, pkg string) error {
		remoteDir := ctx.RemoteSoftwareDir
		distributed, err := file.FindAndDistribute(ctx, pkg, ctx.LocalSoftwareDirs, remoteDir)
		if err != nil {
			return err
		}
		home := filepathToSlash(layout.Home)
		dist := filepathToSlash(distributed)
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "$stage=Join-Path $env:TEMP 'yinstall_mysql'; $dest='%s'; Expand-Archive -LiteralPath '%s' -DestinationPath $stage -Force; $inner=Get-ChildItem -Path $stage -Directory | Select-Object -First 1; if (-not $inner) { exit 1 }; robocopy $inner.FullName $dest /E; if ($LASTEXITCODE -ge 8) { exit 1 } else { exit 0 }"`,
			home, dist)
		_, err = ctx.ExecuteWithCheck(cmd, false)
		if err != nil {
			return err
		}
		check := fmt.Sprintf(`powershell -NoProfile -Command "if (-not (Test-Path -LiteralPath '%s/bin/mysqld.exe')) { exit 1 }"`, home)
		_, err = ctx.ExecuteWithCheck(check, false)
		return err
	}

	run := func(ctx *runner.StepContext) error {
		layout, err := layoutFromCtx(ctx)
		if err != nil {
			return err
		}
		if softwareMysqldExists(ctx, layout) && !ctx.IsForceStep() {
			ctx.Logger.Info("M-008: software already present at %s, skipping extract (use -f M-008 to reinstall)", layout.Home)
			return nil
		}
		pkg := ctx.GetParamString("mysql_package", "")
		if pkg == "" {
			return fmt.Errorf("mysql_package not set")
		}
		if mysqlInstallMode(ctx) == file.MysqlInstallSource {
			return buildFromSource(ctx, layout, pkg)
		}
		switch ctx.GetTargetPlatform() {
		case PlatformWindows:
			return extractWindows(ctx, layout, pkg)
		default:
			return extractLinux(ctx, layout, pkg)
		}
	}

	return &runner.Step{
		ID:          "M-008",
		Name:        "Extract Package",
		Description: "Upload/extract binary package or build from source tarball",
		Tags:        []string{"mysql", "package", "mysql-software"},
		PreCheck: func(ctx *runner.StepContext) error {
			if _, err := layoutFromCtx(ctx); err != nil {
				return runner.SkipPrecheckDryRunWhenUpstreamArtifactMissing(ctx, err)
			}
			if ctx.GetParamString("mysql_package", "") == "" {
				return fmt.Errorf("mysql_package required")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if mysqlInstallMode(ctx) == file.MysqlInstallSource {
				mysqlLogPhase(ctx, "plan", "M-008 source build")
			} else {
				mysqlLogPhase(ctx, "plan", "M-008 extract")
			}
			return run(ctx)
		},
		PostCheck: func(ctx *runner.StepContext) error {
			layout, err := layoutFromCtx(ctx)
			if err != nil {
				return err
			}
			cmd := fmt.Sprintf("test -x %s/bin/mysqld || test -x %s/bin/mysqld.exe",
				commonos.ShellSingleQuote(layout.Home), commonos.ShellSingleQuote(layout.Home))
			if ctx.GetTargetPlatform() == PlatformWindows {
				cmd = fmt.Sprintf(`powershell -NoProfile -Command "Test-Path -LiteralPath '%s/bin/mysqld.exe'"`, filepathToSlash(layout.Home))
				res, _ := ctx.Execute(cmd, false)
				if !powershellStdoutTrue(res) {
					return fmt.Errorf("mysqld binary not found under %s", layout.Home)
				}
				return nil
			}
			res, _ := ctx.Execute(cmd, false)
			if res == nil || res.GetExitCode() != 0 {
				return fmt.Errorf("mysqld binary not found under %s", layout.Home)
			}
			return nil
		},
	}
}

func flattenExtractDir(ctx *runner.StepContext, layout Layout, innerDir string) error {
	if innerDir == "" {
		return nil
	}
	innerQ := commonos.ShellSingleQuote(innerDir)
	homeQ := commonos.ShellSingleQuote(layout.Home)
	cmd := fmt.Sprintf(`if [ -d %s ]; then mv %s/* %s/ 2>/dev/null; rm -rf %s; fi`, innerQ, innerQ, homeQ, innerQ)
	_, err := ctx.ExecuteWithCheck(cmd, UseSudo(ctx))
	return err
}

func detectInnerMysqlDir(ctx *runner.StepContext, layout Layout, version string) (string, error) {
	pattern := fmt.Sprintf("%s/mysql-%s*", layout.Home, version)
	res, _ := ctx.Execute(fmt.Sprintf("ls -1d %s 2>/dev/null | head -1", pattern), false)
	if res != nil && strings.TrimSpace(res.GetStdout()) != "" {
		return strings.TrimSpace(res.GetStdout()), nil
	}
	return "", nil
}

func sourceBuildRoot(layout Layout) string {
	return fmt.Sprintf("%s/build/%s", layout.Base, layout.Version)
}

func sourceTreeDir(layout Layout) string {
	return fmt.Sprintf("%s/mysql-%s", sourceBuildRoot(layout), layout.Version)
}

func sourceCmakeBuildDir(layout Layout) string {
	return fmt.Sprintf("%s/bld", sourceTreeDir(layout))
}

func buildFromSource(ctx *runner.StepContext, layout Layout, pkg string) error {
	if ctx.GetTargetPlatform() == PlatformWindows {
		return fmt.Errorf("source install is not supported on Windows; provide a mysql-*-winx64.zip binary package")
	}

	remoteDir := ctx.RemoteSoftwareDir
	if remoteDir == "" {
		remoteDir = commonmysql.DefaultRemoteSoftwareDir(ctx.GetTargetPlatform())
	}
	distributed, err := file.FindAndDistribute(ctx, pkg, ctx.LocalSoftwareDirs, remoteDir)
	if err != nil {
		return err
	}

	buildRoot := sourceBuildRoot(layout)
	treeDir := sourceTreeDir(layout)
	bldDir := sourceCmakeBuildDir(layout)
	jobs := ctx.GetParamInt("mysql_build_parallel", 0)
	if jobs <= 0 {
		res, _ := ctx.Execute("nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4", false)
		if res != nil {
			if n, err := strconv.Atoi(strings.TrimSpace(res.GetStdout())); err == nil && n > 0 {
				jobs = n
			}
		}
		if jobs <= 0 {
			jobs = 4
		}
	}

	mysqlLogPhase(ctx, "source-start", fmt.Sprintf("pkg=%s jobs=%d prefix=%s", distributed, jobs, layout.Home))

	for _, tool := range []string{"cmake", "gcc", "g++", "make"} {
		res, _ := ctx.Execute(fmt.Sprintf("command -v %s >/dev/null 2>&1", tool), false)
		if res == nil || res.GetExitCode() != 0 {
			return fmt.Errorf("source build requires %q on target (install via OS deps / B-015)", tool)
		}
	}

	script := fmt.Sprintf(`set -e
rm -rf %s
mkdir -p %s
tar -xf %s -C %s
test -d %s
mkdir -p %s
cd %s
cmake %s \
  -DCMAKE_INSTALL_PREFIX=%s \
  -DMYSQL_DATADIR=%s \
  -DSYSCONFDIR=%s \
  -DWITH_UNIT_TESTS=OFF \
  -DWITH_ROUTER=OFF \
  -DWITHOUT_EXAMPLE_STORAGE_ENGINE=ON \
  -DWITH_BOOST=../boost
cmake --build . --parallel %d
cmake --build . --target install
`,
		commonos.ShellSingleQuote(buildRoot),
		commonos.ShellSingleQuote(buildRoot),
		commonos.ShellSingleQuote(distributed),
		commonos.ShellSingleQuote(buildRoot),
		commonos.ShellSingleQuote(treeDir),
		commonos.ShellSingleQuote(bldDir),
		commonos.ShellSingleQuote(bldDir),
		commonos.ShellSingleQuote(treeDir),
		commonos.ShellSingleQuote(layout.Home),
		commonos.ShellSingleQuote(layout.Data),
		commonos.ShellSingleQuote(layout.Other),
		jobs,
	)
	ctx.LogScriptPreview("shell", "mysql-source-build", script)
	_, err = ctx.ExecuteWithCheck(script, true)
	if err != nil {
		return err
	}

	if ctx.GetTargetPlatform() == PlatformLinux {
		user := ctx.GetParamString("os_user", "mysql")
		group := ctx.GetParamString("os_group", "mysql")
		chown := fmt.Sprintf("chown -R %s:%s %s",
			user, group, commonos.ShellSingleQuote(layout.Home))
		_, err = ctx.ExecuteWithCheck(chown, true)
	}
	return err
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, `\`, `/`)
}
