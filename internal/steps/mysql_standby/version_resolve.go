package mysql_standby

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

type replicaSoftwarePlan struct {
	Version string
	Home    string
	Package string
	Source  string // installed | package | explicit
}

type detectedMysqlSoftware struct {
	Version string
	Home    string
	Source  string // client | product
}

func resolveReplicaSoftware(ctx *runner.StepContext, primaryVer string) (replicaSoftwarePlan, error) {
	primaryNorm := layoutVersionFromServerVersion(primaryVer)
	if primaryNorm == "" && !ctx.DryRun && !ctx.Precheck {
		return replicaSoftwarePlan{}, fmt.Errorf("primary_mysql_version unknown; run MR-002 first")
	}

	base := ctx.GetParamString("mysql_base", commonmysql.DefaultBase(replicaPlatform(ctx)))
	explicitVer := strings.TrimSpace(ctx.GetParamString("mysql_version", ""))
	explicitPkg := strings.TrimSpace(ctx.GetParamString("mysql_package", ""))

	validate := func(ver string) error {
		if primaryNorm == "" || ver == "" {
			return nil
		}
		ok, err := commonmysql.ReplicaVersionOK(ver, primaryNorm)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("replica software %s < primary %s; replication requires replica version >= primary", ver, primaryNorm)
		}
		return nil
	}

	if explicitVer != "" {
		if err := validate(explicitVer); err != nil {
			return replicaSoftwarePlan{}, err
		}
		if softwareExistsForVersion(ctx, base, explicitVer) {
			home := strings.TrimSpace(ctx.GetParamString("mysql_home", ""))
			return replicaSoftwarePlan{Version: explicitVer, Home: home, Package: explicitPkg, Source: "installed"}, nil
		}
		if explicitPkg != "" {
			pkgVer, err := commonfile.ParseMysqlVersionFromPackage(explicitPkg)
			if err != nil {
				return replicaSoftwarePlan{}, err
			}
			if pkgVer != explicitVer {
				return replicaSoftwarePlan{}, fmt.Errorf("mysql_package version %s does not match --mysql-version %s", pkgVer, explicitVer)
			}
			return replicaSoftwarePlan{Version: explicitVer, Package: explicitPkg, Source: "package"}, nil
		}
		if pkg, err := findMysqlPackageForVersion(ctx, explicitVer); err == nil && pkg != "" {
			return replicaSoftwarePlan{Version: explicitVer, Package: pkg, Source: "package"}, nil
		}
		return replicaSoftwarePlan{Version: explicitVer, Source: "explicit"}, nil
	}

	if explicitPkg != "" {
		pkgVer, err := commonfile.ParseMysqlVersionFromPackage(explicitPkg)
		if err != nil {
			return replicaSoftwarePlan{}, err
		}
		if err := validate(pkgVer); err != nil {
			return replicaSoftwarePlan{}, err
		}
		return replicaSoftwarePlan{Version: pkgVer, Package: explicitPkg, Source: "package"}, nil
	}

	installed, err := listInstalledMysqlSoftware(ctx, base)
	if err != nil {
		return replicaSoftwarePlan{}, err
	}
	if sel := pickLowestSoftwareAtLeast(installed, primaryNorm); sel.Version != "" {
		return replicaSoftwarePlan{Version: sel.Version, Home: sel.Home, Source: "installed"}, nil
	}

	if primaryNorm == "" {
		return replicaSoftwarePlan{}, nil
	}

	remoteDir := replicaSoftDir(ctx)
	arch := ctx.GetParamString("mysql_target_arch", "")
	pkg, err := commonfile.FindMysqlBinaryPackageAtLeastVersion(ctx, ctx.LocalSoftwareDirs, remoteDir, replicaPlatform(ctx), arch, primaryNorm)
	if err != nil {
		return replicaSoftwarePlan{}, err
	}
	pkgVer, err := commonfile.ParseMysqlVersionFromPackage(pkg)
	if err != nil {
		return replicaSoftwarePlan{}, err
	}
	if err := validate(pkgVer); err != nil {
		return replicaSoftwarePlan{}, err
	}
	return replicaSoftwarePlan{Version: pkgVer, Package: pkg, Source: "package"}, nil
}

func listInstalledMysqlSoftware(ctx *runner.StepContext, base string) ([]detectedMysqlSoftware, error) {
	var out []detectedMysqlSoftware
	seen := make(map[string]bool)

	if ver, home, ok, err := commonmysql.DetectInstalledSoftwareViaClient(ctx, commonmysql.Layout{}); err != nil {
		return nil, err
	} else if ok {
		out = append(out, detectedMysqlSoftware{Version: ver, Home: home, Source: "client"})
		seen[ver+"|"+home] = true
	}

	for _, sw := range scanStandardProductSoftware(ctx, base) {
		key := sw.Version + "|" + sw.Home
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, sw)
	}
	return out, nil
}

func scanStandardProductSoftware(ctx *runner.StepContext, base string) []detectedMysqlSoftware {
	platform := replicaPlatform(ctx)
	base = strings.TrimRight(strings.ReplaceAll(base, `\`, `/`), "/")
	var paths []string

	if platform == commonmysql.PlatformWindows {
		baseWin := strings.ReplaceAll(base, `'`, `''`)
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "Get-ChildItem -Path '%s/product/*/dbhome_1/bin/mysqld.exe' -ErrorAction SilentlyContinue | ForEach-Object { $_.FullName }"`, baseWin)
		res, _ := ctx.Execute(cmd, false)
		if res != nil {
			paths = strings.Split(strings.TrimSpace(res.GetStdout()), "\n")
		}
	} else {
		cmd := fmt.Sprintf("ls -1 %s/product/*/dbhome_1/bin/mysqld 2>/dev/null", shellQuote(base))
		res, _ := ctx.Execute(cmd, false)
		if res != nil {
			paths = strings.Split(strings.TrimSpace(res.GetStdout()), "\n")
		}
	}

	var out []detectedMysqlSoftware
	for _, line := range paths {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ver := versionFromProductPath(line)
		if ver == "" {
			continue
		}
		home := filepath.ToSlash(strings.TrimSpace(line))
		home = strings.TrimSuffix(home, "/bin/mysqld")
		home = strings.TrimSuffix(home, "/bin/mysqld.exe")
		out = append(out, detectedMysqlSoftware{Version: ver, Home: home, Source: "product"})
	}
	return out
}

func versionFromProductPath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if part == "product" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func pickLowestSoftwareAtLeast(items []detectedMysqlSoftware, minVersion string) detectedMysqlSoftware {
	if len(items) == 0 {
		return detectedMysqlSoftware{}
	}
	type ranked struct {
		sw  detectedMysqlSoftware
		ver commonmysql.Version
	}
	var ok []ranked
	for _, sw := range items {
		v, err := commonmysql.ParseMySQLVersion(sw.Version)
		if err != nil {
			continue
		}
		if minVersion != "" {
			good, err := commonmysql.ReplicaVersionOK(sw.Version, minVersion)
			if err != nil || !good {
				continue
			}
		}
		ok = append(ok, ranked{sw: sw, ver: v})
	}
	if len(ok) == 0 {
		return detectedMysqlSoftware{}
	}
	sort.Slice(ok, func(i, j int) bool {
		a, b := ok[i].ver, ok[j].ver
		if a.Major != b.Major {
			return a.Major < b.Major
		}
		if a.Minor != b.Minor {
			return a.Minor < b.Minor
		}
		return a.Patch < b.Patch
	})
	return ok[0].sw
}

func softwareExistsForVersion(ctx *runner.StepContext, base, version string) bool {
	if mysqldExistsAtStandardHome(ctx, base, version) {
		return true
	}
	if ver, home, ok, _ := commonmysql.DetectInstalledSoftwareViaClient(ctx, commonmysql.Layout{}); ok {
		return ver == version && commonmysql.MysqldExistsAtHome(ctx, home, replicaPlatform(ctx))
	}
	return false
}

func mysqldExistsAtStandardHome(ctx *runner.StepContext, base, version string) bool {
	layout := commonmysql.LayoutFromParams(replicaPlatform(ctx), base, replicaPort(ctx), version)
	if strings.TrimSpace(layout.Home) == "" {
		return false
	}
	platform := replicaPlatform(ctx)
	if platform == commonmysql.PlatformWindows {
		home := strings.ReplaceAll(layout.Home, `'`, `''`)
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "Test-Path -LiteralPath '%s/bin/mysqld.exe'"`, home)
		res, _ := ctx.Execute(cmd, false)
		return res != nil && res.GetExitCode() == 0 && strings.EqualFold(strings.TrimSpace(res.GetStdout()), "True")
	}
	cmd := fmt.Sprintf("test -x %s/bin/mysqld", shellQuote(layout.Home))
	res, _ := ctx.Execute(cmd, false)
	return res != nil && res.GetExitCode() == 0
}

func applyReplicaSoftwarePlan(ctx *runner.StepContext, plan replicaSoftwarePlan) {
	if plan.Version != "" {
		ctx.Params["mysql_version"] = plan.Version
		ctx.SetResult("replica_mysql_version", plan.Version)
	}
	if plan.Home != "" {
		ctx.Params["mysql_home"] = plan.Home
		ctx.SetResult("replica_mysql_home", plan.Home)
	}
	if plan.Package != "" {
		ctx.Params["mysql_package"] = plan.Package
		ctx.SetResult("mysql_package", plan.Package)
	}
	if plan.Source != "" {
		ctx.SetResult("replica_software_source", plan.Source)
	}
}

func findMysqlPackageForVersion(ctx *runner.StepContext, wantVersion string) (string, error) {
	wantVersion = strings.TrimSpace(wantVersion)
	if wantVersion == "" {
		return "", fmt.Errorf("empty version")
	}
	remoteDir := replicaSoftDir(ctx)
	arch := ctx.GetParamString("mysql_target_arch", "")
	all, _, err := commonfile.CollectMysqlBinaryPackages(ctx, ctx.LocalSoftwareDirs, remoteDir, replicaPlatform(ctx), arch)
	if err != nil {
		return "", err
	}
	for _, pkg := range all {
		ver, verr := commonfile.ParseMysqlVersionFromPackage(pkg)
		if verr != nil {
			continue
		}
		if ver == wantVersion {
			return pkg, nil
		}
	}
	return "", fmt.Errorf("no mysql package for version %s", wantVersion)
}
