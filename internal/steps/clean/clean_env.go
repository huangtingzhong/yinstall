// clean_env.go - clean DB 步骤的 env 发现、source 校验与带 env 执行
package clean

import (
	"fmt"
	"path"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
	standbysteps "github.com/yinstall/internal/steps/standby"
)

const (
	resultKeyCleanEnvFile        = "clean_env_file"
	resultKeyCleanSourcedHome    = "clean_sourced_yasdb_home"
	resultKeyCleanSourcedData    = "clean_sourced_yasdb_data"
	resultKeyCleanSourcedYascs   = "clean_sourced_yascs_home"
	resultKeyCleanSourcedCluster = "clean_sourced_cluster"
	resultKeyCleanEnvValidated   = "clean_env_validated"
	resultKeyCleanDBStageDir     = "clean_db_stage_dir"
	// resultKeyCleanAltYasdbHome：CLI/安装根目录（与 source 的 YASDB_HOME 不同时双删）
	resultKeyCleanAltYasdbHome = "clean_alt_yasdb_home"
	// resultKeyCleanYasbootHomeLink：~/.yasboot/<cluster>_yasdb_home 符号链接路径（删树时一并移除）
	resultKeyCleanYasbootHomeLink = "clean_yasboot_home_link"
)

// sourcedDBEnv 为 source env 后读取到的关键变量（不含密码）。
type sourcedDBEnv struct {
	YASDBHome string
	YASDBData string
	YASCSHome string
	Cluster   string
}

// buildCleanAdaptedParams 映射 clean ctx.Params 为 standby.GetPrimaryEnvFile 所需键。
func buildCleanAdaptedParams(ctx *runner.StepContext) map[string]interface{} {
	adapted := make(map[string]interface{}, len(ctx.Params)+2)
	for k, v := range ctx.Params {
		adapted[k] = v
	}
	adapted["primary_os_user"] = ctx.GetParamString("os_user", "yashan")
	if ef := ctx.GetParamString("clean_env_file", ""); ef != "" {
		adapted["primary_env_file"] = ef
	}
	return adapted
}

func resolveCleanEnvFile(ctx *runner.StepContext) (string, error) {
	adaptedCtx := &runner.StepContext{
		Executor:      ctx.Executor,
		Logger:        ctx.Logger,
		Params:        buildCleanAdaptedParams(ctx),
		Results:       ctx.Results,
		CurrentStepID: ctx.CurrentStepID,
	}
	return standbysteps.GetPrimaryEnvFile(adaptedCtx)
}

// readSourcedDBEnvVars 以产品用户 source env 后打印 YASDB_HOME/YASDB_DATA/YASCS_HOME。
// 使用 Quiet 版：source 失败时由 prepareCleanDBEnv 回退 CLI 路径，避免刷 Error Exit。
func readSourcedDBEnvVars(ctx *runner.StepContext, osUser, envFile string) (*sourcedDBEnv, error) {
	cmd := `printf 'YASDB_HOME=%s\nYASDB_DATA=%s\nYASCS_HOME=%s\n' "${YASDB_HOME:-}" "${YASDB_DATA:-}" "${YASCS_HOME:-}"`
	result, err := commonos.ExecuteAsUserWithEnvCheckQuiet(ctx, osUser, envFile, cmd, false)
	if err != nil {
		return nil, err
	}
	out := result.GetStdout()
	vars := &sourcedDBEnv{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "YASDB_HOME":
			vars.YASDBHome = val
		case "YASDB_DATA":
			vars.YASDBData = val
		case "YASCS_HOME":
			vars.YASCSHome = val
		}
	}
	fillSourcedEnvFromCLI(ctx, vars)
	if vars.YASDBHome == "" {
		return nil, fmt.Errorf("YASDB_HOME is empty after sourcing %s (set --yasdb-home or use an env file that exports YASDB_HOME)", envFile)
	}
	if vars.YASDBData == "" {
		return nil, fmt.Errorf("YASDB_DATA is empty after sourcing %s (set --yasdb-data or use an env file that exports YASDB_DATA)", envFile)
	}
	return vars, nil
}

// envPathMatchesParam 校验两路径在字面量上兼容：相等，或 a 在 b 下，或 b 在 a 下。
func envPathMatchesParam(a, b, label string) error {
	if PathsCompatibleLiterals(a, b) {
		return nil
	}
	a = path.Clean(strings.ReplaceAll(strings.TrimSpace(a), `\`, `/`))
	b = path.Clean(strings.ReplaceAll(strings.TrimSpace(b), `\`, `/`))
	if a == "" || b == "" {
		return fmt.Errorf("%s: empty path (a=%q b=%q)", label, a, b)
	}
	return fmt.Errorf("%s mismatch: %q is not equal to nor under %q", label, a, b)
}

// resolveRemoteRealPath 在远端解析符号链接真实路径；失败时返回清理后的原路径。
func resolveRemoteRealPath(ctx *runner.StepContext, p string) string {
	p = path.Clean(strings.ReplaceAll(strings.TrimSpace(p), `\`, `/`))
	if p == "" || p == "." || p == "/" {
		return p
	}
	q := commonos.ShellSingleQuote(p)
	cmd := fmt.Sprintf("readlink -f %s 2>/dev/null || realpath %s 2>/dev/null || printf '%%s' %s", q, q, q)
	result, _ := ctx.Execute(cmd, false)
	if result == nil || result.GetExitCode() != 0 {
		return p
	}
	out := path.Clean(strings.ReplaceAll(strings.TrimSpace(result.GetStdout()), `\`, `/`))
	if out == "" || out == "." {
		return p
	}
	return out
}

// envPathsCompatible 字面量或 resolve 后兼容则通过。
func envPathsCompatible(ctx *runner.StepContext, sourced, param, label string) (resolvedSourced, resolvedParam string, err error) {
	resolvedSourced = resolveRemoteRealPath(ctx, sourced)
	resolvedParam = resolveRemoteRealPath(ctx, param)
	if envPathMatchesParam(sourced, param, label) == nil {
		return resolvedSourced, resolvedParam, nil
	}
	if envPathMatchesParam(resolvedSourced, resolvedParam, label) == nil {
		return resolvedSourced, resolvedParam, nil
	}
	if envPathMatchesParam(resolvedSourced, param, label) == nil {
		return resolvedSourced, resolvedParam, nil
	}
	if envPathMatchesParam(sourced, resolvedParam, label) == nil {
		return resolvedSourced, resolvedParam, nil
	}
	return resolvedSourced, resolvedParam, fmt.Errorf("%s mismatch: sourced %q (resolved %q) != CLI %q (resolved %q)",
		label, sourced, resolvedSourced, param, resolvedParam)
}

func validateSourcedEnvAgainstParams(ctx *runner.StepContext, envFile string, vars *sourcedDBEnv) error {
	if ctx.Results == nil {
		ctx.Results = make(map[string]interface{})
	}
	paramHome := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
	paramData := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
	paramLog := ctx.GetParamString("yasdb_log", "/data/yashan/log")
	paramCluster := ctx.GetParamString("db_cluster_name", "yashandb")
	cleanYAC := ctx.GetParamString("clean_yac_disks", "") != ""

	resolvedHome, resolvedParamHome, err := envPathsCompatible(ctx, vars.YASDBHome, paramHome, "YASDB_HOME")
	if err != nil {
		return err
	}
	// ~/.yasboot 链接与真实安装树并存：进程匹配用真实路径，删树时双删链接与安装根
	if strings.Contains(vars.YASDBHome, "/.yasboot/") {
		ctx.Results[resultKeyCleanYasbootHomeLink] = path.Clean(strings.ReplaceAll(vars.YASDBHome, `\`, `/`))
		installRoot := resolvedHome
		if reVersionLeaf.MatchString(path.Base(resolvedHome)) {
			installRoot = path.Dir(resolvedHome)
		}
		// CLI 显式安装根优先（含自定义 cust_home）；否则用 resolve 推导的安装根
		altHome := strings.TrimSpace(paramHome)
		if altHome == "" || strings.Contains(altHome, "/.yasboot/") {
			altHome = installRoot
		}
		if altHome != "" && altHome != resolvedHome {
			ctx.Results[resultKeyCleanAltYasdbHome] = altHome
			ctx.Logger.Warn("env YASDB_HOME=%s resolves to %s; install tree %s also targeted for cleanup",
				vars.YASDBHome, resolvedHome, altHome)
		}
	} else if resolvedParamHome != "" && resolvedHome != "" && resolvedHome != resolvedParamHome {
		if commonos.DeletePathUnder(resolvedHome, resolvedParamHome) || commonos.DeletePathUnder(resolvedParamHome, resolvedHome) {
			ctx.Results[resultKeyCleanAltYasdbHome] = paramHome
		}
	}
	if resolvedHome != "" {
		vars.YASDBHome = resolvedHome
	}

	resolvedData, _, err := envPathsCompatible(ctx, vars.YASDBData, paramData, "YASDB_DATA")
	if err != nil {
		return err
	}
	if resolvedData != "" {
		vars.YASDBData = resolvedData
	}

	// 日志目录：与 DATA 同属安装根（如 /data/yashan/log 与 /data/yashan/yasdb_data）
	paramLogClean := path.Clean(strings.ReplaceAll(paramLog, `\`, `/`))
	parentOfData := path.Dir(path.Clean(strings.ReplaceAll(paramData, `\`, `/`)))
	if !commonos.DeletePathUnder(paramLogClean, parentOfData) {
		return fmt.Errorf("yasdb_log mismatch: %q is not under install parent %q (sourced YASDB_DATA=%q)",
			paramLogClean, parentOfData, vars.YASDBData)
	}

	readResult, _ := ctx.Execute(fmt.Sprintf("cat %s", commonos.ShellSingleQuote(envFile)), false)
	if readResult != nil && readResult.GetExitCode() == 0 {
		if cn, err := standbysteps.ClusterNameFromEnvFileContent(readResult.GetStdout()); err == nil && cn != "" {
			vars.Cluster = strings.TrimSpace(cn)
		}
	}
	if vars.Cluster == "" {
		vars.Cluster = paramCluster
	}
	if vars.Cluster != paramCluster {
		return fmt.Errorf("cluster name mismatch: sourced %q != CLI --cluster-name %q (from env file %s)",
			vars.Cluster, paramCluster, envFile)
	}

	if cleanYAC || isYACFromSourced(vars) {
		if vars.YASCSHome == "" {
			if inferred := inferYASCSHome(ctx, vars.YASDBData); inferred != "" {
				vars.YASCSHome = inferred
				ctx.Results[resultKeyCleanSourcedYascs] = inferred
			} else if yac, _ := probeYACEnvironment(ctx); yac {
				ctx.Logger.Warn("YASCS_HOME unset; YAC inferred from %s/ycs (disk auto may use /dev/yfs)", vars.YASDBData)
			} else {
				return fmt.Errorf("YAC cleanup requested but YASCS_HOME is empty after sourcing %s", envFile)
			}
		}
		// YAC：DATA 常为 .../yasdb_data/ce-1-1，YASCS 为 .../yasdb_data/ycs/ce-1-1（与 DATA 同级在 yasdb_data 下）
		dataRoot := path.Clean(strings.ReplaceAll(vars.YASDBData, `\`, `/`))
		yacParent := path.Dir(dataRoot)
		if !commonos.DeletePathUnder(vars.YASDBData, yacParent) {
			return fmt.Errorf("YASDB_DATA mismatch: sourced %q is not under YAC parent %q", vars.YASDBData, yacParent)
		}
		if !commonos.DeletePathUnder(vars.YASCSHome, yacParent) {
			return fmt.Errorf("YASCS_HOME mismatch: sourced %q is not under YAC parent %q", vars.YASCSHome, yacParent)
		}
	}

	ctx.Logger.Info("Sourced env matches CLI parameters:")
	ctx.Logger.Info("  env_file: %s", envFile)
	ctx.Logger.Info("  YASDB_HOME: %s (param %s)", vars.YASDBHome, paramHome)
	ctx.Logger.Info("  YASDB_DATA: %s (param %s)", vars.YASDBData, paramData)
	if vars.YASCSHome != "" {
		ctx.Logger.Info("  YASCS_HOME: %s", vars.YASCSHome)
	}
	ctx.Logger.Info("  cluster: %s", vars.Cluster)
	return nil
}

func isYACFromSourced(vars *sourcedDBEnv) bool {
	if vars.YASCSHome != "" {
		return true
	}
	return false
}

// prepareCleanDBEnv 解析 env 文件、source 并校验与 CLI 参数一致；结果写入 ctx.Results。
// env 缺失且 --force-clean-primary / --skip-cluster-detach 时回退 CLI 推断路径，继续本机擦除。
func prepareCleanDBEnv(ctx *runner.StepContext) error {
	if v, ok := ctx.Results[resultKeyCleanEnvValidated].(bool); ok && v {
		return nil
	}
	if ctx.Results == nil {
		ctx.Results = make(map[string]interface{})
	}

	osUser := ctx.GetParamString("os_user", "yashan")
	envFile, err := resolveCleanEnvFile(ctx)
	if err != nil {
		if allowCleanWithoutEnvFile(ctx) {
			ctx.Logger.Warn("yasboot env file missing (%v); using CLI yasdb paths for local wipe", err)
			applyCleanCLIPathFallback(ctx, "")
			return nil
		}
		return fmt.Errorf("resolve env file: %w", err)
	}

	vars, err := readSourcedDBEnvVars(ctx, osUser, envFile)
	if err != nil {
		// node remove 可能已删掉 .yasboot/<cluster>_yasdb_home，.bashrc 再 source 会失败；回退 CLI 路径继续本机清理
		ctx.Logger.Warn("sourced env unavailable from %s (%v); falling back to CLI yasdb paths", envFile, err)
		vars = &sourcedDBEnv{
			YASDBHome: ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home"),
			YASDBData: ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data"),
			Cluster:   ctx.GetParamString("db_cluster_name", "yashandb"),
		}
	}
	if err := validateSourcedEnvAgainstParams(ctx, envFile, vars); err != nil {
		return err
	}

	ctx.Results[resultKeyCleanEnvFile] = envFile
	ctx.Results[resultKeyCleanSourcedHome] = vars.YASDBHome
	ctx.Results[resultKeyCleanSourcedData] = vars.YASDBData
	if vars.YASCSHome != "" {
		ctx.Results[resultKeyCleanSourcedYascs] = vars.YASCSHome
	}
	if vars.Cluster != "" {
		ctx.Results[resultKeyCleanSourcedCluster] = vars.Cluster
	}
	ctx.Results[resultKeyCleanEnvValidated] = true
	return nil
}

func allowCleanWithoutEnvFile(ctx *runner.StepContext) bool {
	return ctx.GetParamBool("force_clean_primary", false) || ctx.GetParamBool("skip_cluster_detach", false)
}

func applyCleanCLIPathFallback(ctx *runner.StepContext, envFile string) {
	home := ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
	data := ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
	cluster := ctx.GetParamString("db_cluster_name", "yashandb")
	if envFile != "" {
		ctx.Results[resultKeyCleanEnvFile] = envFile
	}
	ctx.Results[resultKeyCleanSourcedHome] = home
	ctx.Results[resultKeyCleanSourcedData] = data
	ctx.Results[resultKeyCleanSourcedCluster] = cluster
	ctx.Results[resultKeyCleanEnvValidated] = true
}

// effectiveCleanDBPaths 返回经 source 校验后的路径（优先 sourced，供进程匹配与删目录）。
func effectiveCleanDBPaths(ctx *runner.StepContext) (home, data, log, cluster, osUser string, err error) {
	if err := prepareCleanDBEnv(ctx); err != nil {
		return "", "", "", "", "", err
	}
	home = ctx.GetParamString("yasdb_home", "/data/yashan/yasdb_home")
	data = ctx.GetParamString("yasdb_data", "/data/yashan/yasdb_data")
	log = ctx.GetParamString("yasdb_log", "/data/yashan/log")
	cluster = ctx.GetParamString("db_cluster_name", "yashandb")
	osUser = ctx.GetParamString("os_user", "yashan")
	if v, ok := ctx.Results[resultKeyCleanSourcedHome].(string); ok && v != "" {
		home = v
	}
	if v, ok := ctx.Results[resultKeyCleanSourcedData].(string); ok && v != "" {
		data = v
	}
	if v, ok := ctx.Results[resultKeyCleanSourcedCluster].(string); ok && v != "" {
		cluster = v
	}
	return home, data, log, cluster, osUser, nil
}

// resolveCleanDBStageDir resolves db_stage_dir via getent (same rules as yinstall db C-004/C-007).
func resolveCleanDBStageDir(ctx *runner.StepContext) (string, error) {
	if v, ok := ctx.Results[resultKeyCleanDBStageDir].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v), nil
	}
	if err := commonos.ResolveStageDirParam(ctx); err != nil {
		return "", err
	}
	stage := strings.TrimSpace(ctx.GetParamString("db_stage_dir", ""))
	if stage == "" {
		user := ctx.GetParamString("os_user", "yashan")
		port := ctx.GetParamInt("db_begin_port", commonos.DefaultDBBeginPort)
		stage = commonos.ConventionStageDir(user, port)
	}
	if ctx.Results == nil {
		ctx.Results = make(map[string]interface{})
	}
	ctx.Results[resultKeyCleanDBStageDir] = stage
	return stage, nil
}

// runYcsctlQueryDisk 在 source env 后执行 ycsctl query disk。
func runYcsctlQueryDisk(ctx *runner.StepContext) (string, error) {
	if err := prepareCleanDBEnv(ctx); err != nil {
		return "", err
	}
	osUser := ctx.GetParamString("os_user", "yashan")
	envFile, _ := ctx.Results[resultKeyCleanEnvFile].(string)
	if envFile == "" {
		return "", fmt.Errorf("clean env file not prepared")
	}

	whichCmd := "command -v ycsctl >/dev/null 2>&1"
	if result, err := commonos.ExecuteAsUserWithEnvCheck(ctx, osUser, envFile, whichCmd, false); err != nil {
		return "", fmt.Errorf("ycsctl not in PATH after sourcing %s: %w", envFile, err)
	} else if result.GetExitCode() != 0 {
		return "", fmt.Errorf("ycsctl not in PATH after sourcing %s", envFile)
	}

	result, err := commonos.ExecuteAsUserWithEnvCheck(ctx, osUser, envFile, "ycsctl query disk", true)
	if err != nil {
		return "", fmt.Errorf("ycsctl query disk: %w", err)
	}
	return result.GetStdout(), nil
}
