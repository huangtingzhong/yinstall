package file

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/runner"
)

const (
	MysqlInstallBinary = "binary"
	MysqlInstallSource = "source"
)

var (
	mysqlSourcePackageRE = regexp.MustCompile(`^mysql-(\d+\.\d+\.\d+)\.tar\.gz$`)
	mysqlPackageGlibcRE  = regexp.MustCompile(`-linux-glibc([\d.]+)-`)
	glibcProbeRE         = regexp.MustCompile(`(?i)(?:glibc|GNU libc)\)?\s*([\d]+(?:\.[\d]+)*)`)
)

// IsMysqlSourcePackage reports whether filename is a MySQL source tarball (mysql-X.Y.Z.tar.gz).
func IsMysqlSourcePackage(filename string) bool {
	base := path.Base(filepath.ToSlash(filename))
	return mysqlSourcePackageRE.MatchString(base)
}

// ParseMysqlVersionFromPackage extracts VERSION from a mysql package filename.
func ParseMysqlVersionFromPackage(filename string) (string, error) {
	base := path.Base(filepath.ToSlash(filename))
	patterns := []string{
		`^mysql-(\d+\.\d+\.\d+)\.tar\.gz$`,
		`mysql-(\d+\.\d+\.\d+)-`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(base); len(m) > 1 {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("cannot parse MySQL version from package name %q", base)
}

func mysqlBinaryPackagePattern(platform, arch string) (*regexp.Regexp, string, error) {
	switch platform {
	case "windows":
		return regexp.MustCompile(`mysql-(\d+\.\d+\.\d+)-winx64\.zip`), "mysql-*-winx64.zip", nil
	case "darwin":
		if arch == "arm64" {
			return regexp.MustCompile(`mysql-(\d+\.\d+\.\d+)-macos\d*-arm64\.tar\.gz`), "mysql-*-macos*-arm64.tar.gz", nil
		}
		return regexp.MustCompile(`mysql-(\d+\.\d+\.\d+)-macos\d*-x86_64\.tar\.gz`), "mysql-*-macos*-x86_64.tar.gz", nil
	case "linux":
		linuxArch := `(?:x86_64|x86-64)`
		glob := "mysql-*-linux-*.tar.*"
		if arch == "aarch64" || arch == "arm64" {
			linuxArch = `(?:aarch64|aarch-64|arm64)`
		}
		re := regexp.MustCompile(fmt.Sprintf(
			`mysql-(\d+\.\d+\.\d+)-(?:el\d+-)?linux(?:-glibc[\d.]+)?-%s\.(?:tar\.gz|tar\.xz)`,
			linuxArch,
		))
		return re, glob, nil
	default:
		return nil, "", fmt.Errorf("unsupported platform %q for mysql package search", platform)
	}
}

// ResolveMysqlPackage selects binary or source package and install mode.
// Explicit source tarball → source; explicit binary → binary; auto: binary first, else source.
func ResolveMysqlPackage(ctx *runner.StepContext, localDirs []string, remoteDir, platform, arch, explicitPkg string) (pkg, mode string, err error) {
	if explicitPkg != "" {
		if IsMysqlSourcePackage(explicitPkg) {
			return explicitPkg, MysqlInstallSource, nil
		}
		version, verr := ParseMysqlVersionFromPackage(explicitPkg)
		if verr != nil {
			return "", "", verr
		}
		_ = version
		if platform == "linux" {
			if err := validateLinuxBinaryGlibc(ctx, explicitPkg); err != nil {
				return "", "", err
			}
		}
		return explicitPkg, MysqlInstallBinary, nil
	}

	binaryPkg, berr := FindLatestMysqlBinaryPackage(ctx, localDirs, remoteDir, platform, arch)
	if berr == nil {
		return binaryPkg, MysqlInstallBinary, nil
	}

	sourcePkg, serr := FindLatestMysqlSourcePackage(ctx, localDirs, remoteDir)
	if serr == nil {
		if ctx != nil && ctx.Logger != nil {
			ctx.Logger.Info("No binary MySQL package for platform=%s arch=%s; using source package %s", platform, arch, sourcePkg)
		}
		return sourcePkg, MysqlInstallSource, nil
	}

	return "", "", fmt.Errorf("no mysql binary package (%v) and no source package (%v)", berr, serr)
}

// FindLatestMysqlBinaryPackage finds the newest platform-specific binary package.
func FindLatestMysqlBinaryPackage(ctx *runner.StepContext, localDirs []string, remoteDir, platform, arch string) (string, error) {
	return findMysqlPackages(ctx, localDirs, remoteDir, platform, arch, false)
}

// FindMysqlBinaryPackageAtLeastVersion finds the newest binary package with version >= minVersion.
func FindMysqlBinaryPackageAtLeastVersion(ctx *runner.StepContext, localDirs []string, remoteDir, platform, arch, minVersion string) (string, error) {
	minVersion = strings.TrimSpace(minVersion)
	if minVersion == "" {
		return FindLatestMysqlBinaryPackage(ctx, localDirs, remoteDir, platform, arch)
	}
	all, re, err := collectMysqlBinaryPackages(ctx, localDirs, remoteDir, platform, arch)
	if err != nil {
		return "", err
	}
	var filtered []string
	for _, f := range all {
		ver, verr := ParseMysqlVersionFromPackage(f)
		if verr != nil {
			continue
		}
		ok, cmpErr := commonmysql.ReplicaVersionOK(ver, minVersion)
		if cmpErr != nil || !ok {
			continue
		}
		filtered = append(filtered, f)
	}
	if len(filtered) == 0 {
		return "", fmt.Errorf("no mysql binary package >= %s found for platform=%s arch=%s", minVersion, platform, arch)
	}
	if platform == "linux" {
		return selectBestMysqlLinuxBinaryPackage(ctx, filtered, re)
	}
	return findLatestVersion(filtered, re), nil
}

func collectMysqlBinaryPackages(ctx *runner.StepContext, localDirs []string, remoteDir, platform, arch string) ([]string, *regexp.Regexp, error) {
	if platform == "" {
		platform = "linux"
	}
	if arch == "" {
		arch = detectRemoteArch(ctx, platform)
	}
	re, _, err := mysqlBinaryPackagePattern(platform, arch)
	if err != nil {
		return nil, nil, err
	}

	var remotePackages []string
	for _, dir := range remoteSearchDirs(ctx, remoteDir) {
		if !remoteSoftwareDirExists(ctx, dir) {
			continue
		}
		for _, f := range listRemoteMysqlPackagePaths(ctx, platform, dir, false) {
			base := path.Base(f)
			if IsMysqlSourcePackage(base) {
				continue
			}
			if re.MatchString(base) {
				remotePackages = append(remotePackages, f)
			}
		}
	}

	var localPackages []string
	for _, dir := range localDirs {
		if !localSoftwareDirExists(dir) {
			continue
		}
		for _, localGlob := range localMysqlGlobs(platform, arch, false) {
			matches, globErr := filepath.Glob(filepath.Join(dir, localGlob))
			if globErr != nil {
				continue
			}
			for _, m := range matches {
				base := filepath.Base(m)
				if IsMysqlSourcePackage(base) {
					continue
				}
				if re.MatchString(base) {
					localPackages = append(localPackages, m)
				}
			}
		}
	}

	all := append(append([]string{}, remotePackages...), localPackages...)
	if len(all) == 0 {
		return nil, re, fmt.Errorf("no mysql binary package found for platform=%s arch=%s in remote or local dirs", platform, arch)
	}
	return all, re, nil
}

// CollectMysqlBinaryPackages lists matching mysql binary packages in local/remote software dirs.
func CollectMysqlBinaryPackages(ctx *runner.StepContext, localDirs []string, remoteDir, platform, arch string) ([]string, *regexp.Regexp, error) {
	return collectMysqlBinaryPackages(ctx, localDirs, remoteDir, platform, arch)
}

// FindLatestMysqlSourcePackage finds the newest mysql-X.Y.Z.tar.gz source tarball.
func FindLatestMysqlSourcePackage(ctx *runner.StepContext, localDirs []string, remoteDir string) (string, error) {
	return findMysqlPackages(ctx, localDirs, remoteDir, "linux", "", true)
}

// FindLatestMysqlPackage is kept for compatibility; prefers binary packages.
func FindLatestMysqlPackage(ctx *runner.StepContext, localDirs []string, remoteDir, platform, arch string) (string, error) {
	pkg, mode, err := ResolveMysqlPackage(ctx, localDirs, remoteDir, platform, arch, "")
	if err != nil {
		return "", err
	}
	if mode == MysqlInstallSource && ctx != nil && ctx.Logger != nil {
		ctx.Logger.Info("FindLatestMysqlPackage resolved to source install: %s", pkg)
	}
	return pkg, nil
}

func findMysqlPackages(ctx *runner.StepContext, localDirs []string, remoteDir, platform, arch string, sourceOnly bool) (string, error) {
	if platform == "" {
		platform = "linux"
	}
	if !sourceOnly && arch == "" {
		arch = detectRemoteArch(ctx, platform)
	}

	var re *regexp.Regexp
	if sourceOnly {
		re = mysqlSourcePackageRE
	} else {
		var err error
		re, _, err = mysqlBinaryPackagePattern(platform, arch)
		if err != nil {
			return "", err
		}
	}

	var remotePackages []string
	for _, dir := range remoteSearchDirs(ctx, remoteDir) {
		if !remoteSoftwareDirExists(ctx, dir) {
			continue
		}
		for _, f := range listRemoteMysqlPackagePaths(ctx, platform, dir, sourceOnly) {
			base := path.Base(f)
			if sourceOnly {
				if re.MatchString(base) {
					remotePackages = append(remotePackages, f)
				}
				continue
			}
			if IsMysqlSourcePackage(base) {
				continue
			}
			if re.MatchString(base) {
				remotePackages = append(remotePackages, f)
			}
		}
	}

	var localPackages []string
	for _, dir := range localDirs {
		if !localSoftwareDirExists(dir) {
			continue
		}
		for _, localGlob := range localMysqlGlobs(platform, arch, sourceOnly) {
			matches, err := filepath.Glob(filepath.Join(dir, localGlob))
			if err != nil {
				continue
			}
			for _, m := range matches {
				base := filepath.Base(m)
				if sourceOnly {
					if re.MatchString(base) {
						localPackages = append(localPackages, m)
					}
					continue
				}
				if IsMysqlSourcePackage(base) {
					continue
				}
				if re.MatchString(base) {
					localPackages = append(localPackages, m)
				}
			}
		}
	}

	all := append(append([]string{}, remotePackages...), localPackages...)
	if len(all) == 0 {
		if sourceOnly {
			return "", fmt.Errorf("no mysql source package (mysql-X.Y.Z.tar.gz) found in remote or local dirs")
		}
		return "", fmt.Errorf("no mysql binary package found for platform=%s arch=%s in remote or local dirs", platform, arch)
	}
	if platform == "linux" && !sourceOnly {
		return selectBestMysqlLinuxBinaryPackage(ctx, all, re)
	}
	return findLatestVersion(all, re), nil
}

func remoteMysqlFileGlobs(platform string, sourceOnly bool) []string {
	if sourceOnly {
		return []string{"mysql-*.tar.gz"}
	}
	switch platform {
	case "windows":
		return []string{"mysql-*-winx64.zip"}
	case "linux":
		return []string{"mysql-*.tar.gz", "mysql-*.tar.xz"}
	default:
		return []string{"mysql-*.tar.gz"}
	}
}

func listRemoteMysqlPackagePaths(ctx *runner.StepContext, platform, dir string, sourceOnly bool) []string {
	var out []string
	for _, glob := range remoteMysqlFileGlobs(platform, sourceOnly) {
		out = append(out, remoteGlobInDir(ctx, platform, dir, glob)...)
	}
	return out
}

func remoteGlobInDir(ctx *runner.StepContext, platform, dir, glob string) []string {
	dir = strings.TrimSpace(dir)
	glob = strings.TrimSpace(glob)
	if dir == "" || glob == "" || ctx == nil {
		return nil
	}
	if isWindowsRemote(ctx, platform) {
		dirQ := psQuotePath(toSlashPath(dir))
		globQ := strings.ReplaceAll(glob, `'`, `''`)
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "Get-ChildItem -Path '%s/%s' -ErrorAction SilentlyContinue | ForEach-Object { $_.FullName }"`, dirQ, globQ)
		result, _ := ctx.Execute(cmd, false)
		return splitNonEmptyLines(result)
	}
	cmd := fmt.Sprintf("ls -1 %s/%s 2>/dev/null || true", dir, glob)
	result, _ := ctx.Execute(cmd, false)
	return splitNonEmptyLines(result)
}

func isWindowsRemote(ctx *runner.StepContext, platform string) bool {
	if ctx.GetTargetPlatform() == "windows" {
		return true
	}
	return platform == "windows"
}

func splitNonEmptyLines(result runner.ExecResult) []string {
	if result == nil || strings.TrimSpace(result.GetStdout()) == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// remoteLsPattern is kept for reference; prefer listRemoteMysqlPackagePaths.
func remoteLsPattern(platform, dir string, sourceOnly bool) string {
	if sourceOnly {
		return fmt.Sprintf("ls -1 %s/mysql-*.tar.gz 2>/dev/null || true", dir)
	}
	switch platform {
	case "windows":
		return fmt.Sprintf("ls -1 %s/mysql-*-winx64.zip 2>/dev/null || true", dir)
	case "linux":
		return fmt.Sprintf("ls -1 %s/mysql-*.tar.gz %s/mysql-*.tar.xz 2>/dev/null || true", dir, dir)
	default:
		return fmt.Sprintf("ls -1 %s/mysql-*.tar.gz 2>/dev/null || true", dir)
	}
}

func detectRemoteArch(ctx *runner.StepContext, platform string) string {
	if platform == "windows" {
		return "winx64"
	}
	result, _ := ctx.Execute("uname -m 2>/dev/null || echo unknown", false)
	if result != nil {
		arch := strings.TrimSpace(result.GetStdout())
		if arch != "" && arch != "unknown" {
			return arch
		}
	}
	if runtime.GOARCH != "" {
		return runtime.GOARCH
	}
	return "x86_64"
}

func localMysqlGlobs(platform, arch string, sourceOnly bool) []string {
	if sourceOnly {
		return []string{"mysql-*.tar.gz"}
	}
	switch platform {
	case "windows":
		return []string{"mysql-*-winx64.zip"}
	case "darwin":
		if arch == "arm64" || arch == "aarch64" {
			return []string{"mysql-*-macos*-arm64.tar.gz"}
		}
		return []string{"mysql-*-macos*-x86_64.tar.gz"}
	default:
		return []string{"mysql-*.tar.gz", "mysql-*.tar.xz"}
	}
}

// parseMysqlPackageGlibc extracts glibc requirement from a Linux binary package basename.
// Returns (encodedVersion, true) when -linux-glibcX.Y- is present; legacy names return (_, false).
func parseMysqlPackageGlibc(baseName string) (int, bool) {
	base := path.Base(filepath.ToSlash(baseName))
	m := mysqlPackageGlibcRE.FindStringSubmatch(base)
	if len(m) < 2 {
		return 0, false
	}
	v, ok := encodeGlibcVersion(m[1])
	return v, ok
}

// parseGlibcProbeOutput parses getconf GNU_LIBC_VERSION or ldd --version first line.
func parseGlibcProbeOutput(stdout string) (int, bool) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := glibcProbeRE.FindStringSubmatch(line)
		if len(m) >= 2 {
			return encodeGlibcVersion(m[1])
		}
	}
	return 0, false
}

func encodeGlibcVersion(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return 0, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || major < 0 || minor < 0 {
		return 0, false
	}
	return major*1000 + minor, true
}

func formatGlibcInt(v int) string {
	if v <= 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d.%d", v/1000, v%1000)
}

func detectHostGlibc(ctx *runner.StepContext) (int, bool) {
	if ctx == nil || ctx.Executor == nil {
		return 0, false
	}
	res, _ := ctx.Execute("getconf GNU_LIBC_VERSION 2>/dev/null || ldd --version 2>&1 | head -1", false)
	if res == nil {
		return 0, false
	}
	out := strings.TrimSpace(res.GetStdout())
	if out == "" {
		return 0, false
	}
	return parseGlibcProbeOutput(out)
}

func validateLinuxBinaryGlibc(ctx *runner.StepContext, packagePath string) error {
	base := path.Base(filepath.ToSlash(packagePath))
	pkgGlibc, tagged := parseMysqlPackageGlibc(base)
	if !tagged {
		return nil
	}
	hostGlibc, ok := detectHostGlibc(ctx)
	if !ok {
		if ctx != nil && ctx.Logger != nil {
			ctx.Logger.Warn("cannot detect host glibc; skipping glibc compatibility check for %s", base)
		}
		return nil
	}
	if pkgGlibc > hostGlibc {
		return fmt.Errorf("mysql binary package %q requires glibc %s but host provides glibc %s",
			base, formatGlibcInt(pkgGlibc), formatGlibcInt(hostGlibc))
	}
	return nil
}

type mysqlLinuxCandidate struct {
	path     string
	mysqlVer []int
	glibc    int
	tagged   bool
}

func parseVersionParts(versionStr string) []int {
	parts := strings.Split(versionStr, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func compareVersionSlices(a, b []int) int {
	for k := 0; k < len(a) && k < len(b); k++ {
		if a[k] != b[k] {
			return a[k] - b[k]
		}
	}
	return len(a) - len(b)
}

// selectBestMysqlLinuxBinaryPackage picks the newest MySQL version whose package glibc <= host glibc.
func selectBestMysqlLinuxBinaryPackage(ctx *runner.StepContext, files []string, re *regexp.Regexp) (string, error) {
	hostGlibc, hostKnown := detectHostGlibc(ctx)
	if hostKnown && ctx != nil && ctx.Logger != nil {
		ctx.Logger.Info("host glibc detected: %s", formatGlibcInt(hostGlibc))
	}
	selected, err := selectBestMysqlLinuxBinaryFromHost(files, re, hostGlibc, hostKnown)
	if err != nil {
		return "", err
	}
	if ctx != nil && ctx.Logger != nil && hostKnown {
		base := path.Base(filepath.ToSlash(selected))
		glibc, tagged := parseMysqlPackageGlibc(base)
		if tagged {
			ctx.Logger.Info("selected mysql binary package %s (package glibc %s, host glibc %s)",
				base, formatGlibcInt(glibc), formatGlibcInt(hostGlibc))
		} else {
			ctx.Logger.Info("selected mysql binary package %s (legacy name without glibc tag, host glibc %s)",
				base, formatGlibcInt(hostGlibc))
		}
	}
	return selected, nil
}

func selectBestMysqlLinuxBinaryFromHost(files []string, re *regexp.Regexp, hostGlibc int, hostKnown bool) (string, error) {
	var candidates []mysqlLinuxCandidate
	skippedIncompatible := 0
	for _, f := range files {
		base := path.Base(filepath.ToSlash(f))
		matches := re.FindStringSubmatch(base)
		if len(matches) < 2 {
			continue
		}
		glibc, tagged := parseMysqlPackageGlibc(base)
		if hostKnown && tagged && glibc > hostGlibc {
			skippedIncompatible++
			continue
		}
		candidates = append(candidates, mysqlLinuxCandidate{
			path:     f,
			mysqlVer: parseVersionParts(matches[1]),
			glibc:    glibc,
			tagged:   tagged,
		})
	}

	if len(candidates) == 0 {
		if hostKnown {
			return "", fmt.Errorf("no mysql binary package compatible with host glibc %s (%d package(s) require newer glibc)",
				formatGlibcInt(hostGlibc), skippedIncompatible)
		}
		return findLatestVersion(files, re), nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		ci, cj := candidates[i], candidates[j]
		if cmp := compareVersionSlices(ci.mysqlVer, cj.mysqlVer); cmp != 0 {
			return cmp > 0
		}
		if ci.tagged != cj.tagged {
			return ci.tagged
		}
		return ci.glibc > cj.glibc
	})
	return candidates[0].path, nil
}
