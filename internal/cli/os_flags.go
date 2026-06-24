package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

const defaultOSUserPassword = "aaBB11@@33$$"

// ApplyOSUserPasswordFromSSHLogin 当 SSH 登录用户与产品用户相同、且采用密码认证并提供了 --ssh-password 时，
// 在未显式指定 --os-user-password 的情况下，将产品用户密码与 SSH 登录密码对齐（供 yasboot / C-001P 使用）。
func ApplyOSUserPasswordFromSSHLogin(cmd *cobra.Command, flags GlobalFlags, productUser string, osPassword *string) bool {
	if cmd == nil || osPassword == nil {
		return false
	}
	if cmd.Flags().Changed("os-user-password") {
		return false
	}
	if strings.TrimSpace(flags.SSHUser) != strings.TrimSpace(productUser) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(flags.SSHAuth), "key") {
		return false
	}
	sshPwd := strings.TrimSpace(flags.SSHPassword)
	if sshPwd == "" {
		return false
	}
	*osPassword = sshPwd
	return true
}

// ResolveOSUserPassword 解析产品用户密码：显式 --os-user-password > SSH 同用户密码对齐 > 默认值。
func ResolveOSUserPassword(cmd *cobra.Command, flags GlobalFlags, productUser string, osPassword *string) {
	if cmd == nil || osPassword == nil {
		return
	}
	if cmd.Flags().Changed("os-user-password") {
		if strings.TrimSpace(*osPassword) == "" {
			*osPassword = defaultOSUserPassword
		}
		return
	}
	if ApplyOSUserPasswordFromSSHLogin(cmd, flags, productUser, osPassword) {
		return
	}
	if strings.TrimSpace(*osPassword) == "" {
		*osPassword = defaultOSUserPassword
	}
}

// registerOSFlagsConfig 控制 OS 相关 flag 注册到 os / db / mysql 子命令时的帮助文案。
type registerOSFlagsConfig struct {
	forDB    bool // true：db 子命令，部分项标注 [OS] 与 --skip-os 说明
	forMySQL bool // true：mysql install/standby，仅注册 MySQL OS 基线用到的参数
}

func (c registerOSFlagsConfig) whenSkipOSFalse(s string) string {
	if c.forDB || c.forMySQL {
		return s + " (only effective when --skip-os=false)"
	}
	return s
}

// registerOSUserGroupFlags 产品用户与组（B-002/B-003 等步骤使用）。
func registerOSUserGroupFlags(cmd *cobra.Command, cfg registerOSFlagsConfig) {
	userDefault := "yashan"
	groupDefault := "yashan"
	shellDefault := "/bin/bash"
	sudoDefault := true
	userPwdHelp := "User password (yashan default; when -u matches --os-user and --ssh-auth uses password, defaults to --ssh-password if unset)"
	if cfg.forMySQL {
		userDefault = "mysql"
		groupDefault = "mysql"
		shellDefault = "/sbin/nologin"
		sudoDefault = false
		userPwdHelp = "MySQL OS user password" + cfg.whenSkipOSFalse("")
	} else if cfg.forDB {
		userPwdHelp = "Product user SSH password for yasboot (default yashan password; when -u matches --os-user and login uses --ssh-password, auto-aligned if unset)"
	}
	cmd.Flags().StringVar(&osUser, "os-user", userDefault, "Product user name")
	cmd.Flags().IntVar(&osUserUID, "os-user-uid", 701, "User UID")
	cmd.Flags().StringVar(&osGroup, "os-group", groupDefault, "Primary group name")
	cmd.Flags().IntVar(&osGroupGID, "os-group-gid", 701, "Primary group GID")
	if !cfg.forMySQL {
		cmd.Flags().StringVar(&osDBAGroup, "os-dba-group", "YASDBA", "DBA group name")
		cmd.Flags().IntVar(&osDBAGroupGID, "os-dba-group-gid", 702, "DBA group GID")
	}
	cmd.Flags().StringVar(&osUserShell, "os-user-shell", shellDefault, "User shell")
	cmd.Flags().StringVar(&osUserPassword, "os-user-password", defaultOSUserPassword, userPwdHelp)
	cmd.Flags().BoolVar(&osSudoersEnable, "os-sudoers-enable", sudoDefault, cfg.whenSkipOSFalse("Enable sudoers configuration"))
}

// registerOSBaselineFlags 时区、内核、YUM、防火墙、大页等 OS 基线参数。
func registerOSBaselineFlags(cmd *cobra.Command, cfg registerOSFlagsConfig) {
	if cfg.forMySQL {
		registerMysqlOSBaselineFlags(cmd, cfg)
		return
	}
	prefix := ""
	if cfg.forDB {
		prefix = "[OS] "
	}
	cmd.Flags().StringVar(&osTimezone, "os-timezone", "Asia/Shanghai", prefix+cfg.whenSkipOSFalse("System timezone"))
	cmd.Flags().StringVar(&osNTPServer, "os-ntp-server", "", prefix+cfg.whenSkipOSFalse("NTP server address (empty to skip NTP configuration)"))
	cmd.Flags().StringVar(&osHostname, "os-hostname", "", prefix+cfg.whenSkipOSFalse("Hostname prefix or comma-separated list for B-023 (empty=auto: replace only localhost/system default names, keep existing custom names)"))
	cmd.Flags().StringVar(&osSysctlFile, "os-sysctl-file", "/etc/sysctl.d/yashandb.conf", prefix+cfg.whenSkipOSFalse("Sysctl config file path"))
	cmd.Flags().StringVar(&osLimitsFile, "os-limits-file", "/etc/security/limits.conf", prefix+cfg.whenSkipOSFalse("Limits config file path"))
	cmd.Flags().BoolVar(&osKernelArgsEnable, "os-kernel-args-enable", true, prefix+cfg.whenSkipOSFalse("Enable kernel args configuration"))
	cmd.Flags().StringVar(&osKernelArgs, "os-kernel-args", "transparent_hugepage=never elevator=deadline LANG=en_US.UTF-8", prefix+cfg.whenSkipOSFalse("Kernel boot arguments"))
	cmd.Flags().BoolVar(&osHugepagesEnable, "os-hugepages-enable", false, prefix+cfg.whenSkipOSFalse("Enable huge pages configuration (memory size based on db-memory-percent)"))
	registerOSYumISOFlags(cmd, cfg)
	depsDefault := "libzstd zlib lz4 openssl openssl-devel libaio"
	toolsDefault := "zip bind-utils sysstat telnet iotop openssh-clients net-tools unzip libvncserver tigervnc-server device-mapper-multipath dstat lsof psmisc redhat-lsb-core parted xhost strace showmount expect tcl sysfsutils gdisk rsync lvm2 qperf chrony tmux bpftrace perf"
	if cfg.forDB {
		toolsDefault = ""
	}
	cmd.Flags().StringVar(&osDepsPkgs, "os-deps-db-packages", depsDefault, prefix+cfg.whenSkipOSFalse("DB dependency packages"))
	cmd.Flags().StringVar(&osToolsPkgs, "os-deps-tools-packages", toolsDefault, prefix+cfg.whenSkipOSFalse("Common tools packages (empty to skip)"))
	cmd.Flags().BoolVar(&osIgnoreInstallErrors, "os-ignore-install-errors", false, prefix+cfg.whenSkipOSFalse("Ignore package installation errors and continue (only show warnings)"))
	cmd.Flags().StringVar(&osZstdSourceTarball, "os-zstd-source-tarball", "", prefix+cfg.whenSkipOSFalse("Explicit zstd source tarball (zstd-x.y.z.tar.gz); empty=auto-discover (EL7 libzstd fallback)"))
	cmd.Flags().StringVar(&osFirewallMode, "os-firewall-mode", "disable", prefix+cfg.whenSkipOSFalse("Firewall mode: keep/disable/open-ports"))
	cmd.Flags().StringVar(&osFirewallPorts, "os-firewall-ports", "", prefix+cfg.whenSkipOSFalse("Ports to open, comma-separated"))
	cmd.Flags().StringVar(&osSELinuxMode, "os-selinux-mode", "keep", prefix+cfg.whenSkipOSFalse("SELinux mode: keep/permissive/disabled"))
}

func registerMysqlOSBaselineFlags(cmd *cobra.Command, cfg registerOSFlagsConfig) {
	cmd.Flags().StringVar(&osTimezone, "os-timezone", "Asia/Shanghai", "System timezone"+cfg.whenSkipOSFalse(""))
	cmd.Flags().StringVar(&osHostname, "os-hostname", "", "Hostname for B-023 (empty=auto: replace only localhost/system default names, keep existing custom names)"+cfg.whenSkipOSFalse(""))
	cmd.Flags().StringVar(&osSysctlFile, "os-sysctl-file", "/etc/sysctl.d/mysql.conf", "Sysctl config file path"+cfg.whenSkipOSFalse(""))
	cmd.Flags().StringVar(&osLimitsFile, "os-limits-file", "/etc/security/limits.conf", "Limits config file path"+cfg.whenSkipOSFalse(""))
	cmd.Flags().BoolVar(&osKernelArgsEnable, "os-kernel-args-enable", true, "Enable kernel args configuration"+cfg.whenSkipOSFalse(""))
	cmd.Flags().StringVar(&osKernelArgs, "os-kernel-args", "elevator=deadline transparent_hugepage=never numa=off", "Kernel boot arguments"+cfg.whenSkipOSFalse(""))
	cmd.Flags().StringVar(&osFirewallMode, "os-firewall-mode", "open-ports", "Firewall mode: keep/disable/open-ports"+cfg.whenSkipOSFalse(""))
	cmd.Flags().StringVar(&osFirewallPorts, "os-firewall-ports", "", "Ports to open (defaults to --mysql-port and mysqlx)"+cfg.whenSkipOSFalse(""))
	cmd.Flags().StringVar(&osSELinuxMode, "os-selinux-mode", "keep", "SELinux mode: keep/permissive/disabled"+cfg.whenSkipOSFalse(""))
}

// registerOSYumISOFlags YUM/ISO 源相关（与 os、db、stressos 共用 osYumMode 等包级变量）。
func registerOSYumISOFlags(cmd *cobra.Command, cfg registerOSFlagsConfig) {
	prefix := ""
	if cfg.forDB {
		prefix = "[OS] "
	}
	cmd.Flags().StringVar(&osYumMode, "os-yum-mode", "none", prefix+cfg.whenSkipOSFalse("YUM mode: online/local-iso/none"))
	cmd.Flags().StringVar(&osISODevice, "os-iso-device", "/dev/cdrom", prefix+cfg.whenSkipOSFalse("ISO file path/name or block device used when --os-yum-mode=local-iso (auto-searched if filename only)"))
	cmd.Flags().StringVar(&osISOMountpoint, "os-iso-mountpoint", "/media", prefix+cfg.whenSkipOSFalse("Mount point for ISO when --os-yum-mode=local-iso"))
	cmd.Flags().StringVar(&osYumRepoFile, "os-yum-repo-file", "/etc/yum.repos.d/local.repo", prefix+cfg.whenSkipOSFalse("YUM repo file path for local-iso mode"))
}

// registerOSMultipathFlags YAC 多路径与 udev（B-019 等）。
func registerOSMultipathFlags(cmd *cobra.Command, cfg registerOSFlagsConfig) {
	prefix := ""
	if cfg.forDB {
		prefix = "[OS] "
	}
	cmd.Flags().BoolVar(&yacMultipathEnable, "yac-multipath-enable", false, prefix+cfg.whenSkipOSFalse("Enable multipath configuration"))
	cmd.Flags().StringVar(&yacMultipathPkgs, "yac-multipath-packages", "device-mapper-multipath", prefix+cfg.whenSkipOSFalse("Multipath packages"))
	cmd.Flags().StringVar(&yacMultipathConf, "yac-multipath-conf", "/etc/multipath.conf", prefix+cfg.whenSkipOSFalse("Multipath config file"))
	cmd.Flags().BoolVar(&yacMultipathAutoWWID, "yac-multipath-auto-wwid", false, prefix+cfg.whenSkipOSFalse("Auto collect WWID"))
	cmd.Flags().StringVar(&yacUdevRulesFile, "yac-udev-rules-file", "/etc/udev/rules.d/99-yashandb-permissions.rules", prefix+cfg.whenSkipOSFalse("Udev rules file"))
	cmd.Flags().StringVar(&yacUdevOwner, "yac-udev-owner", "yashan", prefix+cfg.whenSkipOSFalse("Disk owner"))
	cmd.Flags().StringVar(&yacUdevGroup, "yac-udev-group", "YASDBA", prefix+cfg.whenSkipOSFalse("Disk group"))
	cmd.Flags().StringVar(&yacUdevMode, "yac-udev-mode", "0666", prefix+cfg.whenSkipOSFalse("Disk mode"))
}

// registerOSLocalDiskFlags 本地磁盘 LVM（B-020 等）。
func registerOSLocalDiskFlags(cmd *cobra.Command, cfg registerOSFlagsConfig) {
	prefix := ""
	if cfg.forDB {
		prefix = "[OS] "
	}
	mountDefault := "/data"
	if cfg.forMySQL {
		mountDefault = "/mysql"
	}
	cmd.Flags().StringSliceVar(&osLocalDisks, "os-local-disk", nil, prefix+cfg.whenSkipOSFalse("Local disks for data directory (e.g., /dev/sdb,/dev/sdc)"))
	cmd.Flags().StringVar(&osLocalVG, "os-local-vg", "yasvg", prefix+cfg.whenSkipOSFalse("Volume group name"))
	cmd.Flags().StringVar(&osLocalLV, "os-local-lv", "yaslv", prefix+cfg.whenSkipOSFalse("Logical volume name"))
	cmd.Flags().StringVar(&osLocalMount, "os-local-mount", mountDefault, prefix+cfg.whenSkipOSFalse("Mount point for data directory"))
}

// registerOSYACDiskGroupFlags 与 db 共用的 YAC 磁盘组 / 发现参数（db 子命令单独注册网络类 YAC flag）。
func registerOSYACDiskGroupFlags(cmd *cobra.Command, cfg registerOSFlagsConfig) {
	prefix := ""
	if cfg.forDB {
		prefix = "[OS] "
	}
	cmd.Flags().StringVar(&yacSystemDG, "yac-systemdg", "", prefix+"System diskgroup (format: dgname:/dev/sda,/dev/sdb)")
	cmd.Flags().StringVar(&yacDataDG, "yac-datadg", "", prefix+"Data diskgroup (format: dgname:/dev/sdc,/dev/sdd)")
	cmd.Flags().StringVar(&yacArchDG, "yac-archdg", "", prefix+"Archive diskgroup (format: dgname:/dev/sde, optional)")
	cmd.Flags().BoolVar(&yacArchDGEnable, "yac-archdg-enable", false, prefix+"Enable independent ArchDG creation (separate archive diskgroup)")
	cmd.Flags().StringVar(&yacScanIPs, "yac-scan-ips", "", prefix+"SCAN IP addresses for local SCAN mode (comma-separated, empty=auto-allocate)")
	cmd.Flags().StringVar(&yacDiskPattern, "yac-disk-pattern", "", prefix+cfg.whenSkipOSFalse("Disk path pattern for filtering (e.g., '/dev/sd[c-z]', empty=all disks)"))
	suffix := "Disks to exclude from YAC auto-discovery (comma-separated; exact path, /dev basename, or alias like data2)"
	if cfg.forDB {
		suffix += "; applies to C-001B when --skip-os and B-021 when full OS baseline"
	} else {
		suffix += cfg.whenSkipOSFalse("")
	}
	cmd.Flags().StringVar(&yacExcludeDisks, "yac-exclude-disks", "/dev/sda,/dev/sdb", prefix+suffix)
	cmd.Flags().StringVar(&yacSystemdgSizeMax, "yac-systemdg-size-max", "10G", prefix+cfg.whenSkipOSFalse("Max size threshold for systemdg classification"))
	cmd.Flags().BoolVar(&yacAutoConfirm, "yac-auto-confirm", false, prefix+cfg.whenSkipOSFalse("Skip user confirmation for auto-discovered disks"))
}

// registerYACModeFlag 显式启用 YAC（单节点 db/os 须传 --yac；targets>=2 时自动启用，亦可省略）。
func registerYACModeFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&yacMode, "yac", false, "Enable YAC cluster mode (package ce gen; auto-enabled when targets >= 2)")
}

// registerOSOnlyFlags 仅 os 子命令使用的参数（db 使用独立的 --db-memory-percent）。
func registerOSOnlyFlags(cmd *cobra.Command) {
	cmd.Flags().IntVar(&osDbMemoryPercent, "db-memory-percent", -1, "Planned DB memory percent (1-100) for shared memory sizing; omit on standalone os to use 90%% physical RAM")
	registerYACModeFlag(cmd)
}

// registerAllOSFlags 向子命令注册完整 OS 参数集（供 os / db 共用变量）。
func registerAllOSFlags(cmd *cobra.Command, cfg registerOSFlagsConfig) {
	if cfg.forMySQL {
		registerOSUserGroupFlags(cmd, cfg)
		registerOSBaselineFlags(cmd, cfg)
		registerOSLocalDiskFlags(cmd, cfg)
		return
	}
	registerOSUserGroupFlags(cmd, cfg)
	registerOSBaselineFlags(cmd, cfg)
	registerOSMultipathFlags(cmd, cfg)
	registerOSLocalDiskFlags(cmd, cfg)
	registerOSYACDiskGroupFlags(cmd, cfg)
	if !cfg.forDB {
		registerOSOnlyFlags(cmd)
	}
}
