// env.go - 环境变量配置公共函数
// 提供环境变量配置的通用逻辑，被 DB 安装和备库添加步骤共用

package os

import (
	"fmt"
	"path"
	"strings"

	"github.com/yinstall/internal/runner"
)

// EnvConfig 环境变量配置参数
type EnvConfig struct {
	User        string // 操作系统用户名
	ClusterName string // 数据库集群名
	DataPath    string // 数据目录路径
	BeginPort   int    // 数据库起始端口
	IsYACMode   bool   // 是否 YAC 模式
}

// EnvResult 环境变量配置结果
type EnvResult struct {
	HomeDir       string // 用户主目录
	TargetEnvFile string // 目标环境变量文件
	YasdbCount    int    // 运行中的 yasdb 进程数（保留兼容，不再用于判断文件路径）
	BashrcPath    string // 生成的 bashrc 路径
}

// homeDirFromPasswdLine 从 getent passwd 输出行解析 home 字段（第 6 列，与 cut -d: -f6 一致）。
func homeDirFromPasswdLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	fields := strings.Split(line, ":")
	if len(fields) < 6 {
		return ""
	}
	return strings.TrimSpace(fields[5])
}

// GetUserHomeDir 获取用户主目录（远端 Linux 经 getent passwd，无 shell 管道以避免 pipefail 缺失时误判成功）。
func GetUserHomeDir(ctx *runner.StepContext, user string) (string, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return "", fmt.Errorf("cannot determine home directory for empty username")
	}
	cmd := fmt.Sprintf("getent passwd %s", ShellSingleQuote(user))
	result, err := ctx.Execute(cmd, false)
	if err != nil {
		return "", fmt.Errorf("failed to get home directory for user %s: %w", user, err)
	}
	if result == nil {
		return "", fmt.Errorf("failed to get home directory for user %s: no command result", user)
	}
	if result.GetExitCode() != 0 {
		detail := strings.TrimSpace(result.GetStderr())
		if detail == "" {
			detail = strings.TrimSpace(result.GetStdout())
		}
		if detail == "" {
			detail = fmt.Sprintf("exit code %d", result.GetExitCode())
		}
		return "", fmt.Errorf("failed to get home directory for user %s: %s", user, detail)
	}
	homeDir := homeDirFromPasswdLine(result.GetStdout())
	if homeDir == "" {
		return "", fmt.Errorf("failed to get home directory for user %s: empty home field in passwd entry", user)
	}
	return homeDir, nil
}

// DefaultDBBeginPort 与 CLI --db-port 默认值一致。
const DefaultDBBeginPort = 1688

// ConventionUserHome 无 SSH 时 CLI flag 占位用的 /home/<user>（建连后须 ResolveStageDirParam 等按 getent 修正）。
func ConventionUserHome(user string) string {
	user = strings.TrimSpace(user)
	if user == "" {
		user = "yashan"
	}
	return fmt.Sprintf("/home/%s", user)
}

// DefaultStageDirUnderHome 返回 <home>/install 或 <home>/install_<port>。
func DefaultStageDirUnderHome(homeDir string, port int) string {
	if port == 0 {
		port = DefaultDBBeginPort
	}
	if port == DefaultDBBeginPort {
		return path.Join(homeDir, "install")
	}
	return path.Join(homeDir, fmt.Sprintf("install_%d", port))
}

// ConventionStageDir CLI 未建连时的 stage 目录占位（/home/<user>/install…）。
func ConventionStageDir(user string, port int) string {
	return DefaultStageDirUnderHome(ConventionUserHome(user), port)
}

// ResolveConventionStageDir 显式 stage 优先；空则 ConventionStageDir（1688→install，否则 install_<port>）。
func ResolveConventionStageDir(explicit, user string, port int) string {
	if s := strings.TrimSpace(explicit); s != "" {
		return s
	}
	return ConventionStageDir(user, port)
}

// DefaultDBClusterName 与 CLI --db-cluster-name 推断一致：默认端口 yashandb，否则 yashandb_<port>。
func DefaultDBClusterName(port int) string {
	if port <= 0 {
		port = DefaultDBBeginPort
	}
	if port == DefaultDBBeginPort {
		return "yashandb"
	}
	return fmt.Sprintf("yashandb_%d", port)
}

// ResolveDBClusterName 显式集群名优先，否则 DefaultDBClusterName(port)。
func ResolveDBClusterName(explicit string, port int) string {
	if s := strings.TrimSpace(explicit); s != "" {
		return s
	}
	return DefaultDBClusterName(port)
}

// IsConventionStageDir 判断 stage 是否仍为 CLI /home/<user> 占位路径。
func IsConventionStageDir(stageDir, user string, port int) bool {
	return strings.TrimSpace(stageDir) == ConventionStageDir(user, port)
}

// ResolveStageDirParam 当 db_stage_dir 仍为占位路径时，经 getent 解析真实 home 并写回 Params。
func ResolveStageDirParam(ctx *runner.StepContext) error {
	if ctx == nil {
		return nil
	}
	user := ctx.GetParamString("os_user", "yashan")
	port := ctx.GetParamInt("db_begin_port", DefaultDBBeginPort)
	stage := strings.TrimSpace(ctx.GetParamString("db_stage_dir", ""))
	if stage == "" {
		stage = ConventionStageDir(user, port)
	}
	if !IsConventionStageDir(stage, user, port) {
		return nil
	}
	home, err := GetUserHomeDir(ctx, user)
	if err != nil {
		return fmt.Errorf("resolve db_stage_dir: %w", err)
	}
	resolved := DefaultStageDirUnderHome(home, port)
	if ctx.Params == nil {
		ctx.Params = make(map[string]interface{})
	}
	ctx.Params["db_stage_dir"] = resolved
	if ctx.Logger != nil && resolved != stage {
		ctx.Logger.Info("Resolved db_stage_dir: %s -> %s", stage, resolved)
	}
	return nil
}

// ResolvePrimaryStageDirParam 备库主库 stage 目录：空或占位时经 getent 写回 db_stage_dir。
func ResolvePrimaryStageDirParam(ctx *runner.StepContext, primaryUser string, port int) error {
	if ctx == nil {
		return nil
	}
	user := strings.TrimSpace(primaryUser)
	if user == "" {
		user = "yashan"
	}
	if port == 0 {
		port = DefaultDBBeginPort
	}
	stage := strings.TrimSpace(ctx.GetParamString("db_stage_dir", ""))
	if stage != "" && !IsConventionStageDir(stage, user, port) {
		return nil
	}
	home, err := GetUserHomeDir(ctx, user)
	if err != nil {
		return fmt.Errorf("resolve db_stage_dir for primary user %s: %w", user, err)
	}
	if ctx.Params == nil {
		ctx.Params = make(map[string]interface{})
	}
	ctx.Params["db_stage_dir"] = DefaultStageDirUnderHome(home, port)
	return nil
}

// YasbootEnvFile 返回 <home>/.yasboot/<name>。
func YasbootEnvFile(homeDir, name string) string {
	return path.Join(homeDir, ".yasboot", name)
}

// ConventionYmpOracleEnvFile CLI 占位：/home/<user>/.oracle。
func ConventionYmpOracleEnvFile(user string) string {
	return path.Join(ConventionUserHome(user), ".oracle")
}

// ConventionYmpYasbootEnvFile CLI 占位：/home/<user>/.yasboot/ymp.env。
func ConventionYmpYasbootEnvFile(user string) string {
	return YasbootEnvFile(ConventionUserHome(user), "ymp.env")
}

// ResolveYmpOracleEnvFile 解析 ymp 用户 Oracle env 路径（param 为空或仍为 CLI 占位时用 getent）。
func ResolveYmpOracleEnvFile(ctx *runner.StepContext, ympUser string) (string, error) {
	ympUser = strings.TrimSpace(ympUser)
	if ympUser == "" {
		ympUser = "ymp"
	}
	envFile := strings.TrimSpace(ctx.GetParamString("ymp_oracle_env_file", ""))
	convention := ConventionYmpOracleEnvFile(ympUser)
	if envFile != "" && envFile != convention {
		return envFile, nil
	}
	home, err := GetUserHomeDir(ctx, ympUser)
	if err != nil {
		return "", err
	}
	return path.Join(home, ".oracle"), nil
}

// ResolveYmpYasbootEnvFile 经 getent 返回 ~/.yasboot/ymp.env 真实路径。
func ResolveYmpYasbootEnvFile(ctx *runner.StepContext, ympUser string) (string, error) {
	ympUser = strings.TrimSpace(ympUser)
	if ympUser == "" {
		ympUser = "ymp"
	}
	home, err := GetUserHomeDir(ctx, ympUser)
	if err != nil {
		return "", err
	}
	return YasbootEnvFile(home, "ymp.env"), nil
}

// ResolveEnvFileForUser 优先 ctx.Results["env_file"]，否则 getent home + DetermineEnvFile。
func ResolveEnvFileForUser(parent *runner.StepContext, hctx *runner.StepContext, user string, beginPort int) (string, error) {
	if parent != nil && parent.Results != nil {
		if envFileVal, ok := parent.Results["env_file"]; ok {
			if envFileStr, ok := envFileVal.(string); ok && envFileStr != "" {
				return envFileStr, nil
			}
		}
	}
	homeDir, err := GetUserHomeDir(hctx, user)
	if err != nil {
		return "", err
	}
	return DetermineEnvFile(homeDir, beginPort), nil
}

// GetYasdbProcessCount 获取运行中的 yasdb 进程数
func GetYasdbProcessCount(ctx *runner.StepContext) int {
	result, _ := ctx.Execute("pgrep -c -x yasdb 2>/dev/null || echo 0", false)
	yasdbCount := 0
	if result != nil && result.GetStdout() != "" {
		fmt.Sscanf(strings.TrimSpace(result.GetStdout()), "%d", &yasdbCount)
	}
	return yasdbCount
}

// DetermineEnvFile 根据端口号确定环境变量文件路径
// 规则：默认端口 1688 写入 ~/.bashrc；其他端口写入 ~/.port<端口号>
// 使用 path.Join：homeDir 来自远端 Linux（getent），控制端可能是 Windows，须保持正斜杠。
func DetermineEnvFile(homeDir string, beginPort int) string {
	if beginPort == 1688 {
		return path.Join(homeDir, ".bashrc")
	}
	return path.Join(homeDir, fmt.Sprintf(".port%d", beginPort))
}

// GetBashrcPath 获取 yasboot 生成的 bashrc 文件路径
func GetBashrcPath(homeDir, clusterName string) string {
	return fmt.Sprintf("%s/.yasboot/%s_yasdb_home/conf/%s.bashrc", homeDir, clusterName, clusterName)
}

// bashrcReplaceLine 在文件中查找匹配 grepPattern 的行：
//   - 如果找到且内容不同，用 sed 替换为 newLine
//   - 如果未找到，追加 newLine
//   - 如果已完全相同，不做任何操作
//
// 返回 "added" / "updated" / "unchanged"
func bashrcReplaceLine(ctx *runner.StepContext, file, grepPattern, newLine string) string {
	exactCmd := fmt.Sprintf("grep -qxF '%s' %s 2>/dev/null", newLine, file)
	r, _ := ctx.Execute(exactCmd, false)
	if r != nil && r.GetExitCode() == 0 {
		return "unchanged"
	}

	patternCmd := fmt.Sprintf("grep -qE '%s' %s 2>/dev/null", grepPattern, file)
	r, _ = ctx.Execute(patternCmd, false)
	if r != nil && r.GetExitCode() == 0 {
		sedCmd := fmt.Sprintf("sed -i '\\|%s|c\\%s' %s", grepPattern, newLine, file)
		ctx.Execute(sedCmd, false)
		return "updated"
	}

	appendCmd := fmt.Sprintf("echo '%s' >> %s", newLine, file)
	ctx.Execute(appendCmd, false)
	return "added"
}

// BashrcRemoveLine 从文件中删除匹配 grepPattern 的行
func BashrcRemoveLine(ctx *runner.StepContext, file, grepPattern string) bool {
	checkCmd := fmt.Sprintf("grep -qE '%s' %s 2>/dev/null", grepPattern, file)
	r, _ := ctx.Execute(checkCmd, false)
	if r == nil || r.GetExitCode() != 0 {
		return false
	}
	sedCmd := fmt.Sprintf("sed -i '\\|%s|d' %s", grepPattern, file)
	ctx.Execute(sedCmd, false)
	return true
}

// ConfigureEnvVars 配置环境变量（幂等：已存在的条目会被更新而非重复追加）
//
// 规则：
//   - 端口 1688（默认）：将 source <clusterName>.bashrc 写入 ~/.bashrc
//   - 其他端口：将所有内容写入 ~/.port<port>，不修改 ~/.bashrc
//
// YAC 模式下每个节点均需调用本函数。
func ConfigureEnvVars(ctx *runner.StepContext, cfg *EnvConfig) (*EnvResult, error) {
	homeDir, err := GetUserHomeDir(ctx, cfg.User)
	if err != nil {
		return nil, err
	}

	yasdbCount := GetYasdbProcessCount(ctx)
	targetEnvFile := DetermineEnvFile(homeDir, cfg.BeginPort)
	bashrcPath := GetBashrcPath(homeDir, cfg.ClusterName)

	result := &EnvResult{
		HomeDir:       homeDir,
		TargetEnvFile: targetEnvFile,
		YasdbCount:    yasdbCount,
		BashrcPath:    bashrcPath,
	}

	checkResult, _ := ctx.Execute(fmt.Sprintf("test -f %s", bashrcPath), false)
	if checkResult == nil || checkResult.GetExitCode() != 0 {
		return result, fmt.Errorf("generated bashrc not found at %s", bashrcPath)
	}

	if cfg.BeginPort != 1688 {
		checkResult, _ = ctx.Execute(fmt.Sprintf("test -f %s", targetEnvFile), false)
		if checkResult == nil || checkResult.GetExitCode() != 0 {
			cmd := fmt.Sprintf("touch %s && chown %s:%s %s", targetEnvFile, cfg.User, cfg.User, targetEnvFile)
			if _, err := ctx.Execute(cmd, true); err != nil {
				return result, fmt.Errorf("failed to create env file %s: %w", targetEnvFile, err)
			}
		}
	}

	if cfg.BeginPort == 1688 {
		completionPath := fmt.Sprintf("%s/.yasboot/yasboot.completion.bash", homeDir)
		completionLine := fmt.Sprintf("[ -f %s ] && source %s", completionPath, completionPath)
		bashrcReplaceLine(ctx, targetEnvFile,
			"yasboot\\.completion\\.bash", completionLine)

		sourceLine := fmt.Sprintf("source %s", bashrcPath)
		bashrcReplaceLine(ctx, targetEnvFile,
			"source.*\\.yasboot/.*_yasdb_home/conf/.*\\.bashrc", sourceLine)

		if cfg.IsYACMode {
			instanceResult, _ := ctx.Execute(fmt.Sprintf("ls %s/ycs/ 2>/dev/null | head -1", cfg.DataPath), false)
			if instanceResult != nil && instanceResult.GetStdout() != "" {
				instanceName := strings.TrimSpace(instanceResult.GetStdout())
				yascsHome := fmt.Sprintf("%s/ycs/%s", cfg.DataPath, instanceName)
				exportLine := fmt.Sprintf("export YASCS_HOME=%s", yascsHome)
				bashrcReplaceLine(ctx, targetEnvFile,
					"export YASCS_HOME=", exportLine)
			}
		}
	} else {
		sourceLine := fmt.Sprintf("source %s", bashrcPath)
		bashrcReplaceLine(ctx, targetEnvFile,
			"source.*\\.yasboot/.*_yasdb_home/conf/.*\\.bashrc", sourceLine)

		if cfg.IsYACMode {
			instanceResult, _ := ctx.Execute(fmt.Sprintf("ls %s/ycs/ 2>/dev/null | head -1", cfg.DataPath), false)
			if instanceResult != nil && instanceResult.GetStdout() != "" {
				instanceName := strings.TrimSpace(instanceResult.GetStdout())
				yascsHome := fmt.Sprintf("%s/ycs/%s", cfg.DataPath, instanceName)
				exportLine := fmt.Sprintf("export YASCS_HOME=%s", yascsHome)
				bashrcReplaceLine(ctx, targetEnvFile,
					"export YASCS_HOME=", exportLine)
			}
		}
	}

	return result, nil
}

// CleanEnvVars 清理指定集群的环境变量条目
// - 端口 1688：从 ~/.bashrc 中删除对应条目
// - 其他端口：删除整个 ~/.port<port> 文件
// YAC 模式下需在每个节点分别调用
func CleanEnvVars(ctx *runner.StepContext, user, clusterName, dataPath string, beginPort int) error {
	homeDir, err := GetUserHomeDir(ctx, user)
	if err != nil {
		return err
	}

	if beginPort == 0 {
		beginPort = 1688
	}

	if beginPort == 1688 {
		bashrc := path.Join(homeDir, ".bashrc")

		r, _ := ctx.Execute(fmt.Sprintf("test -f %s", bashrc), false)
		if r == nil || r.GetExitCode() != 0 {
			return nil
		}

		clusterSourcePattern := fmt.Sprintf("source.*\\.yasboot/%s_yasdb_home/conf/%s\\.bashrc", clusterName, clusterName)
		BashrcRemoveLine(ctx, bashrc, clusterSourcePattern)

		if dataPath != "" {
			yascsPattern := fmt.Sprintf("export YASCS_HOME=%s/ycs/", dataPath)
			BashrcRemoveLine(ctx, bashrc, yascsPattern)
		}

		otherClusterCmd := fmt.Sprintf("grep -cE 'source.*\\.yasboot/.*_yasdb_home/conf/.*\\.bashrc' %s 2>/dev/null || echo 0", bashrc)
		countResult, _ := ctx.Execute(otherClusterCmd, false)
		remaining := 0
		if countResult != nil {
			fmt.Sscanf(strings.TrimSpace(countResult.GetStdout()), "%d", &remaining)
		}
		if remaining == 0 {
			BashrcRemoveLine(ctx, bashrc, "yasboot\\.completion\\.bash")
		}

		ctx.Execute(fmt.Sprintf("sed -i '/^$/N;/^\\n$/d' %s", bashrc), false)
	} else {
		portFile := path.Join(homeDir, fmt.Sprintf(".port%d", beginPort))
		portQ := ShellSingleQuote(portFile)
		r, _ := ctx.Execute(fmt.Sprintf("test -f %s", portQ), false)
		if r != nil && r.GetExitCode() == 0 {
			ctx.Execute(fmt.Sprintf("rm -f %s", portQ), true)
		}
	}

	return nil
}

// VerifyYasboot 验证 yasboot 是否可用
func VerifyYasboot(ctx *runner.StepContext, user string) (string, bool) {
	cmd := fmt.Sprintf("su - %s -c 'which yasboot 2>/dev/null'", user)
	result, _ := ctx.Execute(cmd, false)
	if result != nil && result.GetExitCode() == 0 {
		return strings.TrimSpace(result.GetStdout()), true
	}
	return "", false
}

func normalizeLinuxInstallDir(installDir string) (string, error) {
	installDir = strings.TrimSuffix(strings.TrimSpace(installDir), "/")
	installDir = path.Clean(strings.ReplaceAll(installDir, `\`, `/`))
	if installDir == "" || !strings.HasPrefix(installDir, "/") {
		return "", fmt.Errorf("invalid install directory: %q", installDir)
	}
	return installDir, nil
}

// knownYmpInstallTopLevelEntry reports whether name is a YMP install artifact at installDir top level.
func knownYmpInstallTopLevelEntry(name string) bool {
	name = strings.TrimSpace(name)
	switch name {
	case "yashan-migrate-platform", "META-INF":
		return true
	}
	return strings.HasPrefix(name, "instantclient_")
}

// ForeignYmpInstallDirEntries lists top-level paths under installDir that are not YMP install artifacts.
func ForeignYmpInstallDirEntries(ctx *runner.StepContext, installDir string) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("step context is nil")
	}
	installDir, err := normalizeLinuxInstallDir(installDir)
	if err != nil {
		return nil, err
	}
	installQ := ShellSingleQuote(installDir)
	res, _ := ctx.Execute(fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 2>/dev/null", installQ), false)
	if res == nil || strings.TrimSpace(res.GetStdout()) == "" {
		return nil, nil
	}
	var foreign []string
	for _, line := range strings.Split(strings.TrimSpace(res.GetStdout()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		base := path.Base(line)
		if !knownYmpInstallTopLevelEntry(base) {
			foreign = append(foreign, line)
		}
	}
	return foreign, nil
}

// RefuseYmpInstallDirFullWipeIfForeign blocks rm -rf installDir when non-YMP top-level entries exist.
func RefuseYmpInstallDirFullWipeIfForeign(ctx *runner.StepContext, installDir string) error {
	foreign, err := ForeignYmpInstallDirEntries(ctx, installDir)
	if err != nil {
		return err
	}
	if len(foreign) == 0 {
		return nil
	}
	names := make([]string, 0, len(foreign))
	for _, p := range foreign {
		names = append(names, path.Base(p))
	}
	return fmt.Errorf(
		"refusing to remove %s: non-YMP entries detected (%s); move or delete them manually before wiping the install directory",
		installDir,
		strings.Join(names, ", "),
	)
}

// pathUnderInstallDir reports whether target is installDir itself or a path beneath it.
func pathUnderInstallDir(installDir, target string) bool {
	installDir, err := normalizeLinuxInstallDir(installDir)
	if err != nil {
		return false
	}
	target = path.Clean(strings.ReplaceAll(strings.TrimSpace(target), `\`, `/`))
	if target == "" {
		return false
	}
	if target == installDir {
		return true
	}
	return strings.HasPrefix(target, installDir+"/")
}

func ympYasbootEntryReferencesInstallDir(ctx *runner.StepContext, entryPath, installDir string) (bool, error) {
	entryPath = strings.TrimSpace(entryPath)
	if entryPath == "" {
		return false, nil
	}
	entryQ := ShellSingleQuote(entryPath)

	linkRes, _ := ctx.Execute(fmt.Sprintf("test -L %s", entryQ), false)
	if linkRes != nil && linkRes.GetExitCode() == 0 {
		readRes, _ := ctx.Execute(fmt.Sprintf("readlink %s", entryQ), false)
		if readRes == nil || readRes.GetExitCode() != 0 {
			return false, fmt.Errorf("readlink %s failed", entryPath)
		}
		target := strings.TrimSpace(readRes.GetStdout())
		if target == "" {
			return false, nil
		}
		if !strings.HasPrefix(target, "/") {
			// Relative symlink: resolve against entry dir for prefix check.
			target = path.Clean(path.Join(path.Dir(entryPath), target))
		}
		return pathUnderInstallDir(installDir, target), nil
	}

	installQ := ShellSingleQuote(installDir)
	grepRes, _ := ctx.Execute(fmt.Sprintf("grep -F %s %s >/dev/null 2>&1", installQ, entryQ), false)
	if grepRes != nil && grepRes.GetExitCode() == 0 {
		return true, nil
	}
	return false, nil
}

// RemoveYmpYasbootArtifactsUnderInstallDir removes ~/.yasboot/ymp.env and ymp_* entries
// that reference installDir (symlink target or file content path).
func RemoveYmpYasbootArtifactsUnderInstallDir(ctx *runner.StepContext, ympUser, installDir string) error {
	if ctx == nil {
		return fmt.Errorf("step context is nil")
	}
	installDir, err := normalizeLinuxInstallDir(installDir)
	if err != nil {
		return err
	}
	ympUser = strings.TrimSpace(ympUser)
	if ympUser == "" {
		ympUser = "ymp"
	}

	home, err := GetUserHomeDir(ctx, ympUser)
	if err != nil {
		return err
	}
	yasbootDir := path.Join(home, ".yasboot")
	yasbootQ := ShellSingleQuote(yasbootDir)

	listRes, _ := ctx.Execute(fmt.Sprintf(
		`find %s -maxdepth 1 \( -name 'ymp_*' -o -name 'ymp.env' \) 2>/dev/null`,
		yasbootQ,
	), false)
	if listRes == nil || strings.TrimSpace(listRes.GetStdout()) == "" {
		ctx.Logger.Info("No ~/.yasboot/ymp_* artifacts found for user %s", ympUser)
		return nil
	}

	for _, entryPath := range strings.Split(strings.TrimSpace(listRes.GetStdout()), "\n") {
		entryPath = strings.TrimSpace(entryPath)
		if entryPath == "" {
			continue
		}
		refs, err := ympYasbootEntryReferencesInstallDir(ctx, entryPath, installDir)
		if err != nil {
			return err
		}
		if !refs {
			ctx.Logger.Info("Keeping %s (does not reference %s)", entryPath, installDir)
			continue
		}
		if err := ValidateDeletePath(entryPath); err != nil {
			ctx.Logger.Warn("Skipping removal of %s: %v", entryPath, err)
			continue
		}
		ctx.Logger.Info("Removing yasboot artifact referencing %s: %s", installDir, entryPath)
		if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("rm -f %s", ShellSingleQuote(entryPath)), true); err != nil {
			return fmt.Errorf("failed to remove %s: %w", entryPath, err)
		}
	}
	return nil
}
