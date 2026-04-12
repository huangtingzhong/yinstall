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

// PrintOSStepCatalog lists OS baseline steps (yinstall os).
func PrintOSStepCatalog() {
	fmt.Fprintln(os.Stdout, "yinstall os — step catalog (execution order)")
	fmt.Fprintln(os.Stdout, "Use global filters: -s/--include-steps, --exclude-steps, --include-tags, --exclude-tags")
	printStepSection("OS baseline steps", ossteps.GetAllSteps())
	fmt.Fprintln(os.Stdout, "")
}

// PrintDBStepCatalog lists steps for yinstall db (OS prefix + DB).
func PrintDBStepCatalog(skipOS bool) {
	fmt.Fprintln(os.Stdout, "yinstall db — step catalog (typical execution order)")
	if skipOS {
		printStepSection("OS (only when --skip-os: connectivity)", osStepsB001Only())
	} else {
		printStepSection("OS baseline (when --skip-os=false, default)", ossteps.GetAllSteps())
	}
	printStepSection("Database installation", dbsteps.GetAllSteps())
	fmt.Fprintln(os.Stdout, "Note: combined list may be filtered by -s/--include-steps, --exclude-steps (wins over -s for same ID), tags, or --skip-os.")
	fmt.Fprintln(os.Stdout, "")
}

// PrintStandbyStepCatalog lists standby expansion steps.
func PrintStandbyStepCatalog() {
	fmt.Fprintln(os.Stdout, "yinstall standby — step catalog (execution order)")
	printStepSection("Standby / expansion steps", standbysteps.GetAllSteps())
	fmt.Fprintln(os.Stdout, "")
}

// PrintYCMStepCatalog lists YCM install steps and OS prefix.
func PrintYCMStepCatalog(skipOS bool) {
	fmt.Fprintln(os.Stdout, "yinstall ycm — step catalog (typical execution order)")
	if skipOS {
		printStepSection("OS (only when --skip-os: connectivity)", osStepsB001Only())
	} else {
		printStepSection("OS baseline (when --skip-os=false)", ossteps.GetAllSteps())
	}
	printStepSection("YCM installation", ycmsteps.GetAllSteps())
	fmt.Fprintln(os.Stdout, "")
}

// PrintYMPStepCatalog lists YMP install steps and minimal OS prefix.
func PrintYMPStepCatalog(skipOS bool) {
	fmt.Fprintln(os.Stdout, "yinstall ymp — step catalog (typical execution order)")
	if skipOS {
		printStepSection("OS (only when --skip-os: connectivity)", osStepsB001Only())
	} else {
		printStepSection("OS (YMP minimal subset, when --skip-os=false)", getYMPRequiredOSSteps())
	}
	printStepSection("YMP installation", ympsteps.GetAllSteps())
	fmt.Fprintln(os.Stdout, "")
}

// PrintCleanStepCatalog lists clean command steps.
func PrintCleanStepCatalog() {
	fmt.Fprintln(os.Stdout, "yinstall clean — step catalog")
	printStepSection("Top-level clean (orchestration)", clean.GetAllSteps())
	printStepSection("DB detailed substeps (--type db with --detailed-steps)", clean.GetDBCleanSteps())
	fmt.Fprintln(os.Stdout, "For --type ycm / ymp, only the matching top-level step runs, then its internal actions.")
	fmt.Fprintln(os.Stdout, "")
}
