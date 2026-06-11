package file

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yinstall/internal/runner"
)

const vcRedistBaseName = "VC_redist.x64.exe"

// FindVCRedistPackage locates VC++ redistributable in local or remote software dirs.
func FindVCRedistPackage(ctx *runner.StepContext, localDirs []string, remoteDir string) (string, error) {
	for _, dir := range localDirs {
		if !localSoftwareDirExists(dir) {
			continue
		}
		candidate := filepath.Join(dir, vcRedistBaseName)
		if localSoftwareDirExists(candidate) || fileExistsLocal(candidate) {
			return candidate, nil
		}
		matches, _ := filepath.Glob(filepath.Join(dir, "*VC*redist*.exe"))
		for _, m := range matches {
			if strings.EqualFold(filepath.Base(m), vcRedistBaseName) {
				return m, nil
			}
		}
	}

	for _, dir := range remoteSearchDirs(ctx, remoteDir) {
		if !remoteSoftwareDirExists(ctx, dir) {
			continue
		}
		result, _ := ctx.Execute(fmt.Sprintf("ls -1 %s/%s %s/*VC*redist*.exe 2>/dev/null || true", dir, vcRedistBaseName, dir), false)
		if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
			for _, line := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && strings.HasSuffix(strings.ToLower(line), ".exe") {
					return line, nil
				}
			}
		}
	}
	return "", fmt.Errorf("%s not found in local dirs %v or remote dir %s", vcRedistBaseName, localDirs, remoteDir)
}

func fileExistsLocal(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
