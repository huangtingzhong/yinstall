package collect

import "github.com/yinstall/internal/runner"

// Step category constants (English; used by CLI profile filtering).
const (
	CatMeta       = "CAT-META"
	CatDBEnv      = "CAT-DB-ENV"
	CatHW         = "CAT-HW"
	CatOSUser     = "CAT-OS-USER"
	CatKernel     = "CAT-KERNEL"
	CatNetwork    = "CAT-NETWORK"
	CatFirewall   = "CAT-FIREWALL"
	CatPackages   = "CAT-PACKAGES"
	CatStorage    = "CAT-STORAGE"
	CatDBPath     = "CAT-DB-PATH"
	CatDBConfig   = "CAT-DB-CONFIG"
	CatDBFS       = "CAT-DB-FS"
	CatDBData     = "CAT-DB-DATA"
	CatDBSQL      = "CAT-DB-SQL"
	CatDBLog      = "CAT-DB-LOG"
	CatYAC        = "CAT-YAC"
	CatSessionLog = "CAT-SESSION-LOG"
)

var stepCategoryByName = map[string]string{
	"Check Connectivity":             CatMeta,
	"Init archive directory":         CatMeta,
	"Snapshot install run params":    CatMeta,
	"Discover DB environment":        CatDBEnv,
	"Collect host identity":          CatHW,
	"Collect DMI hardware info":      CatHW,
	"Collect user resource limits":   CatOSUser,
	"Collect kernel parameters":      CatKernel,
	"Collect time and NTP status":    CatKernel,
	"Collect network interfaces":     CatNetwork,
	"Collect network routes and DNS": CatNetwork,
	"Collect firewall status":        CatFirewall,
	"Collect installed packages":     CatPackages,
	"Collect storage and LVM info":   CatStorage,
	"Collect DB paths and version":   CatDBPath,
	"Collect DB config files":        CatDBConfig,
	"Collect DB filesystem layout":   CatDBFS,
	"Collect DB cluster status":      CatDBData,
	"Collect DB processes and ports": CatDBData,
	"Collect DB autostart config":    CatDBData,
	"Collect DB SQL catalog":         CatDBSQL,
	"Detect DB config drift":         CatDBConfig,
	"Archive session logs":           CatSessionLog,
	"Collect YAC cluster info":       CatYAC,
	"Collect DB logs":                CatDBLog,
	"Run collect rules":              CatDBSQL,
	"Finalize manifest and summary":  CatMeta,
}

// StepCategory returns the CAT category for a collect step (defaults to CAT-META).
func StepCategory(step *runner.Step) string {
	if step == nil {
		return CatMeta
	}
	if c, ok := stepCategoryByName[step.Name]; ok {
		return c
	}
	return CatMeta
}
