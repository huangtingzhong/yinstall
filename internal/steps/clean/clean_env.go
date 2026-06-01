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
	// resultKeyCleanAltYasdbHome：YAC 时 env 指向 ~/.yasboot/...，实际软件树常在 /data/.../yasdb_home
	resultKeyCleanAltYasdbHome = "clean_alt_yasdb_home"
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
func readSourcedDBEnvVars(ctx *runner.StepContext, osUser, envFile string) (*sourcedDBEnv, error) {
	cmd := `printf 'YASDB_HOME=%s\nYASDB_DATA=%s\nYASCS_HOME=%s\n' "${YASDB_HOME:-}" "${YASDB_DATA:-}" "${YASCS_HOME:-}"`
	result, err := commonos.ExecuteAsUserWithEnvCheck(ctx, osUser, envFile, cmd, false)
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

// envPathMatchesParam 校验 source 后的路径与 CLI 参数一致：相等或 sourced 为 param 子路径。
func envPathMatchesParam(sourced, param, label string) error {
	sourced = path.Clean(strings.ReplaceAll(strings.TrimSpace(sourced), `\`, `/`))
	param = path.Clean(strings.ReplaceAll(strings.TrimSpace(param), `\`, `/`))
	if sourced == "" || param == "" {
		return fmt.Errorf("%s: empty path (sourced=%q param=%q)", label, sourced, param)
	}
	if sourced == param || commonos.DeletePathUnder(sourced, param) {
		return nil
	}
	return fmt.Errorf("%s mismatch: sourced %q is not equal to nor under CLI param %q", label, sourced, param)
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

	if err := envPathMatchesParam(vars.YASDBHome, paramHome, "YASDB_HOME"); err != nil {
		// YAC：source 的 YASDB_HOME 常为 ~/.yasboot/<cluster>_yasdb_home，与 CLI 默认 /data/.../yasdb_home 并存
		if strings.Contains(vars.YASDBHome, "/.yasboot/") && strings.Contains(paramHome, "yasdb_home") {
			paramQ := commonos.ShellSingleQuote(paramHome)
			result, _ := ctx.Execute(fmt.Sprintf("test -d %s", paramQ), false)
			if result != nil && result.GetExitCode() == 0 {
				ctx.Results[resultKeyCleanAltYasdbHome] = paramHome
				ctx.Logger.Warn("YAC layout: env YASDB_HOME=%s; install tree at %s (both targeted for cleanup)", vars.YASDBHome, paramHome)
			} else {
				return err
			}
		} else {
			return err
		}
	}
	if err := envPathMatchesParam(vars.YASDBData, paramData, "YASDB_DATA"); err != nil {
		return err
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
func prepareCleanDBEnv(ctx *runner.StepContext) error {
	if v, ok := ctx.Results[resultKeyCleanEnvValidated].(bool); ok && v {
		return nil
	}

	osUser := ctx.GetParamString("os_user", "yashan")
	envFile, err := resolveCleanEnvFile(ctx)
	if err != nil {
		return fmt.Errorf("resolve env file: %w", err)
	}

	vars, err := readSourcedDBEnvVars(ctx, osUser, envFile)
	if err != nil {
		return fmt.Errorf("read sourced env from %s: %w", envFile, err)
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
