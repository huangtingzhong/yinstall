package clean

import (
	"fmt"
	"strings"
	"time"

	"github.com/yinstall/internal/common/file"
	commonmysql "github.com/yinstall/internal/common/mysql"
	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
	mysqlsteps "github.com/yinstall/internal/steps/mysql"
)

// GetMysqlCleanSteps returns MySQL cleanup steps.
func GetMysqlCleanSteps() []*runner.Step {
	return []*runner.Step{
		StepCleanMysql001StopService(),
		StepCleanMysql002RemoveUnit(),
		StepCleanMysql003RemoveDirectories(),
		StepCleanMysql004FinalCheck(),
	}
}

func cleanUseSudo(ctx *runner.StepContext) bool {
	return ctx.GetParamBool("sudo", false) && !ctx.GetParamBool("local_mode", false)
}

func mysqlCleanLayout(ctx *runner.StepContext) (commonmysql.Layout, error) {
	stage := cleanStageFromCtx(ctx)
	params := map[string]interface{}{
		"mysql_port": ctx.GetParamInt("mysql_port", 3306),
		"mysql_base": ctx.GetParamString("mysql_base", commonmysql.DefaultBase(ctx.GetTargetPlatform())),
	}
	version := ctx.GetParamString("mysql_version", "")
	if version == "" {
		pkg := ctx.GetParamString("mysql_package", "")
		if pkg != "" {
			var err error
			version, err = file.ParseMysqlVersionFromPackage(pkg)
			if err != nil {
				return commonmysql.Layout{}, err
			}
		}
	}
	if version == "" && stage == commonmysql.StageSoftware {
		return commonmysql.Layout{}, fmt.Errorf("mysql_version not set for --stage software (pass --mysql-package or --mysql-version)")
	}
	if version != "" {
		params["mysql_version"] = version
	}
	return commonmysql.ResolveLayout(params)
}

func cleanStageFromCtx(ctx *runner.StepContext) string {
	stage, err := commonmysql.ParseStage(ctx.GetParamString("mysql_stage", commonmysql.DefaultCleanStage()))
	if err != nil {
		return commonmysql.StageInstance
	}
	return stage
}

func mysqlServiceUnit(port int) string {
	return fmt.Sprintf("mysqld%d.service", port)
}

func mysqlStopPatterns(ctx *runner.StepContext, layout commonmysql.Layout) []string {
	cfg := layout.Other + "/my.cnf"
	if ctx.GetTargetPlatform() == "windows" {
		cfg = layout.Other + "/my.ini"
	}
	return []string{
		"--datadir=" + layout.Data,
		"--defaults-file=" + cfg,
		fmt.Sprintf("--port=%d", layout.Port),
		"mysqld.exe",
	}
}

func mysqlCollectPIDs(ctx *runner.StepContext, patterns []string) []string {
	if ctx.GetTargetPlatform() == "windows" {
		return mysqlCollectPIDsWindows(ctx, patterns)
	}
	seen := make(map[string]bool)
	var pids []string
	for _, pat := range patterns {
		if strings.TrimSpace(pat) == "" {
			continue
		}
		findCmd := fmt.Sprintf("ps -ef | grep -F -- %s | grep -v grep | grep -v yinstall | awk '{print $2}'",
			commonos.ShellSingleQuote(pat))
		result, _ := ctx.Execute(findCmd, false)
		if result == nil || strings.TrimSpace(result.GetStdout()) == "" {
			continue
		}
		for _, pid := range strings.Split(strings.TrimSpace(result.GetStdout()), "\n") {
			pid = strings.TrimSpace(pid)
			if pid != "" && !seen[pid] {
				seen[pid] = true
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

// mysqlWindowsCollectAllPIDs is true when patterns request every mysqld.exe (StageAll /
// kill-all), not instance-specific CommandLine matching (--port, --datadir, etc.).
func mysqlWindowsCollectAllPIDs(patterns []string) bool {
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat != "" && pat != "mysqld.exe" {
			return false
		}
	}
	return true
}

func mysqlCollectPIDsWindows(ctx *runner.StepContext, patterns []string) []string {
	seen := make(map[string]bool)
	var pids []string
	addPIDs := func(stdout string) {
		for _, pid := range strings.Split(strings.TrimSpace(stdout), "\n") {
			pid = strings.TrimSpace(pid)
			if pid != "" && !seen[pid] {
				seen[pid] = true
				pids = append(pids, pid)
			}
		}
	}
	if mysqlWindowsCollectAllPIDs(patterns) {
		cmd := `powershell -NoProfile -Command "Get-Process mysqld -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Id"`
		if result, _ := ctx.Execute(cmd, false); result != nil {
			addPIDs(result.GetStdout())
		}
	}
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" || pat == "mysqld.exe" {
			continue
		}
		escaped := strings.ReplaceAll(pat, `'`, `''`)
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "Get-CimInstance Win32_Process -Filter \"Name = 'mysqld.exe'\" | Where-Object { $_.CommandLine -and $_.CommandLine -like '*%s*' } | Select-Object -ExpandProperty ProcessId"`, escaped)
		result, _ := ctx.Execute(cmd, false)
		if result == nil {
			continue
		}
		addPIDs(result.GetStdout())
	}
	return pids
}

func mysqlShutdownGraceful(ctx *runner.StepContext, layout commonmysql.Layout) {
	if ctx.GetTargetPlatform() == "windows" {
		home := strings.ReplaceAll(layout.Home, `\`, `/`)
		ctx.Logger.Info("Shutting down MySQL via mysqladmin TCP (port=%d)", layout.Port)
		cmd := fmt.Sprintf(`powershell -NoProfile -Command "& '%s/bin/mysqladmin.exe' shutdown --host=127.0.0.1 --port=%d -uroot"`, home, layout.Port)
		ctx.Execute(cmd, false)
		time.Sleep(5 * time.Second)
		return
	}
	socket := layout.Other + "/mysql.sock"
	if !file.FileExists(ctx, socket) {
		return
	}
	admin := commonos.ShellSingleQuote(layout.Home + "/bin/mysqladmin")
	sockQ := commonos.ShellSingleQuote(socket)
	ctx.Logger.Info("Shutting down MySQL via mysqladmin (socket=%s)", socket)
	ctx.Execute(fmt.Sprintf("%s -S %s shutdown 2>/dev/null || true", admin, sockQ), false)
	time.Sleep(5 * time.Second)
}

func mysqlKillProcesses(ctx *runner.StepContext, layout commonmysql.Layout) {
	patterns := mysqlStopPatterns(ctx, layout)
	if ctx.GetTargetPlatform() == "windows" {
		for _, pid := range mysqlCollectPIDs(ctx, patterns) {
			ctx.Execute(fmt.Sprintf(`powershell -NoProfile -Command "Stop-Process -Id %s -Force -ErrorAction SilentlyContinue"`, pid), false)
		}
		time.Sleep(2 * time.Second)
		return
	}
	killPIDs := func(signal string) {
		for _, pid := range mysqlCollectPIDs(ctx, patterns) {
			ctx.Execute(fmt.Sprintf("kill %s %s 2>/dev/null || true", signal, pid), false)
		}
	}
	killPIDs("-15")
	time.Sleep(3 * time.Second)
	if len(mysqlCollectPIDs(ctx, patterns)) > 0 {
		killPIDs("-9")
		time.Sleep(2 * time.Second)
	}
}

func mysqlKillAllMysqld(ctx *runner.StepContext) {
	if ctx.GetTargetPlatform() == "windows" {
		for _, pid := range mysqlCollectPIDsWindows(ctx, []string{"mysqld.exe"}) {
			ctx.Execute(fmt.Sprintf(`powershell -NoProfile -Command "Stop-Process -Id %s -Force -ErrorAction SilentlyContinue"`, pid), false)
		}
		time.Sleep(2 * time.Second)
		return
	}
	for _, pid := range mysqlCollectPIDs(ctx, []string{"mysqld"}) {
		ctx.Execute(fmt.Sprintf("kill -9 %s 2>/dev/null || true", pid), false)
	}
	time.Sleep(2 * time.Second)
}

func mysqlKillProcessesByHome(ctx *runner.StepContext, layout commonmysql.Layout) {
	if strings.TrimSpace(layout.Home) == "" {
		mysqlKillAllMysqld(ctx)
		return
	}
	patterns := []string{layout.Home, "mysqld.exe"}
	mysqlKillProcessesWithPatterns(ctx, patterns)
}

func mysqlKillProcessesWithPatterns(ctx *runner.StepContext, patterns []string) {
	if ctx.GetTargetPlatform() == "windows" {
		for _, pid := range mysqlCollectPIDsWindows(ctx, patterns) {
			ctx.Execute(fmt.Sprintf(`powershell -NoProfile -Command "Stop-Process -Id %s -Force -ErrorAction SilentlyContinue"`, pid), false)
		}
		time.Sleep(2 * time.Second)
		return
	}
	killPIDs := func(signal string) {
		for _, pid := range mysqlCollectPIDs(ctx, patterns) {
			ctx.Execute(fmt.Sprintf("kill %s %s 2>/dev/null || true", signal, pid), false)
		}
	}
	killPIDs("-15")
	time.Sleep(3 * time.Second)
	if len(mysqlCollectPIDs(ctx, patterns)) > 0 {
		killPIDs("-9")
		time.Sleep(2 * time.Second)
	}
}

func mysqlCleanStop(ctx *runner.StepContext, layout commonmysql.Layout) {
	stage := cleanStageFromCtx(ctx)
	switch stage {
	case commonmysql.StageSoftware:
		ctx.Logger.Info("MySQL cleanup (software): stopping processes under %s", layout.Home)
		mysqlKillProcessesByHome(ctx, layout)
	case commonmysql.StageAll:
		ctx.Logger.Info("MySQL cleanup (all): stopping all instances under %s", layout.Base)
		mysqlStopAll(ctx, layout)
		mysqlKillAllMysqld(ctx)
	default:
		mysqlStopAll(ctx, layout)
	}
}

func mysqlStopAll(ctx *runner.StepContext, layout commonmysql.Layout) {
	if ctx.GetTargetPlatform() == "windows" {
		svc := fmt.Sprintf("MySQL%d", layout.Port)
		ctx.Logger.Info("Stopping Windows service %s", svc)
		ctx.Execute("net stop "+svc, false)
	}
	unit := mysqlServiceUnit(layout.Port)
	if commonos.CheckSystemdAvailable(ctx) {
		ctx.Execute(fmt.Sprintf("systemctl stop %s 2>/dev/null || true", unit), cleanUseSudo(ctx))
		ctx.Execute(fmt.Sprintf("systemctl disable %s 2>/dev/null || true", unit), cleanUseSudo(ctx))
	}
	mysqlShutdownGraceful(ctx, layout)
	mysqlKillProcesses(ctx, layout)
}

func ensureMysqlCleanPlatform(ctx *runner.StepContext) {
	if ctx.GetTargetPlatform() != "" && ctx.GetTargetPlatform() != runner.PlatformLinuxDefault {
		return
	}
	platform := mysqlsteps.DetectTargetPlatform(ctx)
	mysqlsteps.StoreTargetPlatform(ctx, platform)
}

// StepCleanMysql001StopService stops mysqld via systemd or process kill.
func StepCleanMysql001StopService() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-MYSQL-001",
		Name:        "Stop MySQL Service",
		Description: "Stop MySQL systemd service or mysqld processes",
		Tags:        []string{"clean", "mysql"},
		PreCheck: func(ctx *runner.StepContext) error {
			ensureMysqlCleanPlatform(ctx)
			layout, err := mysqlCleanLayout(ctx)
			if err != nil {
				return err
			}
			ctx.Logger.Info("MySQL cleanup: stage=%s port=%d base=%s", cleanStageFromCtx(ctx), layout.Port, layout.Base)
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			layout, err := mysqlCleanLayout(ctx)
			if err != nil {
				return err
			}
			mysqlCleanStop(ctx, layout)
			return nil
		},
	}
}

// StepCleanMysql002RemoveUnit removes systemd unit file.
func StepCleanMysql002RemoveUnit() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-MYSQL-002",
		Name:        "Remove MySQL Unit",
		Description: "Remove mysqld systemd unit file",
		Tags:        []string{"clean", "mysql"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if cleanStageFromCtx(ctx) == commonmysql.StageSoftware {
				return fmt.Errorf("software stage: skip service removal")
			}
			if ctx.GetTargetPlatform() == "windows" {
				return nil
			}
			if !commonos.CheckSystemdAvailable(ctx) {
				return fmt.Errorf("systemd not available")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			layout, err := mysqlCleanLayout(ctx)
			if err != nil {
				return err
			}
			if ctx.GetTargetPlatform() == "windows" {
				svc := fmt.Sprintf("MySQL%d", layout.Port)
				home := strings.ReplaceAll(layout.Home, `\`, `/`)
				ctx.Logger.Info("Removing Windows service %s", svc)
				_, _ = ctx.Execute(fmt.Sprintf(`"%s/bin/mysqld.exe" --remove %s`, home, svc), false)
				return nil
			}
			unit := mysqlServiceUnit(layout.Port)
			unitPath := "/etc/systemd/system/" + unit
			ctx.Logger.Info("Removing unit file %s", unitPath)
			ctx.Execute("systemctl daemon-reload 2>/dev/null || true", cleanUseSudo(ctx))
			_, _ = ctx.Execute(fmt.Sprintf("rm -f %s", commonos.ShellSingleQuote(unitPath)), cleanUseSudo(ctx))
			ctx.Execute("systemctl daemon-reload 2>/dev/null || true", cleanUseSudo(ctx))
			return nil
		},
	}
}

// StepCleanMysql003RemoveDirectories removes MySQL home/data/other paths.
func StepCleanMysql003RemoveDirectories() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-MYSQL-003",
		Name:        "Remove MySQL Directories",
		Description: "Remove MySQL base/data/other directories",
		Tags:        []string{"clean", "mysql"},
		PreCheck: func(ctx *runner.StepContext) error {
			layout, err := mysqlCleanLayout(ctx)
			if err != nil {
				return err
			}
			for _, p := range commonmysql.CleanRemovePaths(cleanStageFromCtx(ctx), layout) {
				if strings.TrimSpace(p) == "" {
					continue
				}
				if err := commonos.ValidateDeletePath(p); err != nil {
					return fmt.Errorf("invalid delete path %q: %w", p, err)
				}
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			layout, err := mysqlCleanLayout(ctx)
			if err != nil {
				return err
			}
			stage := cleanStageFromCtx(ctx)
			for _, p := range commonmysql.CleanRemovePaths(stage, layout) {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if err := commonos.ValidateDeletePath(p); err != nil {
					ctx.Logger.Warn("Skip delete %s: %v", p, err)
					continue
				}
				ctx.Logger.Info("Removing %s (stage=%s)", p, stage)
				if err := file.RemoteRemovePath(ctx, p, cleanUseSudo(ctx)); err != nil {
					return err
				}
				if file.DirExists(ctx, p) || file.FileExists(ctx, p) {
					return fmt.Errorf("failed to remove %s", p)
				}
			}
			return nil
		},
	}
}

// StepCleanMysql004FinalCheck verifies mysqld stopped and dirs removed.
func StepCleanMysql004FinalCheck() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-MYSQL-004",
		Name:        "MySQL Cleanup Final Check",
		Description: "Verify MySQL processes stopped",
		Tags:        []string{"clean", "mysql"},
		Action: func(ctx *runner.StepContext) error {
			layout, err := mysqlCleanLayout(ctx)
			if err != nil {
				return err
			}
			stage := cleanStageFromCtx(ctx)
			var patterns []string
			switch stage {
			case commonmysql.StageSoftware:
				patterns = []string{layout.Home, "mysqld.exe"}
			case commonmysql.StageAll:
				patterns = []string{"mysqld", "mysqld.exe"}
			default:
				patterns = mysqlStopPatterns(ctx, layout)
			}
			if len(mysqlCollectPIDs(ctx, patterns)) > 0 {
				return fmt.Errorf("MySQL processes still running")
			}
			ctx.Logger.Info("[OK] MySQL cleanup verification passed (stage=%s)", stage)
			return nil
		},
	}
}

// StepCleanMySQL legacy single-step alias.
func StepCleanMySQL() *runner.Step {
	return &runner.Step{
		ID:          "CLEAN-MYSQL",
		Name:        "Clean MySQL",
		Description: "Clean MySQL installation (all phases)",
		Tags:        []string{"clean", "mysql"},
		PreCheck: func(ctx *runner.StepContext) error {
			for _, step := range GetMysqlCleanSteps() {
				if step.PreCheck != nil {
					if err := step.PreCheck(ctx); err != nil && !step.Optional {
						return err
					}
				}
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			for _, step := range GetMysqlCleanSteps() {
				if step.Action == nil {
					continue
				}
				if step.PreCheck != nil {
					if err := step.PreCheck(ctx); err != nil {
						if step.Optional {
							continue
						}
						return err
					}
				}
				if err := step.Action(ctx); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
