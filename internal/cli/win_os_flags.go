package cli

import (
	"github.com/spf13/cobra"
)

// Windows OS extension flags (shared os_* variables live in os.go).
var (
	osMaxTimeSkew       int
	osPowerPlan         string
	osRemoteMgmtEnable  bool
	osAvExclusionEnable bool
	osPagefileEnable    bool
	osLockPagesInMemory bool
	osAllowClientSKU    bool
	osLocalVolumeLabel  string
	osLocalVolumeSizeGB int
)

type registerWinOSFlagsConfig struct {
	whenSkipOSFalse string // suffix for help
}

func (c registerWinOSFlagsConfig) suffix(s string) string {
	if c.whenSkipOSFalse != "" {
		return s + c.whenSkipOSFalse
	}
	return s
}

// registerWinOSExtensionFlags registers Windows-only OS flags (safe to combine with registerAllOSFlags on mysql).
func registerWinOSExtensionFlags(cmd *cobra.Command, cfg registerWinOSFlagsConfig) {
	cmd.Flags().IntVar(&osMaxTimeSkew, "os-max-time-skew", 60, "Max time skew seconds (W-003/W-012)"+cfg.suffix(""))
	cmd.Flags().StringVar(&osPowerPlan, "os-power-plan", "high-performance", "Power plan: skip|high-performance|balanced (W-013)"+cfg.suffix(""))
	cmd.Flags().BoolVar(&osRemoteMgmtEnable, "os-remote-mgmt-enable", false, "Enable OpenSSH/WinRM if missing (W-005)"+cfg.suffix(""))
	cmd.Flags().BoolVar(&osAvExclusionEnable, "os-av-exclusion-enable", false, "Add Defender exclusions (W-010)"+cfg.suffix(""))
	cmd.Flags().BoolVar(&osPagefileEnable, "os-pagefile-enable", false, "Configure pagefile (W-007)"+cfg.suffix(""))
	cmd.Flags().BoolVar(&osLockPagesInMemory, "os-lock-pages-in-memory", false, "Grant LPIM for SQL service (W-007)"+cfg.suffix(""))
	cmd.Flags().BoolVar(&osAllowClientSKU, "os-allow-client-sku", false, "Allow non-Server Windows SKU (lab)"+cfg.suffix(""))
	cmd.Flags().StringVar(&osLocalVolumeLabel, "os-local-volume-label", "SQLData", "NTFS volume label (W-006)"+cfg.suffix(""))
	cmd.Flags().IntVar(&osLocalVolumeSizeGB, "os-local-volume-size-gb", 0, "Max volume size GB (0=whole disk)"+cfg.suffix(""))
}

func registerWinOSCommonFlags(cmd *cobra.Command, cfg registerWinOSFlagsConfig) {
	cmd.Flags().StringVar(&osTimezone, "os-timezone", "", "Windows timezone ID for W-003 (empty=China Standard Time)"+cfg.suffix(""))
	cmd.Flags().StringVar(&osNTPServer, "os-ntp-server", "", "NTP server for w32tm (empty=skip manual peer)"+cfg.suffix(""))
	cmd.Flags().StringVar(&osFirewallMode, "os-firewall-mode", "open-ports", "Firewall: keep|disable|open-ports|disable-lab"+cfg.suffix(""))
	cmd.Flags().StringVar(&osFirewallPorts, "os-firewall-ports", "", "Extra TCP ports to open (comma-separated)"+cfg.suffix(""))
	cmd.Flags().StringSliceVar(&osLocalDisks, "os-local-disk", nil, "Disk number(s) to initialize on Windows (e.g. 1,2)"+cfg.suffix(""))
	cmd.Flags().StringVar(&osLocalMount, "os-local-mount", "", "Data root path (e.g. D:\\SQL)"+cfg.suffix(""))
}

// registerMssqlOSFlags registers Win OS flags for mssql subcommands.
func registerMssqlOSFlags(cmd *cobra.Command) {
	cfg := registerWinOSFlagsConfig{
		whenSkipOSFalse: " (only when --skip-os=false)",
	}
	registerWinOSCommonFlags(cmd, cfg)
	registerWinOSExtensionFlags(cmd, cfg)
}
