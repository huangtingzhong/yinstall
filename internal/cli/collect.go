// collect.go - yinstall collect 子命令
// 实现采集目标主机 OS 基线与 YashanDB 运行状态的子命令，将结果归档到本地目录。
// 命令结构镜像 os.go；两阶段执行（连通性检查 + 步骤执行）均复用 runner_host.go。
package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/yinstall/internal/common/archive"
	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/ssh"
)

// collectExecAdapter 实现 runner.Executor，额外暴露 ExecuteCtx 供 collect 步骤施加 SSH session 超时。
// 仅供 collect 子命令使用，不影响其它子命令的 runnerExecAdapter。
type collectExecAdapter struct {
	e ssh.Executor
}

func (a *collectExecAdapter) Execute(cmd string, sudo bool) (runner.ExecResult, error) {
	return a.e.Execute(cmd, sudo)
}

// ExecuteCtx 通过类型断言调用底层执行器的 ExecuteContext（带 context 超时）。
// collect 步骤通过 contextualExecutor 接口（collect_util.go 内部定义）调用此方法。
func (a *collectExecAdapter) ExecuteCtx(ctx context.Context, cmd string, sudo bool) (runner.ExecResult, error) {
	type ctxExec interface {
		ExecuteContext(context.Context, string, bool) (*ssh.ExecResult, error)
	}
	if ce, ok := a.e.(ctxExec); ok {
		return ce.ExecuteContext(ctx, cmd, sudo)
	}
	// fallback：底层不支持 context 时（理论上不会发生），退回普通执行
	return a.e.Execute(cmd, sudo)
}

func (a *collectExecAdapter) Host() string { return a.e.Host() }
func (a *collectExecAdapter) Close() error { return a.e.Close() }
func (a *collectExecAdapter) Upload(localPath, remotePath string, uploadCtx *ssh.UploadContext) error {
	return a.e.Upload(localPath, remotePath, uploadCtx)
}

// collectExecFactory 是 ExecutorAdapterFactory，供 RunPerHostStepsEx 使用。
func collectExecFactory(e ssh.Executor) runner.Executor {
	return &collectExecAdapter{e: e}
}

var (
	// collect 子命令专属参数
	collectProfile       string // --profile
	collectDBLogSince    string // --db-log-since
	collectDBLogUntil    string // --db-log-until
	collectEnvFile       string // --env-file（显式指定 env 文件路径）
	collectClusterName   string // --cluster-name（显式指定集群名）
	collectCmdTimeoutSec int    // --collect-cmd-timeout
	collectSQLTimeoutSec int    // --collect-sql-timeout
	collectLogTimeoutSec int    // --collect-log-timeout
	collectRulesFile     string // --rules-file（外部规则文件，合并到内置规则）
	collectNoPack        bool   // --no-pack
)

// newCollectCommand 构造并返回 collect 子命令。
func newCollectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect OS baseline and YashanDB runtime info from target hosts",
		Long: `Collect system and database diagnostic information from target hosts via SSH.
Output is archived to a local directory with JSON/text files organized per host.

Use --profile to select a predefined collection scope, or -s to pick individual steps.`,
		RunE:         runCollect,
		SilenceUsage: true,
	}

	// collect 专属 flag
	cmd.Flags().StringVar(&collectProfile, "profile", "full",
		"Collection profile: full, os, db, db-core, db-runtime, db-logs, baseline, network, hardware, kernel, storage, yac, minimal, standby")
	cmd.Flags().BoolVar(&collectNoPack, "no-pack", false,
		"Do not create an archive after collection (default: try tar.gz, then zip)")
	cmd.Flags().StringVar(&collectDBLogSince, "db-log-since", "",
		"Start time for DB log collection, e.g. '2026-01-01 00:00:00' (enables R-034)")
	cmd.Flags().StringVar(&collectDBLogUntil, "db-log-until", "",
		"End time for DB log collection (optional, requires --db-log-since)")
	cmd.Flags().StringVar(&collectEnvFile, "env-file", "",
		"Explicit path to YashanDB env file on remote host (auto-discovered if omitted)")
	cmd.Flags().StringVar(&collectClusterName, "cluster-name", "",
		"YashanDB cluster name (auto-discovered if omitted)")
	cmd.Flags().IntVar(&collectCmdTimeoutSec, "collect-cmd-timeout", 30,
		"Max seconds per collect shell command (0 = no limit)")
	cmd.Flags().IntVar(&collectSQLTimeoutSec, "collect-sql-timeout", 30,
		"Max seconds per yasql query in R-026 (0 = no limit)")
	cmd.Flags().IntVar(&collectLogTimeoutSec, "collect-log-timeout", 60,
		"Max seconds per DB log operation in R-034 (0 = no limit)")
	cmd.Flags().StringVar(&collectRulesFile, "rules-file", "",
		"Path to an extra rules YAML file; rules with the same id override built-in ones, new ids are appended")

	// 只注册 --os-user（collect 仅需知道产品用户，不需要其它 OS 基线 flag）
	cmd.Flags().StringVar(&osUser, "os-user", "yashan",
		"YashanDB OS user (used to discover env file and run DB commands)")

	return cmd
}

// runCollect 是 collect 子命令的主处理函数。
func runCollect(cmd *cobra.Command, args []string) error {
	flags := GetGlobalFlags()

	// --list-steps / -l：打印步骤目录后退出
	if flags.ListSteps {
		PrintCollectStepCatalog(collectProfile)
		return nil
	}

	// collect 不支持 --precheck 模式，若传入则警告并继续
	if flags.Precheck {
		fmt.Fprintln(os.Stderr, "Warning: --precheck is not supported for collect, ignoring")
		flags.Precheck = false
	}

	// 未指定 --targets 时，默认本地执行（与 os/db 一致）。
	if len(flags.Targets) == 0 {
		flags.Local = true
		flags.Targets = []string{"localhost"}
	} else {
		flags.Local = false
	}

	// 确定输出目录（默认 ./output/collect/<timestamp>，无写权限时回退到系统临时目录）
	outDir, outFallback, err := archive.ResolveOutputDir(flags.Output, "collect")
	if err != nil {
		return err
	}
	if err := archive.EnsureOutputDir(outDir); err != nil {
		return err
	}
	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("collect-%s", time.Now().Format(archive.TimestampFormat))
	}

	logger, err := newSessionLogger(rid, flags.LogDir)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	logger.Info("Starting yinstall collect (RunID: %s)", rid)
	logger.Info("Targets: %v", flags.Targets)
	logger.Info("Profile: %s", collectProfile)
	logger.Info("Output directory: %s", outDir)
	if outFallback {
		logger.Warn("Could not create ./output under current directory (permission denied); using temp dir: %s", outDir)
	}
	if collectCmdTimeoutSec > 0 {
		logger.Info("Collect cmd timeout: %ds", collectCmdTimeoutSec)
	} else {
		logger.Info("Collect cmd timeout: disabled (0)")
	}
	if collectSQLTimeoutSec > 0 {
		logger.Info("Collect SQL timeout: %ds", collectSQLTimeoutSec)
	}
	if collectLogTimeoutSec > 0 {
		logger.Info("Collect log timeout: %ds", collectLogTimeoutSec)
	}

	// 构造步骤参数
	params := buildCollectParams(outDir, collectNoPack, flags.UseSudo)

	sharedResults := map[string]interface{}{"output_dir": outDir}
	err = runCollectPipeline(CollectPipelineOpts{
		Profile:       collectProfile,
		OutDir:        outDir,
		NoPack:        collectNoPack,
		Params:        params,
		Flags:         flags,
		Logger:        logger,
		SharedResults: sharedResults,
	})
	if err != nil {
		logger.Error("Collect completed with errors")
		logger.Info("Partial results saved to: %s", outDir)
		logger.Info("Check debug logs at: %s", logger.DebugLogPath())
		return err
	}

	logger.Info("Collect completed successfully")
	archive.LogSummary(logger, outDir, sharedResults)
	archive.PrintTerminalSummary("Collect results", "Packaged file", outDir, sharedResults)
	return nil
}

// buildCollectParams 构造 collect 步骤所需的 ctx.Params map。
// useSudo 对应全局 --sudo 标志，与 db/os 子命令保持一致：
//   - true  → 非 root SSH 用户可通过免密 sudo 切换至产品用户或执行特权命令
//   - false → 仅允许 root 或已是目标用户时执行相关命令
func buildCollectParams(outDir string, noPack, useSudo bool) map[string]interface{} {
	return map[string]interface{}{
		"output_dir":          outDir,
		"archive_no_pack":     noPack,
		"os_user":             osUser,
		"env_file":            collectEnvFile,
		"cluster_name":        collectClusterName,
		"db_log_since":        collectDBLogSince,
		"db_log_until":        collectDBLogUntil,
		"profile":             collectProfile,
		"sudo":                useSudo,
		"collect_cmd_timeout": collectCmdTimeoutSec,
		"collect_sql_timeout": collectSQLTimeoutSec,
		"collect_log_timeout": collectLogTimeoutSec,
		"collect_rules_file":  collectRulesFile,
	}
}
