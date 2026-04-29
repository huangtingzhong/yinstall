package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	// 全局参数
	runID           string
	dryRun          bool
	precheck        bool
	includeSteps    []string
	excludeSteps    []string
	forceAll        bool     // -f 无参数时为 true，强制执行所有步骤
	forceSteps      []string // 强制执行的步骤（会删除已存在的资源）
	forceDeleteUser bool     // --force-delete-user: 允许 -f 时删除并重建已存在的用户/组
	logDir          string

	// SSH 参数
	targets []string
	sshPort int
	// yasbootSshPort 为 0 时表示与 sshPort 一致（传给 yasboot package se/ce gen、config node gen 等的 --port）
	yasbootSshPort int
	sshUser        string
	sshAuth        string
	sshPassword    string
	sshKeyPath     string
	useSudo        bool

	// 软件目录参数
	localSoftwareDirs []string // 本地软件目录（控制端）
	remoteSoftwareDir string   // 远程软件目录（目标端）

	listSteps bool // -l / --list-steps：列出当前子命令的步骤说明后退出
)

// AppVersion 在运行时可被 cmd/yinstall/main.go 的 init() 通过构建时注入的 Version 变量覆盖
var (
	AppVersion = "0.1.0"
	AppAuthor  = "huangtingzhong@hotmail.com"
	AppContact = "huangtingzhong@hotmail.com"
)

var rootCmd = &cobra.Command{
	Use:   "yinstall",
	Short: "YashanDB Installation Automation Tool",
	Long: `yinstall - YashanDB Installation Automation Tool

A CLI tool for automating YashanDB installation, including:
  - OS baseline preparation
  - Database installation (single/YAC)
  - Standby database setup
  - YCM/YMP installation

Use  yinstall <command> -l  to print the step catalog (IDs, order, descriptions) for that command.`,
	Version: AppVersion,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := validatePort("--ssh-port", sshPort); err != nil {
			return err
		}
		if yasbootSshPort != 0 {
			return validatePort("--yasboot-ssh-port", yasbootSshPort)
		}
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

// SetAppVersion 在运行时更新应用版本号。
func SetAppVersion(version string) {
	AppVersion = version
	rootCmd.Version = version
}

func init() {
	// 全局参数
	rootCmd.PersistentFlags().StringVar(&runID, "run-id", "", "Run ID (auto-generated if not specified)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Skip each step's Action and PostCheck after PreCheck (connectivity/SSH and out-of-band checks may still run)")
	rootCmd.PersistentFlags().BoolVar(&precheck, "precheck", false, "Only run checks, no changes")
	rootCmd.PersistentFlags().StringSliceVarP(&includeSteps, "include-steps", "s", nil, "Only execute these steps (default: all; e.g. -s B-005,B-017). Trailing hyphen is a range (E-011- = E-011 through last); use E-011 for a single step. If --exclude-steps also lists a step, exclude wins")
	rootCmd.PersistentFlags().StringSliceVarP(&excludeSteps, "exclude-steps", "e", nil, "Skip these steps (applied after --include-steps; same ID in both flags is skipped)")
	rootCmd.PersistentFlags().BoolVarP(&forceAll, "force", "F", false, "Force execute all steps (skip pre-check guards); or use -f/--force-steps to specify individual steps")
	rootCmd.PersistentFlags().StringSliceVarP(&forceSteps, "force-steps", "f", nil, "Force execute specific steps (e.g. -f B-002,B-003)")
	rootCmd.PersistentFlags().BoolVar(&forceDeleteUser, "force-delete-user", false, "Allow -F/--force or -f/--force-steps to delete and recreate existing users and groups (dangerous)")
	rootCmd.PersistentFlags().StringVar(&logDir, "log-dir", defaultLogDir(), "Log directory")
	rootCmd.PersistentFlags().BoolVarP(&listSteps, "list-steps", "l", false, "List step catalog for the subcommand (IDs, order, descriptions) and exit")

	// SSH 参数
	rootCmd.PersistentFlags().StringSliceVarP(&targets, "targets", "t", nil, "Target hosts (comma-separated)")
	rootCmd.PersistentFlags().IntVarP(&sshPort, "ssh-port", "p", 22, "SSH port")
	rootCmd.PersistentFlags().IntVar(&yasbootSshPort, "yasboot-ssh-port", 0, "SSH port passed to yasboot remote operations (--port; 0 = same as --ssh-port)")
	rootCmd.PersistentFlags().StringVarP(&sshUser, "ssh-user", "u", "root", "SSH user")
	rootCmd.PersistentFlags().StringVar(&sshAuth, "ssh-auth", "password", "SSH auth method (password|key)")
	rootCmd.PersistentFlags().StringVarP(&sshPassword, "ssh-password", "P", "", "SSH password")
	rootCmd.PersistentFlags().StringVar(&sshKeyPath, "ssh-key-path", defaultSSHKeyPath(), sshKeyPathFlagUsage())
	rootCmd.PersistentFlags().BoolVar(&useSudo, "sudo", true, "Use sudo for privileged operations")

	// 软件目录参数
	rootCmd.PersistentFlags().StringSliceVarP(&localSoftwareDirs, "local-software-dirs", "L", defaultLocalSoftwareDirs(), "Local software directories (control plane)")
	rootCmd.PersistentFlags().StringVarP(&remoteSoftwareDir, "remote-software-dir", "R", "/data/yashan/soft", "Remote software directory (target host)")

	// 添加子命令
	rootCmd.AddCommand(osCmd)
	rootCmd.AddCommand(dbCmd)
	rootCmd.AddCommand(standbyCmd)
	rootCmd.AddCommand(ycmCmd)
	rootCmd.AddCommand(ympCmd)
	rootCmd.AddCommand(NewCleanCommand())
}

func defaultLogDir() string {
	// 直接在当前目录下创建 logs 目录
	return "logs"
}

// defaultSSHKeyPath 返回当前运行环境、当前系统用户下的默认私钥路径。
// Windows: 一般为 <UserProfile>\.ssh\id_rsa；macOS/Linux: <home>/.ssh/id_rsa（随用户与 OS 变化；与 cross-compile 目标机无关，由运行本进程时的系统决定）。
func defaultSSHKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "id_rsa")
}

// sshKeyPathFlagUsage 为 --ssh-key-path 提供按平台区分的说明；具体解析后的路径见 help 末尾 Cobra 输出的 (default "…")。
func sshKeyPathFlagUsage() string {
	switch runtime.GOOS {
	case "windows":
		return "SSH private key path; implicit default is UserProfile\\.ssh\\id_rsa for the current Windows user"
	default:
		return "SSH private key path; implicit default is $HOME/.ssh/id_rsa for the current user (macOS/Linux)"
	}
}

// defaultLocalSoftwareDirs 返回默认本地软件目录列表。
// 除固定的 ./software 和 ./pkg 外，还会加入当前用户家目录下的
// Downloads/yashan 目录（跨平台：Windows 为 Downloads\yashan）。
func defaultLocalSoftwareDirs() []string {
	dirs := []string{"./software", "./pkg"}
	home, err := os.UserHomeDir()
	if err != nil {
		return dirs
	}
	var downloadsYashan string
	if runtime.GOOS == "windows" {
		// Windows：C:\Users\<user>\Downloads\yashan
		downloadsYashan = filepath.Join(home, "Downloads", "yashan")
	} else {
		// macOS / Linux：~/Downloads/yashan
		downloadsYashan = filepath.Join(home, "Downloads", "yashan")
	}
	// 仅当目录存在时才加入，避免无效路径干扰查找
	if info, err := os.Stat(downloadsYashan); err == nil && info.IsDir() {
		dirs = append(dirs, downloadsYashan)
	}
	return dirs
}

// GetGlobalFlags 获取全局参数
type GlobalFlags struct {
	RunID           string
	DryRun          bool
	Precheck        bool
	IncludeSteps    []string
	ExcludeSteps    []string
	ForceAll        bool
	ForceSteps      []string
	ForceDeleteUser bool
	LogDir          string
	Targets         []string
	SSHPort         int
	YasbootSSHPort  int // 传给 yasboot 的远端 SSH 端口；与 SSHPort 相同时即未单独指定 --yasboot-ssh-port（0 已解析）
	SSHUser         string
	SSHAuth         string
	SSHPassword     string
	SSHKeyPath      string
	// Local 表示是否使用本地执行器（不走 SSH）。
	// 它不再作为 CLI flag 暴露；各子命令会根据是否指定 --targets 来推导该值。
	Local             bool
	UseSudo           bool
	LocalSoftwareDirs []string
	RemoteSoftwareDir string
	ListSteps         bool
}

func GetGlobalFlags() GlobalFlags {
	ybPort := yasbootSshPort
	if ybPort == 0 {
		ybPort = sshPort
	}
	return GlobalFlags{
		RunID:             runID,
		DryRun:            dryRun,
		Precheck:          precheck,
		IncludeSteps:      includeSteps,
		ExcludeSteps:      excludeSteps,
		ForceAll:          forceAll,
		ForceSteps:        forceSteps,
		ForceDeleteUser:   forceDeleteUser,
		LogDir:            logDir,
		Targets:           targets,
		SSHPort:           sshPort,
		YasbootSSHPort:    ybPort,
		SSHUser:           sshUser,
		SSHAuth:           sshAuth,
		SSHPassword:       sshPassword,
		SSHKeyPath:        sshKeyPath,
		Local:             false,
		UseSudo:           useSudo,
		LocalSoftwareDirs: localSoftwareDirs,
		RemoteSoftwareDir: remoteSoftwareDir,
		ListSteps:         listSteps,
	}
}

func PrintError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}
