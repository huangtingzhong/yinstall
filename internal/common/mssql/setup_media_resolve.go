package mssql

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

const (
	SetupMediaKindUNC = "unc"
	SetupMediaKindISO = "iso"
	SetupMediaKindDir = "dir"
)

var (
	sqlServerISOVersionRE = regexp.MustCompile(`(?i)sql[_-]?server[_-]?(\d{4})`)
	sqlServerISONameRE    = regexp.MustCompile(`(?i)(sql[_-]?server|sqlserver)`)
)

// IsSQLServerSetupISO reports whether a filename looks like SQL Server installation media.
func IsSQLServerSetupISO(name string) bool {
	name = strings.TrimSpace(filepath.Base(name))
	return name != "" && sqlServerISONameRE.MatchString(name)
}

// DefaultRemoteSoftwareDirWindows is the default -R target on Windows when not specified.
const DefaultRemoteSoftwareDirWindows = `D:\soft`

// DefaultRemoteSoftwareDir returns the default remote software directory for MSSQL media upload (-R).
func DefaultRemoteSoftwareDir() string {
	return DefaultRemoteSoftwareDirWindows
}

// RemoteSoftwareDir returns ctx.RemoteSoftwareDir (-R) or the MSSQL Windows default.
func RemoteSoftwareDir(ctx *runner.StepContext) string {
	if ctx != nil {
		if rd := strings.TrimSpace(ctx.RemoteSoftwareDir); rd != "" {
			return strings.TrimRight(normalizeWinPath(rd), `\`)
		}
	}
	return DefaultRemoteSoftwareDir()
}

// SetupMediaLocation describes resolved setup media before MS-006 upload/mount.
type SetupMediaLocation struct {
	Kind       string // unc | iso | dir
	UNCPath    string
	LocalPath  string // local ISO file or directory tree (control plane)
	RemotePath string // remote ISO file or directory with setup.exe
}

// ResolveAndStoreSetupMedia locates setup media (remote → local) and stores results for MS-006.
func ResolveAndStoreSetupMedia(ctx *runner.StepContext) error {
	loc, err := LocateSetupMedia(ctx)
	if err != nil {
		return err
	}
	storeSetupMediaResults(ctx, loc)
	switch loc.Kind {
	case SetupMediaKindUNC:
		ctx.Logger.Info("MS-004: using UNC setup media %s", loc.UNCPath)
	case SetupMediaKindISO:
		if loc.RemotePath != "" {
			ctx.Logger.Info("MS-004: found remote ISO %s", loc.RemotePath)
		} else {
			ctx.Logger.Info("MS-004: found local ISO %s (will upload in MS-006)", loc.LocalPath)
		}
	case SetupMediaKindDir:
		if loc.RemotePath != "" {
			ctx.Logger.Info("MS-004: found remote setup directory %s", loc.RemotePath)
		} else {
			ctx.Logger.Info("MS-004: found local setup directory %s (will upload in MS-006)", loc.LocalPath)
		}
	}
	return nil
}

func storeSetupMediaResults(ctx *runner.StepContext, loc SetupMediaLocation) {
	ctx.SetResult("mssql_setup_media_kind", loc.Kind)
	ctx.SetResult("mssql_setup_local_path", loc.LocalPath)
	ctx.SetResult("mssql_setup_remote_path", loc.RemotePath)
	switch loc.Kind {
	case SetupMediaKindUNC:
		ctx.SetResult("mssql_setup_root", loc.UNCPath)
	default:
		if loc.RemotePath != "" {
			ctx.SetResult("mssql_setup_root", loc.RemotePath)
		} else if loc.LocalPath != "" {
			ctx.SetResult("mssql_setup_root", loc.LocalPath)
		}
	}
	explicit := ""
	if ctx != nil {
		explicit = ctx.GetParamString("mssql_setup_package", "")
	}
	if major := inferSetupProductMajor(loc, explicit); major > 0 {
		ctx.SetResult("mssql_setup_product_major", major)
		if ctx.Logger != nil {
			ctx.Logger.Info("MS-004: setup product major=%d", major)
		}
	}
}

// inferSetupProductMajor derives ProductMajorVersion from setup media path (ISO release year).
func inferSetupProductMajor(loc SetupMediaLocation, explicit string) int {
	for _, ref := range []string{explicit, loc.LocalPath, loc.RemotePath, loc.UNCPath} {
		if major := productMajorFromMediaRef(ref); major > 0 {
			return major
		}
	}
	return 0
}

func productMajorFromMediaRef(ref string) int {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0
	}
	year := parseSQLServerISOVersion(filepath.Base(ref))
	if year == 0 {
		return 0
	}
	major, ok := SQLMajorFromReleaseYear(year)
	if !ok {
		return 0
	}
	return major
}

// LocateSetupMedia finds SQL Server setup media without uploading or mounting.
func LocateSetupMedia(ctx *runner.StepContext) (SetupMediaLocation, error) {
	unc := strings.TrimSpace(ctx.GetParamString("mssql_setup_unc", ""))
	if unc != "" {
		return SetupMediaLocation{Kind: SetupMediaKindUNC, UNCPath: unc}, nil
	}

	explicit := strings.TrimSpace(ctx.GetParamString("mssql_setup_package", ""))
	remoteDir := RemoteSoftwareDir(ctx)
	localDirs := ctx.LocalSoftwareDirs

	if explicit != "" {
		if loc, ok, err := locateExplicitSetupMedia(ctx, explicit, localDirs, remoteDir); err != nil {
			return SetupMediaLocation{}, err
		} else if ok {
			return loc, nil
		}
	}

	if canProbeRemoteMedia(ctx) {
		if loc, ok := locateRemoteSetupMedia(ctx, remoteSearchDirs(ctx, remoteDir)); ok {
			return loc, nil
		}
	}
	if loc, ok := locateLocalSetupMedia(localDirs); ok {
		return loc, nil
	}

	return SetupMediaLocation{}, fmt.Errorf(
		"no MSSQL setup media found; place a SQL Server *.iso (name contains sql_server/sqlserver) or setup.exe under remote -R/%s or local -L, or use --mssql-setup-package / --mssql-setup-unc",
		remoteDir,
	)
}

func remoteSearchDirs(ctx *runner.StepContext, remoteDir string) []string {
	home := ""
	if canProbeRemoteMedia(ctx) {
		home = commonfile.RemoteHomeDir(ctx)
	}
	seen := map[string]bool{}
	var dirs []string
	for _, d := range []string{remoteDir, home} {
		d = strings.TrimSpace(d)
		if d == "" || seen[strings.ToLower(d)] {
			continue
		}
		seen[strings.ToLower(d)] = true
		dirs = append(dirs, d)
	}
	return dirs
}

func canProbeRemoteMedia(ctx *runner.StepContext) bool {
	return ctx != nil && ctx.Executor != nil
}

func locateExplicitSetupMedia(ctx *runner.StepContext, explicit string, localDirs []string, remoteDir string) (SetupMediaLocation, bool, error) {
	if commonfile.IsISOFile(explicit) {
		if loc, ok := locateISOPath(ctx, explicit, localDirs, remoteDir); ok {
			return loc, true, nil
		}
		return SetupMediaLocation{}, false, fmt.Errorf("ISO not found: %s", explicit)
	}

	if loc, ok := locateDirectoryMedia(ctx, explicit, localDirs, remoteDir); ok {
		return loc, true, nil
	}
	if filepath.IsAbs(explicit) || strings.HasPrefix(explicit, "/") || strings.Contains(explicit, `\`) {
		return SetupMediaLocation{}, false, fmt.Errorf("setup directory not found or missing setup.exe: %s", explicit)
	}
	return SetupMediaLocation{}, false, nil
}

func isWindowsDrivePath(p string) bool {
	p = strings.TrimSpace(p)
	return len(p) >= 2 && p[1] == ':'
}

func winPathBaseName(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func locateISOPath(ctx *runner.StepContext, isoRef string, localDirs []string, remoteDir string) (SetupMediaLocation, bool) {
	normalized := filepath.ToSlash(isoRef)
	baseName := winPathBaseName(normalized)

	if isWindowsDrivePath(normalized) {
		remotePath := normalizeWinPath(isoRef)
		if canProbeRemoteMedia(ctx) && commonfile.FileExists(ctx, remotePath) {
			return SetupMediaLocation{Kind: SetupMediaKindISO, RemotePath: remotePath}, true
		}
	}

	if filepath.IsAbs(isoRef) {
		if st, err := os.Stat(isoRef); err == nil && !st.IsDir() && commonfile.IsISOFile(isoRef) {
			return SetupMediaLocation{Kind: SetupMediaKindISO, LocalPath: isoRef}, true
		}
	}

	for _, remotePath := range remoteISOCandidates(ctx, remoteDir, baseName) {
		if canProbeRemoteMedia(ctx) && commonfile.FileExists(ctx, remotePath) {
			return SetupMediaLocation{Kind: SetupMediaKindISO, RemotePath: remotePath}, true
		}
	}

	for _, dir := range localDirs {
		candidate := filepath.Join(dir, baseName)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return SetupMediaLocation{Kind: SetupMediaKindISO, LocalPath: candidate}, true
		}
	}
	if st, err := os.Stat(isoRef); err == nil && !st.IsDir() && commonfile.IsISOFile(isoRef) {
		return SetupMediaLocation{Kind: SetupMediaKindISO, LocalPath: isoRef}, true
	}
	return SetupMediaLocation{}, false
}

func remoteISOCandidates(ctx *runner.StepContext, remoteDir, baseName string) []string {
	home := ""
	if canProbeRemoteMedia(ctx) {
		home = commonfile.RemoteHomeDir(ctx)
	}
	var out []string
	if remoteDir != "" {
		out = append(out, joinWinPath(remoteDir, baseName))
	}
	if home != "" {
		out = append(out, joinWinPath(home, baseName))
	}
	return out
}

func locateDirectoryMedia(ctx *runner.StepContext, dirRef string, localDirs []string, remoteDir string) (SetupMediaLocation, bool) {
	if canProbeRemoteMedia(ctx) && remoteSetupRoot(ctx, dirRef) {
		return SetupMediaLocation{Kind: SetupMediaKindDir, RemotePath: strings.TrimRight(dirRef, `\`)}, true
	}

	if filepath.IsAbs(dirRef) {
		if localDirHasSetup(dirRef) {
			return SetupMediaLocation{Kind: SetupMediaKindDir, LocalPath: dirRef}, true
		}
	}

	baseName := filepath.Base(filepath.ToSlash(dirRef))
	for _, dir := range localDirs {
		candidate := filepath.Join(dir, dirRef)
		if localDirHasSetup(candidate) {
			return SetupMediaLocation{Kind: SetupMediaKindDir, LocalPath: candidate}, true
		}
		candidate = filepath.Join(dir, baseName)
		if localDirHasSetup(candidate) {
			return SetupMediaLocation{Kind: SetupMediaKindDir, LocalPath: candidate}, true
		}
	}
	return SetupMediaLocation{}, false
}

func locateRemoteSetupMedia(ctx *runner.StepContext, dirs []string) (SetupMediaLocation, bool) {
	if len(dirs) == 0 {
		return SetupMediaLocation{}, false
	}
	if iso, ok := findRemoteISO(ctx, dirs); ok {
		return SetupMediaLocation{Kind: SetupMediaKindISO, RemotePath: iso}, true
	}
	if root, ok := findRemoteSetupDir(ctx, dirs); ok {
		return SetupMediaLocation{Kind: SetupMediaKindDir, RemotePath: root}, true
	}
	return SetupMediaLocation{}, false
}

func locateLocalSetupMedia(localDirs []string) (SetupMediaLocation, bool) {
	if iso := pickNewestISO(collectLocalISOs(localDirs)); iso != "" {
		return SetupMediaLocation{Kind: SetupMediaKindISO, LocalPath: iso}, true
	}
	for _, dir := range localDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if localDirHasSetup(dir) {
			return SetupMediaLocation{Kind: SetupMediaKindDir, LocalPath: dir}, true
		}
	}
	return SetupMediaLocation{}, false
}

func findRemoteISO(ctx *runner.StepContext, dirs []string) (string, bool) {
	script := buildRemoteSearchScript(dirs, "iso")
	res, err := runRemotePowerShellScript(ctx, script)
	if err != nil || res == nil {
		return "", false
	}
	candidates := parseRemoteLines(res.GetStdout())
	if len(candidates) == 0 {
		return "", false
	}
	best := pickNewestISO(candidates)
	return best, best != ""
}

func findRemoteSetupDir(ctx *runner.StepContext, dirs []string) (string, bool) {
	script := buildRemoteSearchScript(dirs, "setupdir")
	res, err := runRemotePowerShellScript(ctx, script)
	if err != nil || res == nil {
		return "", false
	}
	for _, line := range parseRemoteLines(res.GetStdout()) {
		if remoteSetupRoot(ctx, line) {
			return line, true
		}
	}
	return "", false
}

func runRemotePowerShellScript(ctx *runner.StepContext, script string) (runner.ExecResult, error) {
	if ctx == nil || ctx.Executor == nil {
		return nil, fmt.Errorf("nil context")
	}
	enc := EncodePowerShellCommand(script)
	return ctx.Execute(`powershell -NoProfile -EncodedCommand `+enc, false)
}

func buildRemoteSearchScript(dirs []string, mode string) string {
	var quoted []string
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		quoted = append(quoted, "'"+strings.ReplaceAll(d, "'", "''")+"'")
	}
	dirList := strings.Join(quoted, ",")
	switch mode {
	case "iso":
		return fmt.Sprintf(`
$dirs = @(%s)
$found = @()
foreach ($d in $dirs) {
  if (-not (Test-Path -LiteralPath $d)) { continue }
  $found += Get-ChildItem -LiteralPath $d -Filter *.iso -File -ErrorAction SilentlyContinue | Where-Object { $_.Name -match '(?i)(sql[_-]?server|sqlserver)' }
  $found += Get-ChildItem -LiteralPath $d -Filter *.iso -File -Recurse -ErrorAction SilentlyContinue | Where-Object { $_.Name -match '(?i)(sql[_-]?server|sqlserver)' }
}
$found | Select-Object -Unique FullName | ForEach-Object { $_.FullName }
`, dirList)
	default:
		return fmt.Sprintf(`
$dirs = @(%s)
foreach ($d in $dirs) {
  if (-not (Test-Path -LiteralPath $d)) { continue }
  $s = Get-ChildItem -LiteralPath $d -Filter setup.exe -File -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($s) { $s.DirectoryName; break }
}
`, dirList)
	}
}

func parseRemoteLines(stdout string) []string {
	var out []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func collectLocalISOs(dirs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		for _, pattern := range []string{filepath.Join(dir, "*.iso"), filepath.Join(dir, "*", "*.iso")} {
			matches, _ := filepath.Glob(pattern)
			for _, m := range matches {
				if !IsSQLServerSetupISO(m) || seen[m] {
					continue
				}
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

type isoCandidate struct {
	path    string
	version int
}

func pickNewestISO(paths []string) string {
	var cands []isoCandidate
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || !IsSQLServerSetupISO(p) {
			continue
		}
		cands = append(cands, isoCandidate{path: p, version: parseSQLServerISOVersion(filepath.Base(p))})
	}
	if len(cands) == 0 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].version != cands[j].version {
			return cands[i].version > cands[j].version
		}
		return cands[i].path > cands[j].path
	})
	return cands[0].path
}

func parseSQLServerISOVersion(name string) int {
	m := sqlServerISOVersionRE.FindStringSubmatch(name)
	if len(m) < 2 {
		return 0
	}
	v, _ := strconv.Atoi(m[1])
	return v
}

func localDirHasSetup(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "setup.exe"))
	return err == nil
}

func remoteSetupRoot(ctx *runner.StepContext, dir string) bool {
	_, ok := findRemoteSetupRoot(ctx, dir)
	return ok
}

// findRemoteSetupRoot returns the directory containing setup.exe under dir (recursive).
func findRemoteSetupRoot(ctx *runner.StepContext, dir string) (string, bool) {
	dir = strings.TrimRight(strings.TrimSpace(dir), `\`)
	if dir == "" || !canProbeRemoteMedia(ctx) {
		return "", false
	}
	if strings.HasSuffix(strings.ToLower(dir), ".iso") {
		return "", false
	}
	if commonfile.FileExists(ctx, joinWinPath(dir, "setup.exe")) {
		return dir, true
	}
	q := strings.ReplaceAll(dir, `'`, `''`)
	script := fmt.Sprintf(
		`$s=Get-ChildItem -LiteralPath '%s' -Filter setup.exe -File -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1; if ($s) { Split-Path -Parent $s.FullName }`,
		q,
	)
	res, err := runRemotePowerShellScript(ctx, script)
	if err != nil || res == nil || res.GetExitCode() != 0 {
		return "", false
	}
	root := strings.TrimSpace(firstOutputLine(res.GetStdout()))
	if root == "" {
		return "", false
	}
	if !commonfile.FileExists(ctx, joinWinPath(root, "setup.exe")) {
		return "", false
	}
	return normalizeWinPath(root), true
}

func joinWinPath(base, name string) string {
	name = strings.TrimLeft(strings.ReplaceAll(name, "/", `\`), `\`)
	if isWindowsDrivePath(name) {
		return normalizeWinPath(name)
	}
	base = strings.TrimRight(strings.ReplaceAll(base, "/", `\`), `\`)
	if base == "" {
		return name
	}
	return base + `\` + name
}

func readSetupMediaFromResults(ctx *runner.StepContext) SetupMediaLocation {
	get := func(key string) string {
		if v, ok := ctx.Results[key].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	loc := SetupMediaLocation{
		Kind:       get("mssql_setup_media_kind"),
		LocalPath:  get("mssql_setup_local_path"),
		RemotePath: get("mssql_setup_remote_path"),
	}
	if loc.Kind == SetupMediaKindUNC {
		loc.UNCPath = get("mssql_setup_root")
	}
	if loc.Kind == "" {
		if unc := strings.TrimSpace(ctx.GetParamString("mssql_setup_unc", "")); unc != "" {
			return SetupMediaLocation{Kind: SetupMediaKindUNC, UNCPath: unc}
		}
	}
	return loc
}
