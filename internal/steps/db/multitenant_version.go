package db

import (
	"fmt"
	"strings"

	commonfile "github.com/yinstall/internal/common/file"
	"github.com/yinstall/internal/runner"
)

// Multitenant requires YashanDB v23.5+ (CREATE PLUGGABLE DATABASE / --enable-pluggable-database).
var minMultitenantVersion = []int{23, 5, 0}

// ValidateMultitenantDBPackage returns an error when pkgPath is empty or version < 23.5.
func ValidateMultitenantDBPackage(pkgPath string) error {
	pkgPath = strings.TrimSpace(pkgPath)
	if pkgPath == "" {
		return fmt.Errorf("multitenant (CDB) requires YashanDB v%s+; set --db-package or place a v%s+ package under software dirs",
			commonfile.FormatYashanDBVersion(minMultitenantVersion),
			commonfile.FormatYashanDBVersion(minMultitenantVersion))
	}
	ver, err := commonfile.ParseYashanDBPackageVersion(pkgPath)
	if err != nil {
		return fmt.Errorf("multitenant (CDB) requires YashanDB v%s+: %w",
			commonfile.FormatYashanDBVersion(minMultitenantVersion), err)
	}
	if !commonfile.VersionAtLeast(ver, minMultitenantVersion...) {
		return fmt.Errorf("multitenant (CDB) requires YashanDB v%s+, but package %q is v%s",
			commonfile.FormatYashanDBVersion(minMultitenantVersion),
			pkgPath,
			commonfile.FormatYashanDBVersion(ver))
	}
	return nil
}

// ensureMultitenantPackageVersionCtx validates db_package >= 23.5 when multitenant is enabled.
func ensureMultitenantPackageVersionCtx(ctx *runner.StepContext, stepID string) error {
	if ctx == nil || !ctx.GetParamBool("db_enable_pluggable", false) {
		return nil
	}
	pkg := strings.TrimSpace(ctx.GetParamString("db_package", ""))
	if pkg == "" {
		if ctx.DryRun || ctx.Precheck {
			ctx.Logger.Warn("%s: multitenant enabled; package version check deferred (db_package not resolved yet)", stepID)
			return nil
		}
	}
	if err := ValidateMultitenantDBPackage(pkg); err != nil {
		return fmt.Errorf("%s: %w", stepID, err)
	}
	if pkg != "" {
		if ver, err := commonfile.ParseYashanDBPackageVersion(pkg); err == nil {
			ctx.Logger.Info("%s: multitenant package version OK (v%s)", stepID, commonfile.FormatYashanDBVersion(ver))
		}
	}
	return nil
}
