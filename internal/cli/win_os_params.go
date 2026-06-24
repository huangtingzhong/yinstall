package cli

import commonwin "github.com/yinstall/internal/common/win_os"

// buildWinOSParams returns Windows OS baseline params (shared os_* keys with Linux).
func buildWinOSParams(skipOS bool, profile commonwin.Profile) map[string]interface{} {
	profile = commonwin.ApplyParams(profile, map[string]interface{}{
		"os_power_plan": osPowerPlan,
	})
	return map[string]interface{}{
		"skip_os":                 skipOS,
		"os_hostname":             osHostname,
		"os_timezone":             commonwin.NormalizeWindowsTimezone(osTimezone),
		"os_ntp_server":           osNTPServer,
		"os_firewall_mode":        osFirewallMode,
		"os_firewall_ports":       osFirewallPorts,
		"os_local_disks":          osLocalDisks,
		"os_local_mount":          osLocalMount,
		"os_local_volume_label":   osLocalVolumeLabel,
		"os_local_volume_size_gb": osLocalVolumeSizeGB,
		"os_max_time_skew":        osMaxTimeSkew,
		"os_power_plan":           osPowerPlan,
		"os_remote_mgmt_enable":   osRemoteMgmtEnable,
		"os_av_exclusion_enable":  osAvExclusionEnable,
		"os_pagefile_enable":      osPagefileEnable,
		"os_lock_pages_in_memory": osLockPagesInMemory,
		"os_allow_client_sku":     osAllowClientSKU,
		"win_os_profile":          profile.Name,
		"win_os_profile_struct":   profile,
		"target_platform":         "windows",
	}
}
