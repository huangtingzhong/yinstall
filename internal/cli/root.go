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
	runID        string
	dryRun       bool
	precheck     bool
	resume       bool
	includeSteps []string
	excludeSteps []string
	includeTags  []string
	excludeTags  []string
	forceSteps   []string // 强制执行的步骤（会删除已存在的资源）
	logDir       string

	// SSH 参数
	targets     []string
	sshPort     int
	sshUser     string
	sshAuth     string
	sshPassword string
	sshKeyPath  string
	useSudo     bool

	// 软件目录参数
	localSoftwareDirs []string // 本地软件目录（控制端）
	remoteSoftwareDir string   // 远程软件目录（目标端）
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
  - YCM/YMP installation`,
	Version: AppVersion,
}

func Execute() error {
	return rootCmd.Execute()
}

// SetAppVersion updates the application version at runtime
func SetAppVersion(version string) {
	AppVersion = version
	rootCmd.Version = version
}

func init() {
	// 全局参数
	rootCmd.PersistentFlags().StringVar(&runID, "run-id", "", "Run ID (auto-generated if not specified)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Generate plan without execution")
	rootCmd.PersistentFlags().BoolVar(&precheck, "precheck", false, "Only run checks, no changes")
	rootCmd.PersistentFlags().BoolVar(&resume, "resume", false, "Resume from last failed step")
	rootCmd.PersistentFlags().StringSliceVarP(&includeSteps, "include-steps", "s", nil, "Only execute these steps (default: execute all steps; e.g. -s B-004,B-014)")
	rootCmd.PersistentFlags().StringSliceVar(&excludeSteps, "exclude-steps", nil, "Skip these steps")
	rootCmd.PersistentFlags().StringSliceVar(&includeTags, "include-tags", nil, "Only execute steps with these tags")
	rootCmd.PersistentFlags().StringSliceVar(&excludeTags, "exclude-tags", nil, "Skip steps with these tags")
	rootCmd.PersistentFlags().StringSliceVarP(&forceSteps, "force", "f", nil, "Force execute steps (delete existing resources, e.g., -f B-001,B-002)")
	rootCmd.PersistentFlags().StringVar(&logDir, "log-dir", defaultLogDir(), "Log directory")

	// SSH 参数
	rootCmd.PersistentFlags().StringSliceVarP(&targets, "targets", "t", nil, "Target hosts (comma-separated)")
	rootCmd.PersistentFlags().IntVarP(&sshPort, "ssh-port", "p", 22, "SSH port")
	rootCmd.PersistentFlags().StringVarP(&sshUser, "ssh-user", "u", "root", "SSH user")
	rootCmd.PersistentFlags().StringVar(&sshAuth, "ssh-auth", "password", "SSH auth method (password|key)")
	rootCmd.PersistentFlags().StringVar(&sshPassword, "ssh-password", "", "SSH password")
	rootCmd.PersistentFlags().StringVar(&sshKeyPath, "ssh-key-path", defaultSSHKeyPath(), "SSH private key path")
	rootCmd.PersistentFlags().BoolVar(&useSudo, "sudo", true, "Use sudo for privileged operations")

	// 软件目录参数
	rootCmd.PersistentFlags().StringSliceVar(&localSoftwareDirs, "local-software-dirs", defaultLocalSoftwareDirs(), "Local software directories (control plane)")
	rootCmd.PersistentFlags().StringVar(&remoteSoftwareDir, "remote-software-dir", "/data/yashan/soft", "Remote software directory (target host)")

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

func defaultSSHKeyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "id_rsa")
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
		// Windows: C:\Users\<user>\Downloads\yashan
		downloadsYashan = filepath.Join(home, "Downloads", "yashan")
	} else {
		// macOS / Linux: ~/Downloads/yashan
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
	RunID             string
	DryRun            bool
	Precheck          bool
	Resume            bool
	IncludeSteps      []string
	ExcludeSteps      []string
	IncludeTags       []string
	ExcludeTags       []string
	ForceSteps        []string
	LogDir            string
	Targets           []string
	SSHPort           int
	SSHUser           string
	SSHAuth           string
	SSHPassword       string
	SSHKeyPath        string
	// Local indicates whether to use local executor (no SSH).
	// It is not exposed as a CLI flag anymore; commands derive it from whether --targets is specified.
	Local             bool
	UseSudo           bool
	LocalSoftwareDirs []string
	RemoteSoftwareDir string
}

func GetGlobalFlags() GlobalFlags {
	return GlobalFlags{
		RunID:             runID,
		DryRun:            dryRun,
		Precheck:          precheck,
		Resume:            resume,
		IncludeSteps:      includeSteps,
		ExcludeSteps:      excludeSteps,
		IncludeTags:       includeTags,
		ExcludeTags:       excludeTags,
		ForceSteps:        forceSteps,
		LogDir:            logDir,
		Targets:           targets,
		SSHPort:           sshPort,
		SSHUser:           sshUser,
		SSHAuth:           sshAuth,
		SSHPassword:       sshPassword,
		SSHKeyPath:        sshKeyPath,
		Local:             false,
		UseSudo:           useSudo,
		LocalSoftwareDirs: localSoftwareDirs,
		RemoteSoftwareDir: remoteSoftwareDir,
	}
}

func PrintError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}
