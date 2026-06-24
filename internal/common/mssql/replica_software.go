package mssql

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

// sqlReleaseYearToMajor maps SQL Server release year (from ISO filename) to ProductMajorVersion.
var sqlReleaseYearToMajor = map[int]int{
	2012: 11,
	2014: 12,
	2016: 13,
	2017: 14,
	2019: 15,
	2022: 16,
}

// SQLReleaseYearFromMajor returns marketing year for a ProductMajorVersion (e.g. 13 → 2016).
func SQLReleaseYearFromMajor(major int) (int, bool) {
	for year, m := range sqlReleaseYearToMajor {
		if m == major {
			return year, true
		}
	}
	return 0, false
}

// SQLMajorFromReleaseYear returns ProductMajorVersion for ISO release year (e.g. 2016 → 13).
func SQLMajorFromReleaseYear(year int) (int, bool) {
	m, ok := sqlReleaseYearToMajor[year]
	return m, ok
}

// ValidateSetupMediaMatchesPrimary ensures explicit setup media matches primary SQL major version.
func ValidateSetupMediaMatchesPrimary(mediaRef string, primary MirrorInstanceInfo) error {
	mediaRef = strings.TrimSpace(mediaRef)
	if mediaRef == "" {
		return nil
	}
	major, err := strconv.Atoi(strings.TrimSpace(primary.ProductMajorVersion))
	if err != nil || major <= 0 {
		return fmt.Errorf("primary ProductMajorVersion %q invalid", primary.ProductMajorVersion)
	}
	expectedYear, ok := SQLReleaseYearFromMajor(major)
	if !ok {
		return fmt.Errorf("unsupported primary SQL major %d (ProductVersion %s)", major, primary.ProductVersion)
	}
	if strings.HasPrefix(strings.ToLower(mediaRef), `\\`) || strings.HasPrefix(strings.ToLower(mediaRef), "//") {
		// UNC path: cannot infer version from name reliably; user must ensure match.
		return nil
	}
	base := filepath.Base(mediaRef)
	if IsSQLServerSetupISO(base) || strings.HasSuffix(strings.ToLower(base), ".iso") {
		isoYear := parseSQLServerISOVersion(base)
		if isoYear == 0 {
			return fmt.Errorf("cannot parse SQL Server release year from ISO name %q", base)
		}
		isoMajor, ok := SQLMajorFromReleaseYear(isoYear)
		if !ok {
			return fmt.Errorf("unsupported SQL Server ISO release year %d in %q", isoYear, base)
		}
		if isoMajor != major {
			return fmt.Errorf(
				"setup media %q is SQL Server %d (major %d) but primary is %s (major %d, ProductVersion %s)",
				base, isoYear, isoMajor, primary.Edition, major, primary.ProductVersion,
			)
		}
		if expectedYear != isoYear {
			return fmt.Errorf("setup media year %d != expected %d for primary major %d", isoYear, expectedYear, major)
		}
	}
	return nil
}

// LocateSetupMediaMatchingPrimary finds setup media whose ISO release year matches primary ProductMajorVersion.
func LocateSetupMediaMatchingPrimary(ctx *runner.StepContext, primary MirrorInstanceInfo) (SetupMediaLocation, error) {
	major, err := strconv.Atoi(strings.TrimSpace(primary.ProductMajorVersion))
	if err != nil || major <= 0 {
		return SetupMediaLocation{}, fmt.Errorf("invalid primary ProductMajorVersion %q", primary.ProductMajorVersion)
	}
	expectedYear, ok := SQLReleaseYearFromMajor(major)
	if !ok {
		return SetupMediaLocation{}, fmt.Errorf("unsupported primary SQL major %d (ProductVersion %s)", major, primary.ProductVersion)
	}

	explicit := strings.TrimSpace(ctx.GetParamString("mssql_setup_package", ""))
	remoteDir := RemoteSoftwareDir(ctx)
	localDirs := ctx.LocalSoftwareDirs
	if explicit != "" {
		if err := ValidateSetupMediaMatchesPrimary(explicit, primary); err != nil {
			return SetupMediaLocation{}, err
		}
		loc, ok, err := locateExplicitSetupMedia(ctx, explicit, localDirs, remoteDir)
		if err != nil {
			return SetupMediaLocation{}, err
		}
		if !ok {
			return SetupMediaLocation{}, fmt.Errorf("setup media not found: %s", explicit)
		}
		return loc, nil
	}

	if canProbeRemoteMedia(ctx) {
		if loc, ok := findMatchingRemoteISO(ctx, remoteDir, expectedYear); ok {
			return loc, nil
		}
	}
	if loc, ok := findMatchingLocalISO(localDirs, expectedYear); ok {
		return loc, nil
	}

	return SetupMediaLocation{}, fmt.Errorf(
		"no matching SQL Server %d ISO on replica -R (%s); place *sql*server*%d*.iso under -L (e.g. ~/Downloads/mssql) for auto-upload, or use --mssql-setup-package (primary ProductVersion %s)",
		expectedYear, remoteDir, expectedYear, primary.ProductVersion,
	)
}

// LogLocatedReplicaSetupMedia logs how MSH-091 resolved setup media for embedded MS-006.
func LogLocatedReplicaSetupMedia(ctx *runner.StepContext, loc SetupMediaLocation) {
	if ctx == nil || ctx.Logger == nil {
		return
	}
	remoteDir := RemoteSoftwareDir(ctx)
	switch {
	case loc.Kind == SetupMediaKindUNC && loc.UNCPath != "":
		ctx.Logger.Info("Replica install media: UNC %s", loc.UNCPath)
	case loc.RemotePath != "":
		ctx.Logger.Info("Replica install media: remote ISO %s (found on replica)", loc.RemotePath)
	case loc.LocalPath != "":
		ctx.Logger.Info("Replica install media: local ISO %s (will upload to replica -R %s in MS-006)", loc.LocalPath, remoteDir)
	default:
		ctx.Logger.Info("Replica install media resolved (kind=%s)", loc.Kind)
	}
}

func findMatchingRemoteISO(ctx *runner.StepContext, remoteDir string, expectedYear int) (SetupMediaLocation, bool) {
	for _, iso := range collectRemoteISOs(ctx, remoteDir) {
		if parseSQLServerISOVersion(filepath.Base(iso)) == expectedYear {
			return SetupMediaLocation{Kind: SetupMediaKindISO, RemotePath: iso}, true
		}
	}
	return SetupMediaLocation{}, false
}

func findMatchingLocalISO(localDirs []string, expectedYear int) (SetupMediaLocation, bool) {
	for _, iso := range collectLocalISOs(localDirs) {
		if parseSQLServerISOVersion(filepath.Base(iso)) == expectedYear {
			if st, err := os.Stat(iso); err == nil && !st.IsDir() {
				return SetupMediaLocation{Kind: SetupMediaKindISO, LocalPath: iso}, true
			}
		}
	}
	return SetupMediaLocation{}, false
}

func collectRemoteISOs(ctx *runner.StepContext, remoteDir string) []string {
	if !canProbeRemoteMedia(ctx) {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, dir := range remoteSearchDirs(ctx, remoteDir) {
		q := strings.ReplaceAll(dir, `'`, `''`)
		script := fmt.Sprintf(
			`Get-ChildItem -LiteralPath '%s' -Filter *.iso -File -ErrorAction SilentlyContinue | ForEach-Object { $_.FullName }`,
			q,
		)
		res, err := runRemotePowerShellScript(ctx, script)
		if err != nil || res == nil || res.GetExitCode() != 0 {
			continue
		}
		for _, line := range strings.Split(res.GetStdout(), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || seen[line] {
				continue
			}
			if IsSQLServerSetupISO(filepath.Base(line)) {
				seen[line] = true
				out = append(out, line)
			}
		}
	}
	return out
}

// ReplicaInstanceMatchesPrimary reports whether replica SQL instance version/edition matches primary for HA.
func ReplicaInstanceMatchesPrimary(replica, primary MirrorInstanceInfo) bool {
	if strings.TrimSpace(replica.ProductVersion) == "" {
		return false
	}
	return compareMirrorInstancePair(primary, replica) == nil
}

// ReplicaSoftwareVersionMatchesPrimary reports whether detected replica software version equals primary.
func ReplicaSoftwareVersionMatchesPrimary(replica, primary MirrorInstanceInfo) bool {
	r := strings.TrimSpace(replica.ProductVersion)
	p := strings.TrimSpace(primary.ProductVersion)
	if r == "" || p == "" {
		return false
	}
	if r != p {
		return false
	}
	rm := strings.TrimSpace(replica.ProductMajorVersion)
	pm := strings.TrimSpace(primary.ProductMajorVersion)
	return rm == "" || pm == "" || rm == pm
}

// ShouldSkipReplicaSoftwareInstall decides whether embedded MS-* install can be skipped on replica.
func ShouldSkipReplicaSoftwareInstall(replica, primary MirrorInstanceInfo) bool {
	if ReplicaInstanceMatchesPrimary(replica, primary) {
		return true
	}
	if strings.TrimSpace(replica.Edition) == "" && strings.TrimSpace(replica.EngineEdition) == "" {
		return ReplicaSoftwareVersionMatchesPrimary(replica, primary)
	}
	return false
}

// PrimaryInstanceInfoKey is the shared Results key for primary instance metadata.
const PrimaryInstanceInfoKey = "primary_mssql_instance"

// StorePrimaryInstanceInfo saves primary instance info into shared results.
func StorePrimaryInstanceInfo(results map[string]interface{}, info MirrorInstanceInfo) {
	if results == nil {
		return
	}
	results[PrimaryInstanceInfoKey] = info
	results[MirrorInstanceInfoResultKey(info.Host)] = info
}

// PrimaryInstanceInfoFromResults reads primary instance info collected by MSH-090.
func PrimaryInstanceInfoFromResults(results map[string]interface{}) (MirrorInstanceInfo, bool) {
	if results == nil {
		return MirrorInstanceInfo{}, false
	}
	if v, ok := results[PrimaryInstanceInfoKey].(MirrorInstanceInfo); ok && strings.TrimSpace(v.ProductVersion) != "" {
		return v, true
	}
	return MirrorInstanceInfo{}, false
}

// ApplySetupMediaToContext stores resolved media for embedded MS-004..MS-006.
func ApplySetupMediaToContext(ctx *runner.StepContext, loc SetupMediaLocation) {
	if ctx == nil {
		return
	}
	storeSetupMediaResults(ctx, loc)
	if loc.Kind == SetupMediaKindUNC && loc.UNCPath != "" {
		ctx.Params["mssql_setup_unc"] = loc.UNCPath
	} else if loc.RemotePath != "" {
		ctx.Params["mssql_setup_package"] = loc.RemotePath
	} else if loc.LocalPath != "" {
		ctx.Params["mssql_setup_package"] = loc.LocalPath
	}
}

// ProbeReplicaInstalledSoftware detects SQL Server on replica via sqlcmd or registry.
func ProbeReplicaInstalledSoftware(ctx *runner.StepContext) (MirrorInstanceInfo, bool, error) {
	if ctx == nil {
		return MirrorInstanceInfo{}, false, fmt.Errorf("nil context")
	}
	host := TargetHost(ctx)
	if ctx.DryRun {
		return MirrorInstanceInfo{}, false, nil
	}
	entries, err := ListInstanceRegistry(ctx)
	if err != nil {
		return MirrorInstanceInfo{}, false, err
	}
	if len(entries) == 0 {
		return MirrorInstanceInfo{}, false, nil
	}
	if _, err := EnsureInstanceResolved(ctx); err != nil {
		return MirrorInstanceInfo{}, false, err
	}
	if stdout, err := QuerySqlcmdScalarOptional(ctx, "mirror instance info", MirrorInstanceInfoSQL()); err == nil && strings.TrimSpace(stdout) != "" {
		info, err := ParseMirrorInstanceInfo(host, stdout)
		if err != nil {
			return MirrorInstanceInfo{}, false, err
		}
		if strings.TrimSpace(info.ProductVersion) != "" {
			return info, true, nil
		}
	}
	instance := strings.TrimSpace(ctx.GetParamString("mssql_instance", InstanceAuto))
	if IsInstanceAuto(instance) {
		if inst, _, _ := HAInstanceSelection(ctx); !IsInstanceAuto(inst) {
			instance = inst
		} else {
			instance = DefaultInstance
		}
	}
	if info, ok, err := probeReplicaInstanceFromRegistry(ctx, host, instance); err != nil {
		return MirrorInstanceInfo{}, false, err
	} else if ok {
		return info, true, nil
	}
	return MirrorInstanceInfo{}, false, nil
}

func mirrorInfoFromRegistryEntry(host string, entry InstanceRegistryEntry) MirrorInstanceInfo {
	engine := engineEditionFromSQL(entry.Edition)
	major := entry.ProductMajor
	if major == 0 {
		major = ProductMajorFromInternalID(entry.InternalID)
	}
	return MirrorInstanceInfo{
		Host:                host,
		ProductVersion:      entry.Version,
		ProductLevel:        entry.PatchLevel,
		Edition:             entry.Edition,
		EngineEdition:       engine,
		ProductMajorVersion: strconv.Itoa(major),
	}
}

func engineEditionFromSQL(edition string) string {
	switch {
	case strings.Contains(edition, "Express"):
		return "4"
	case strings.Contains(edition, "Standard"):
		return "2"
	case strings.Contains(edition, "Personal"), strings.Contains(edition, "Desktop"):
		return "1"
	default:
		return "3"
	}
}

func probeReplicaInstanceFromRegistry(ctx *runner.StepContext, host, instance string) (MirrorInstanceInfo, bool, error) {
	if ctx.DryRun {
		return MirrorInstanceInfo{}, false, nil
	}
	entries, err := ListInstanceRegistry(ctx)
	if err != nil {
		return MirrorInstanceInfo{}, false, err
	}
	entry, ok := FindInstanceByName(entries, instance)
	if !ok || strings.TrimSpace(entry.Version) == "" {
		return MirrorInstanceInfo{}, false, nil
	}
	return mirrorInfoFromRegistryEntry(host, entry), true, nil
}

func probeInstanceRootFromRegistry(ctx *runner.StepContext, instance string) string {
	if ctx.DryRun || ctx.Precheck {
		return ""
	}
	if entry, ok := RegistryEntryFromContext(ctx); ok && strings.TrimSpace(entry.SQLPath) != "" {
		return entry.SQLPath
	}
	instance = strings.TrimSpace(instance)
	if instance == "" || IsInstanceAuto(instance) {
		instance = DefaultInstance
	}
	entries, err := ListInstanceRegistry(ctx)
	if err != nil {
		return ""
	}
	entry, ok := FindInstanceByName(entries, instance)
	if !ok {
		return ""
	}
	return entry.SQLPath
}
