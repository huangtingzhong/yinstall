package mssql

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

// SetupStagingDir returns a legacy staging path under the instance data root (probed for existing media only).
func SetupStagingDir(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), `\`)
	if base == "" {
		base = `D:\SQL`
	}
	return base + `\setup`
}

// EnsureSetupMediaOnTarget uploads/mounts setup media and returns setup root on target.
func EnsureSetupMediaOnTarget(ctx *runner.StepContext) (string, error) {
	loc := readSetupMediaFromResults(ctx)
	if loc.Kind == "" {
		if err := ResolveAndStoreSetupMedia(ctx); err != nil {
			return "", err
		}
		loc = readSetupMediaFromResults(ctx)
	}
	if loc.Kind == SetupMediaKindUNC {
		return loc.UNCPath, nil
	}

	remoteDir := RemoteSoftwareDir(ctx)
	staging := remoteDir

	switch loc.Kind {
	case SetupMediaKindDir:
		if loc.RemotePath != "" && remoteSetupRoot(ctx, loc.RemotePath) {
			return strings.TrimRight(loc.RemotePath, `\`), nil
		}
		if loc.LocalPath == "" {
			return "", fmt.Errorf("local setup directory not resolved")
		}
		if ctx.DryRun || ctx.Precheck {
			ctx.Logger.Info("MS-006 dry-run/precheck: would upload setup tree %s -> %s", loc.LocalPath, staging)
			return staging, nil
		}
		if err := uploadSetupTree(ctx, loc.LocalPath, staging); err != nil {
			return "", err
		}
		if !remoteSetupRoot(ctx, staging) {
			return "", fmt.Errorf("setup.exe missing under %s after upload", staging)
		}
		return staging, nil

	case SetupMediaKindISO:
		remoteISO := loc.RemotePath
		if remoteISO == "" && loc.LocalPath != "" {
			if ctx.DryRun || ctx.Precheck {
				ctx.Logger.Info("MS-006 dry-run/precheck: would upload ISO %s", loc.LocalPath)
				return loc.LocalPath, nil
			}
			uploaded, err := commonfile.FindAndDistribute(ctx, filepath.Base(loc.LocalPath), ctx.LocalSoftwareDirs, remoteDir)
			if err != nil {
				return "", fmt.Errorf("upload ISO: %w", err)
			}
			remoteISO = uploaded
		}
		if remoteISO == "" {
			return "", fmt.Errorf("ISO path not resolved")
		}
		if ctx.DryRun || ctx.Precheck {
			ctx.Logger.Info("MS-006 dry-run/precheck: would mount ISO %s", remoteISO)
			return remoteISO, nil
		}
		return mountWindowsISO(ctx, remoteISO)
	default:
		return "", fmt.Errorf("unsupported setup media kind %q", loc.Kind)
	}
}

func uploadSetupTree(ctx *runner.StepContext, localRoot, remoteStaging string) error {
	localRoot = strings.TrimSpace(localRoot)
	if !localDirHasSetup(localRoot) {
		return fmt.Errorf("local setup directory missing setup.exe: %s", localRoot)
	}
	ctx.Logger.Info("MS-006 uploading setup tree %s -> %s", localRoot, remoteStaging)
	if err := commonfile.RemoteEnsureDir(ctx, remoteStaging, false); err != nil {
		return fmt.Errorf("ensure staging %s: %w", remoteStaging, err)
	}

	return filepath.Walk(localRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(localRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		remotePath := joinWinPath(remoteStaging, rel)
		if info.IsDir() {
			return commonfile.RemoteEnsureDir(ctx, remotePath, false)
		}
		if commonfile.FileExists(ctx, remotePath) {
			if rs := commonfile.RemoteFileSize(ctx, remotePath); rs >= 0 && rs == info.Size() {
				return nil
			}
		}
		if err := commonfile.RemoteEnsureDir(ctx, joinWinPath(remoteStaging, filepath.ToSlash(filepath.Dir(rel))), false); err != nil {
			return err
		}
		if err := ctx.Executor.Upload(path, remotePath, ctx.UploadContext()); err != nil {
			return fmt.Errorf("upload %s: %w", rel, err)
		}
		if !commonfile.FileExists(ctx, remotePath) {
			return fmt.Errorf("upload verification failed: %s", remotePath)
		}
		return nil
	})
}

func mountWindowsISO(ctx *runner.StepContext, isoPath string) (string, error) {
	isoPath = normalizeWinPath(isoPath)
	if isoPath == "" {
		return "", fmt.Errorf("ISO path empty")
	}
	q := strings.ReplaceAll(isoPath, `'`, `''`)
	// Single-line script: multiline -Command breaks over SSH/OpenSSH on Windows.
	script := fmt.Sprintf(
		`$iso='%s'; if (-not (Test-Path -LiteralPath $iso)) { throw ('ISO missing: ' + $iso) }; `+
			`if (-not (Get-DiskImage -ImagePath $iso -ErrorAction SilentlyContinue).Attached) { Mount-DiskImage -ImagePath $iso -ErrorAction Stop | Out-Null }; `+
			`$dl=$null; for ($i=0; $i -lt 20 -and -not $dl; $i++) { Start-Sleep -Seconds 2; `+
			`$di=Get-DiskImage -ImagePath $iso -ErrorAction SilentlyContinue; `+
			`if ($di) { $dl=(Get-Volume -DiskImage $di -ErrorAction SilentlyContinue | Where-Object { $_.DriveLetter } | Select-Object -ExpandProperty DriveLetter -First 1) } }; `+
			`if (-not $dl) { throw 'no drive letter for mounted ISO' }; Write-Output ($dl + ':\')`,
		q,
	)
	ctx.LogScriptPreview("powershell", "MS-006 mount ISO", script)
	res, err := ctx.ExecuteWithCheck(`powershell -NoProfile -Command "`+script+`"`, false)
	if err != nil {
		return "", fmt.Errorf("mount ISO %s: %w", isoPath, err)
	}
	root := strings.TrimSpace(firstOutputLine(res.GetStdout()))
	if root == "" {
		return "", fmt.Errorf("mount ISO returned empty drive letter (stdout=%q)", strings.TrimSpace(res.GetStdout()))
	}
	root = normalizeWinPath(root)
	if !strings.HasSuffix(root, `\`) {
		root += `\`
	}
	setupRoot, ok := findRemoteSetupRoot(ctx, root)
	if !ok {
		return "", fmt.Errorf("setup.exe not found on mounted ISO at %s", root)
	}
	ctx.Logger.Info("MS-006 mounted ISO at %s (setup root %s)", root, setupRoot)
	return setupRoot, nil
}

func normalizeWinPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "/", `\`)
	// Shells often eat "\s" in D:\soft → D:soft; repair drive-letter paths.
	if len(p) >= 3 && p[1] == ':' && p[2] != '\\' {
		p = p[:2] + `\` + strings.TrimLeft(p[2:], `\`)
	}
	return p
}

func firstOutputLine(stdout string) string {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// DistributeCUPackage uploads local CU/SP setup.exe to remote cu staging.
func DistributeCUPackage(ctx *runner.StepContext, localPkg, remoteStaging string) (string, error) {
	localPkg = strings.TrimSpace(localPkg)
	if localPkg == "" {
		return "", fmt.Errorf("mssql CU package path not set")
	}
	info, err := os.Stat(localPkg)
	if err != nil {
		return "", fmt.Errorf("local CU package: %w", err)
	}
	localDir := localPkg
	if !info.IsDir() {
		localDir = filepath.Dir(localPkg)
	}
	if err := commonfile.RemoteEnsureDir(ctx, remoteStaging, false); err != nil {
		return "", fmt.Errorf("ensure CU staging %s: %w", remoteStaging, err)
	}
	if _, err := commonfile.FindAndDistribute(ctx, "setup.exe", []string{localDir}, remoteStaging); err != nil {
		return "", fmt.Errorf("upload CU setup.exe: %w", err)
	}
	remoteSetup := remoteStaging + `\setup.exe`
	if !commonfile.FileExists(ctx, remoteSetup) {
		return "", fmt.Errorf("CU setup.exe missing after upload: %s", remoteSetup)
	}
	return remoteSetup, nil
}

// ResolveSetupExeAndINI locates setup.exe and Configuration.ini for MS-008 (uses Results or discovers on target).
func ResolveSetupExeAndINI(ctx *runner.StepContext) (setupExe, iniPath string, err error) {
	if ctx == nil {
		return "", "", fmt.Errorf("nil context")
	}
	layout := ResolveLayoutFromContext(ctx)
	iniPath = joinWinPath(layout.AdminBase, "Configuration.ini")
	if p, ok := ctx.Results["mssql_configuration_ini_path"].(string); ok && strings.TrimSpace(p) != "" {
		iniPath = normalizeWinPath(p)
	}
	setupRoot := ""
	if r, ok := ctx.Results["mssql_setup_root"].(string); ok {
		setupRoot = strings.TrimRight(strings.TrimSpace(r), `\`)
	}
	if setupRoot == "" {
		setupRoot = strings.TrimRight(strings.TrimSpace(ctx.GetParamString("mssql_setup_unc", "")), `\`)
	}
	if setupRoot == "" {
		if root, ok := ReadySetupRoot(ctx); ok {
			setupRoot = root
		}
	}
	if setupRoot == "" {
		setupRoot, err = EnsureSetupMediaOnTarget(ctx)
		if err != nil {
			return "", "", err
		}
		ctx.SetResult("mssql_setup_root", setupRoot)
	}
	setupExe = joinWinPath(setupRoot, "setup.exe")
	if ctx.DryRun || ctx.Precheck {
		return setupExe, iniPath, nil
	}
	if !commonfile.FileExists(ctx, setupExe) {
		return "", "", fmt.Errorf("setup.exe not found: %s", setupExe)
	}
	if !commonfile.FileExists(ctx, iniPath) {
		return "", "", fmt.Errorf("Configuration.ini not found: %s (run MS-007 or ensure file exists)", iniPath)
	}
	return setupExe, iniPath, nil
}

// PatchCommand builds setup.exe /Action=Patch command line.
func PatchCommand(setupExe string, quiet bool) string {
	flags := "/Action=Patch /IACCEPTSQLSERVERLICENSETERMS"
	if quiet {
		flags += " /QS"
	}
	return fmt.Sprintf(`"%s" %s`, setupExe, flags)
}

// ReadySetupRoot returns a directory containing setup.exe when media is already on the target.
func ReadySetupRoot(ctx *runner.StepContext) (string, bool) {
	if ctx == nil {
		return "", false
	}
	if r, ok := ctx.Results["mssql_setup_root"].(string); ok {
		r = strings.TrimRight(strings.TrimSpace(r), `\`)
		if root, found := findRemoteSetupRoot(ctx, r); found {
			return root, true
		}
	}
	if unc := strings.TrimSpace(ctx.GetParamString("mssql_setup_unc", "")); unc != "" {
		if root, found := findRemoteSetupRoot(ctx, unc); found {
			return root, true
		}
	}
	for _, dir := range setupMediaSearchDirs(ctx) {
		if root, found := findRemoteSetupRoot(ctx, dir); found {
			return root, true
		}
	}
	return "", false
}

// setupMediaSearchDirs lists remote dirs to probe for setup.exe (-R first, then legacy under instance data root).
func setupMediaSearchDirs(ctx *runner.StepContext) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimRight(strings.TrimSpace(p), `\`)
		if p == "" || seen[strings.ToLower(p)] {
			return
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
	}
	add(RemoteSoftwareDir(ctx))
	// legacy layouts from older yinstall runs
	add(`D:\SQL`)
	add(joinWinPath(`D:\SQL`, "soft"))
	add(SetupStagingDir(`D:\SQL`))
	return out
}

// RemoteSetupExeExists reports whether setup.exe exists under root on the target.
func RemoteSetupExeExists(ctx *runner.StepContext, root string) bool {
	root = strings.TrimRight(strings.TrimSpace(root), `\`)
	if root == "" {
		return false
	}
	return commonfile.FileExists(ctx, joinWinPath(root, "setup.exe"))
}
