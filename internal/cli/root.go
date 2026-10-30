package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/logging"
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

	// 归档/输出（collect、stressos、os/db 安装后挂钩共用）
	outputDir        string // -o / --output
	archiveOnSuccess bool   // -a / --archive

	// 当前 OM (yasom) 地址；短参 -M，长参 --om（已移除旧别名 --om-ip）
	globalOmIP string

	// 参数配置文件 (--config/-c)；无路径时不读不写
	configFilePath     string
	configFileOriginal map[string]interface{}
	configWritebackCmd *cobra.Command // PersistentPreRun 记录的叶命令, 供 Execute 收尾回写
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

Use  yinstall <command> -l  to print the step catalog (IDs, order, descriptions) for that command.
Optional --config/-c loads a flat YAML parameter file (keys = long flag names; see examples/yinstall.example.yaml).
CLI flags override the file; on exit the same file is overwritten with effective flag values (except with -l).`,
	Version: AppVersion,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		configFileOriginal = nil
		configWritebackCmd = cmd
		configDebugLogPath = ""
		clearConfigProcessLog()
		if path := strings.TrimSpace(configFilePath); path != "" {
			logConfigProcess("config load| path=%s cmd=%s", path, cmd.Name())
			m, err := LoadConfigFileMap(path)
			if err != nil {
				logConfigProcess("config load| action=error err=%v", err)
				return err
			}
			configFileOriginal = copyConfigMap(m)
			keys := sortedConfigKeys(m)
			logConfigProcess("config load| action=ok key_count=%d keys=%s", len(keys), strings.Join(keys, ","))
			warn := func(format string, a ...interface{}) {
				fmt.Fprintf(os.Stderr, "WARNING: "+format+"\n", a...)
			}
			if err := ApplyConfigMapToCommand(cmd, m, warn); err != nil {
				logConfigProcess("config apply| action=error err=%v", err)
				return err
			}
			logConfigProcess("config apply| action=done")
		}
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
	// cobra 在 RunE 失败时不会跑 PersistentPostRun; 在此统一回写以满足「失败也覆盖」。
	runErr := rootCmd.Execute()
	if wbErr := flushConfigWriteback(); wbErr != nil {
		if runErr != nil {
			return fmt.Errorf("%v; config writeback: %w", runErr, wbErr)
		}
		return wbErr
	}
	return runErr
}

// flushConfigWriteback 将生效 flags 合并写回 --config 文件.
func flushConfigWriteback() error {
	path := strings.TrimSpace(configFilePath)
	if path == "" || configFileOriginal == nil || configWritebackCmd == nil {
		return nil
	}
	if !configWritebackCmd.HasParent() {
		return nil
	}
	configProcessMu.Lock()
	auditFrom := len(configProcessLog)
	configProcessMu.Unlock()
	if listSteps {
		logConfigProcess("config writeback| path=%s action=skip reason=list-steps", path)
		appendConfigProcessLogToDebugFile(auditFrom)
		return nil
	}
	includeKeys := make(map[string]bool, len(configFileOriginal))
	for k := range configFileOriginal {
		includeKeys[k] = true
	}
	flagVals := CollectCommandFlagValues(configWritebackCmd, includeKeys)
	merged := MergeConfigForWrite(configFileOriginal, flagVals)
	logConfigProcess("config writeback| path=%s action=start collected=%d merged=%d", path, len(flagVals), len(merged))
	for _, k := range sortedConfigKeys(flagVals) {
		logConfigProcess("config writeback| set key=%s value=%s", k, formatConfigValue(flagVals[k]))
	}
	for _, k := range sortedConfigKeys(merged) {
		if _, ok := flagVals[k]; ok {
			continue
		}
		logConfigProcess("config writeback| keep key=%s value=%s", k, formatConfigValue(merged[k]))
	}
	if err := WriteConfigFileAtomic(path, merged); err != nil {
		logConfigProcess("config writeback| action=error err=%v", err)
		appendConfigProcessLogToDebugFile(auditFrom)
		return fmt.Errorf("write config file %s: %w", path, err)
	}
	logConfigProcess("config writeback| action=done path=%s", path)
	appendConfigProcessLogToDebugFile(auditFrom)
	return nil
}

// newSessionLogger 创建 session/debug 日志，并将完整启动命令行写入 debug 日志。
func newSessionLogger(runID, logDir string) (*logging.Logger, error) {
	logger, err := logging.NewLogger(runID, logDir, AppVersion, AppAuthor, AppContact)
	if err != nil {
		return nil, err
	}
	logger.LogInvocationCommandLine(os.Args)
	flushConfigProcessLogToLogger(logger)
	return logger, nil
}

// SetAppVersion 在运行时更新应用版本号。
func SetAppVersion(version string) {
	AppVersion = version
	rootCmd.Version = version
}

// copyConfigMap 浅拷贝配置 map, 避免灌参过程改动原始快照.
func copyConfigMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func init() {
	// 全局参数
	rootCmd.PersistentFlags().StringVarP(&configFilePath, "config", "c", "",
		"YAML parameter file (flat keys = long flag names); CLI flags override file; on exit overwrites this file with effective values (see examples/yinstall.example.yaml)")
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
	rootCmd.PersistentFlags().StringVarP(&globalOmIP, "om", "M", "", "Current OM (yasom) host IP (standby/om/clean; empty = discover from om_addr when possible)")

	// 软件目录参数：-L 默认含 ./software、./pkg、当前目录、家目录等；-R 上传落点，查找时另扫 SSH 用户 $HOME
	// DefValue 设为 []：隐藏 Cobra 自动打印的展开路径，避免与 usage 中的目录说明重复
	rootCmd.PersistentFlags().StringSliceVarP(&localSoftwareDirs, "local-software-dirs", "L", defaultLocalSoftwareDirs(), "Local software dirs (./software, ./pkg, cwd, $HOME, and ~/Downloads/yashan or ~/Downloads/oracle if present)")
	if f := rootCmd.PersistentFlags().Lookup("local-software-dirs"); f != nil {
		f.DefValue = "[]"
	}
	rootCmd.PersistentFlags().StringVarP(&remoteSoftwareDir, "remote-software-dir", "R", "", "Remote software dir for upload/lookup (default: <os-user home>/soft; also searches SSH login user $HOME)")

	rootCmd.PersistentFlags().StringVarP(&outputDir, "output", "o", "",
		"Local output directory for collect/stress archives and install post-success archive (default: ./output/<kind>/<timestamp>)")
	rootCmd.PersistentFlags().BoolVarP(&archiveOnSuccess, "archive", "a", false,
		"After successful os/db install, run scoped collect into --output (default true for os/db; --archive=false to disable)")

	// 添加子命令
	rootCmd.AddCommand(osCmd)
	rootCmd.AddCommand(dbCmd)
	rootCmd.AddCommand(standbyCmd)
	rootCmd.AddCommand(omCmd)
	rootCmd.AddCommand(ycmCmd)
	rootCmd.AddCommand(ympCmd)
	rootCmd.AddCommand(NewCleanCommand())
	rootCmd.AddCommand(newCollectCommand())
	rootCmd.AddCommand(stressosCmd)
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

// DefaultLocalSoftwareDirs 返回 -L 未指定时的默认本地软件目录（供测试与调用方复用）。
func DefaultLocalSoftwareDirs() []string {
	return defaultLocalSoftwareDirs()
}

// defaultLocalSoftwareDirs 返回默认本地软件目录列表（控制端）。
// 顺序：./software、./pkg、工具当前工作目录（.）、用户家目录、
// ~/Downloads/yashan 与 ~/Downloads/oracle（目录存在时）。
func defaultLocalSoftwareDirs() []string {
	dirs := []string{"./software", "./pkg", "."}
	home, err := os.UserHomeDir()
	if err != nil {
		return uniquePreserveOrder(dirs)
	}
	dirs = append(dirs, home)
	for _, name := range []string{"yashan", "oracle"} {
		p := filepath.Join(home, "Downloads", name)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			dirs = append(dirs, p)
		}
	}
	return uniquePreserveOrder(dirs)
}

// uniquePreserveOrder 去重并保持顺序。
func uniquePreserveOrder(dirs []string) []string {
	seen := make(map[string]bool, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
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
	Output            string // --output / -o
	ArchiveOnSuccess  bool   // --archive / -a
	OmIP              string // -M/--om：当前 OM (yasom) 地址
}

// applyInstallArchiveDefault 在 os/db 子命令中，用户未显式传 -a/--archive 时默认开启安装后归档。
func applyInstallArchiveDefault(cmd *cobra.Command) {
	if !cmd.Flags().Changed("archive") {
		archiveOnSuccess = true
	}
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
		Output:            outputDir,
		ArchiveOnSuccess:  archiveOnSuccess,
		OmIP:              strings.TrimSpace(globalOmIP),
	}
}

// stepRemoteSoftwareDir 解析步骤用远端软件目录：显式 -R 优先，否则 <产品用户家目录占位>/soft。
func stepRemoteSoftwareDir(flags GlobalFlags, params map[string]interface{}) string {
	user := "yashan"
	if params != nil {
		if u, ok := params["os_user"].(string); ok && strings.TrimSpace(u) != "" {
			user = strings.TrimSpace(u)
		} else if u, ok := params["primary_os_user"].(string); ok && strings.TrimSpace(u) != "" {
			user = strings.TrimSpace(u)
		} else if u, ok := params["ymp_user"].(string); ok && strings.TrimSpace(u) != "" {
			user = strings.TrimSpace(u)
		}
	}
	return commonos.ResolveRemoteSoftwareDirExplicit(flags.RemoteSoftwareDir, user)
}

// ResolveGlobalOmIP 返回全局 -M/--om；若为空则依次尝试 extra（如 --om-current、targets[0]）。
func ResolveGlobalOmIP(extra ...string) string {
	if ip := strings.TrimSpace(globalOmIP); ip != "" {
		return ip
	}
	for _, e := range extra {
		if ip := strings.TrimSpace(e); ip != "" {
			return ip
		}
	}
	return ""
}

func PrintError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}
