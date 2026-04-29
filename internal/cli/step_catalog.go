package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/yinstall/internal/runner"
	"github.com/yinstall/internal/steps/clean"
	dbsteps "github.com/yinstall/internal/steps/db"
	ossteps "github.com/yinstall/internal/steps/os"
	standbysteps "github.com/yinstall/internal/steps/standby"
	ycmsteps "github.com/yinstall/internal/steps/ycm"
	ympsteps "github.com/yinstall/internal/steps/ymp"
)

func printStepSection(title string, steps []*runner.Step) {
	fmt.Fprintf(os.Stdout, "\n%s\n", title)
	sepLen := len(title) + 8
	if sepLen > 72 {
		sepLen = 72
	}
	fmt.Fprintln(os.Stdout, strings.Repeat("-", sepLen))
	if len(steps) == 0 {
		fmt.Fprintln(os.Stdout, "(none)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tID\tNAME\tOPT\tGLOBAL\tDANGER\tDESCRIPTION\tTAGS")
	for i, s := range steps {
		opt, glob, dang := "no", "no", "no"
		if s.Optional {
			opt = "yes"
		}
		if s.Global {
			glob = "yes"
		}
		if s.Dangerous {
			dang = "yes"
		}
		desc := strings.ReplaceAll(s.Description, "\n", " ")
		if len(desc) > 120 {
			desc = desc[:117] + "..."
		}
		tags := strings.Join(s.Tags, ",")
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1, s.ID, s.Name, opt, glob, dang, desc, tags)
	}
	_ = w.Flush()
}

func osStepsB001Only() []*runner.Step {
	for _, s := range ossteps.GetAllSteps() {
		if s.ID == "B-001" {
			return []*runner.Step{s}
		}
	}
	return nil
}

// PrintOSStepCatalog 打印 OS 基线 steps（yinstall os）。
func PrintOSStepCatalog() {
	fmt.Fprintln(os.Stdout, "yinstall os - step catalog (execution order)")
	fmt.Fprintln(os.Stdout, "Use global filters: -s/--include-steps, -e/--exclude-steps")
	printStepSection("OS baseline steps", ossteps.GetAllSteps())
	fmt.Fprintln(os.Stdout, "")
}

// PrintDBStepCatalog 打印 yinstall db 的 steps（OS 前置 + DB）。
func PrintDBStepCatalog(skipOS bool) {
	fmt.Fprintln(os.Stdout, "yinstall db - step catalog (typical execution order)")
	if skipOS {
		printStepSection("OS (only when --skip-os: connectivity)", osStepsB001Only())
	} else {
		printStepSection("OS baseline (when --skip-os=false, default)", ossteps.GetAllSteps())
	}
	printStepSection("Database installation", dbsteps.GetAllSteps())
	fmt.Fprintln(os.Stdout, "Note: combined list may be filtered by -s/--include-steps, -e/--exclude-steps (wins over -s for same ID), or --skip-os.")
	fmt.Fprintln(os.Stdout, "")
}

// PrintStandbyStepCatalog 打印 OS 前置 + standby steps，与默认 allSteps 布局一致：
// （skipOS=true → 仅 B-001；skipOS=false → 完整 OS 基线），然后是 E-001…E-019。
// 实际执行是分阶段的（primary / per-standby / primary）；详见 standby 命令说明。
func PrintStandbyStepCatalog(skipOS bool) {
	fmt.Fprintln(os.Stdout, "yinstall standby - step catalog")
	fmt.Fprintln(os.Stdout, "Layout matches combined step list before -s/-e filters. Runtime order is phased (see logs: Phase 1-6+).")
	if skipOS {
		printStepSection("OS (default --skip-os=true: B-001 on each standby)", osStepsB001Only())
	} else {
		printStepSection("OS baseline (--skip-os=false, on each standby)", ossteps.GetAllSteps())
	}
	printStepSection("Standby / expansion steps", standbysteps.GetAllSteps())
	fmt.Fprintln(os.Stdout, "E-018 is optional/dangerous: without --force-steps E-018 (or -f) or --standby-cleanup-on-failure, it is skipped when executed.")
	fmt.Fprintln(os.Stdout, "")
}

// PrintYCMStepCatalog 打印 YCM 安装 steps 与 OS 前置 steps。
func PrintYCMStepCatalog(skipOS bool) {
	fmt.Fprintln(os.Stdout, "yinstall ycm - step catalog (typical execution order)")
	if skipOS {
		printStepSection("OS (only when --skip-os: connectivity)", osStepsB001Only())
	} else {
		printStepSection("OS baseline (when --skip-os=false)", ossteps.GetAllSteps())
	}
	printStepSection("YCM installation", ycmsteps.GetAllSteps())
	fmt.Fprintln(os.Stdout, "")
}

// PrintYMPStepCatalog 打印 YMP 安装 steps 与最小 OS 前置 steps。
func PrintYMPStepCatalog(skipOS bool) {
	fmt.Fprintln(os.Stdout, "yinstall ymp - step catalog (typical execution order)")
	if skipOS {
		printStepSection("OS (only when --skip-os: connectivity)", osStepsB001Only())
	} else {
		printStepSection("OS (YMP minimal subset, when --skip-os=false)", getYMPRequiredOSSteps())
	}
	printStepSection("YMP installation", ympsteps.GetAllSteps())
	fmt.Fprintln(os.Stdout, "")
}

// PrintCleanStepCatalog 打印 clean 命令的 steps。
func PrintCleanStepCatalog() {
	fmt.Fprintln(os.Stdout, "yinstall clean - step catalog")
	printStepSection("Top-level clean (orchestration)", clean.GetAllSteps())
	printStepSection("DB detailed substeps (--type db with --detailed-steps)", clean.GetDBCleanSteps())
	fmt.Fprintln(os.Stdout, "For --type ycm / ymp, only the matching top-level step runs, then its internal actions.")
	fmt.Fprintln(os.Stdout, "")
}
