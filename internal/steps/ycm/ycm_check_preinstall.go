// g001_check_preinstall.go - YCM 装前门禁（端口、目录、进程、systemd）
// G-001: 在安装前检查环境冲突；须在 G-003 等 mutating 步骤之前执行

package ycm

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

type ycmPortDef struct {
	name     string
	paramKey string
	defVal   int
}

func ycmPortDefs() []ycmPortDef {
	return []ycmPortDef{
		{"YCM Web", "ycm_port", 9060},
		{"Prometheus", "ycm_prometheus_port", 9061},
		{"Loki HTTP", "ycm_loki_http_port", 9062},
		{"Loki gRPC", "ycm_loki_grpc_port", 9063},
		{"YasDB Exporter", "ycm_yasdb_exporter_port", 9064},
	}
}

// YCMHomeFromContext 返回 YCM 安装根目录（默认 {ycm_install_dir}/ycm）。
func YCMHomeFromContext(ctx *runner.StepContext) string {
	if ctx == nil {
		return "/opt/ycm"
	}
	if h := strings.TrimSpace(ctx.GetParamString("ycm_home", "")); h != "" {
		return strings.TrimRight(h, "/")
	}
	installDir := strings.TrimRight(strings.TrimSpace(ctx.GetParamString("ycm_install_dir", "/opt")), "/")
	if installDir == "" {
		installDir = "/opt"
	}
	return installDir + "/ycm"
}

type ycmPrecheckSummary struct {
	Host                string
	YCMHome             string
	InstallDir          string
	WebPort             int
	InstallDirInferred  bool
	UsedPorts           []string
	PortDetails         []string
	ProcessCount        int
	SystemdService      string
	SystemdActive       string
	SystemdUnitExists   bool
	DirNonEmpty         bool
	DirHasYCMArtifacts  bool
	ExistingYCMInstance bool
}

func (s ycmPrecheckSummary) installLayout() InstallLayout {
	return InstallLayout{
		InstallDir:         s.InstallDir,
		YCMHome:            s.YCMHome,
		WebPort:            s.WebPort,
		InstallDirInferred: s.InstallDirInferred,
	}
}

func portListenerLooksLikeYCM(portDetail, ycmHome string) bool {
	if strings.TrimSpace(portDetail) == "" {
		return false
	}
	lower := strings.ToLower(portDetail)
	if strings.Contains(lower, "ycm") {
		return true
	}
	home := strings.TrimRight(strings.TrimSpace(ycmHome), "/")
	if home != "" && strings.Contains(portDetail, home) {
		return true
	}
	return false
}

func formatYCMPrecheckError(summary ycmPrecheckSummary) string {
	var b strings.Builder
	if summary.ExistingYCMInstance {
		fmt.Fprintf(&b, "YCM appears already installed on %s", summary.Host)
	} else {
		fmt.Fprintf(&b, "YCM pre-install check failed on %s", summary.Host)
	}
	if len(summary.UsedPorts) > 0 {
		fmt.Fprintf(&b, "\n  - ports in use: %s", strings.Join(summary.UsedPorts, ", "))
	}
	if summary.ProcessCount > 0 {
		fmt.Fprintf(&b, "\n  - processes: %d under %s", summary.ProcessCount, summary.YCMHome)
	}
	if summary.SystemdActive == "active" {
		fmt.Fprintf(&b, "\n  - systemd: %s.service is active", summary.SystemdService)
	} else if summary.SystemdUnitExists {
		fmt.Fprintf(&b, "\n  - systemd: %s.service unit exists (inactive)", summary.SystemdService)
	}
	if summary.DirNonEmpty {
		if summary.DirHasYCMArtifacts {
			fmt.Fprintf(&b, "\n  - install directory is not empty (existing YCM files)")
		} else {
			fmt.Fprintf(&b, "\n  - install directory is not empty (does not look like YCM)")
		}
	}
	if summary.ExistingYCMInstance {
		fmt.Fprintf(&b, "\nRemediation: %s ; then re-run yinstall ycm", FormatCleanRemediation(summary.Host, summary.installLayout()))
	} else {
		fmt.Fprint(&b, "\nRemediation: stop conflicting processes, choose different --ycm-port (and related flags), or use a different --ycm-install-dir")
	}
	if summary.DirNonEmpty && summary.DirHasYCMArtifacts && !summary.ExistingYCMInstance {
		fmt.Fprintf(&b, "\nOr wipe install dir: %s", FormatYCMWipeCommand(summary.Host, summary.installLayout()))
	}
	return b.String()
}

func checkYCMPreinstall(ctx *runner.StepContext) (ycmPrecheckSummary, error) {
	host := ""
	if ctx.Executor != nil {
		host = ctx.Executor.Host()
	}
	ycmHome := YCMHomeFromContext(ctx)
	homeQ := commonos.ShellSingleQuote(ycmHome)
	installDir := strings.TrimRight(strings.TrimSpace(ctx.GetParamString("ycm_install_dir", "/opt")), "/")
	if installDir == "" {
		installDir = "/opt"
	}
	webPort := ctx.GetParamInt("ycm_port", defaultYCMPort)
	installDirInferred := webPort != defaultYCMPort && installDir == fmt.Sprintf("/opt/ycm_%d", webPort)
	summary := ycmPrecheckSummary{
		Host:               host,
		YCMHome:            ycmHome,
		InstallDir:         installDir,
		WebPort:            webPort,
		InstallDirInferred: installDirInferred,
	}

	// 端口占用（不可 force 跳过）
	for _, p := range ycmPortDefs() {
		portVal := ctx.GetParamInt(p.paramKey, p.defVal)
		ok, detail := isPortListening(ctx, portVal)
		if !ok {
			continue
		}
		summary.UsedPorts = append(summary.UsedPorts, fmt.Sprintf("%d(%s)", portVal, p.name))
		summary.PortDetails = append(summary.PortDetails, detail)
		if portListenerLooksLikeYCM(detail, ycmHome) {
			summary.ExistingYCMInstance = true
		}
	}
	for _, p := range ycmAuxListenPorts(webPort) {
		ok, detail := isPortListening(ctx, p.port)
		if !ok {
			continue
		}
		summary.UsedPorts = append(summary.UsedPorts, fmt.Sprintf("%d(%s)", p.port, p.name))
		summary.PortDetails = append(summary.PortDetails, detail)
		if portListenerLooksLikeYCM(detail, ycmHome) {
			summary.ExistingYCMInstance = true
		}
	}

	// 进程（不可 force 跳过）：按本实例 ycm_home 匹配
	homePattern := ycmHome
	if !strings.HasSuffix(homePattern, "/") {
		homePattern += "/"
	}
	processCount, processLines, _ := countYCMProcesses(ctx, homePattern)
	summary.ProcessCount = processCount
	if processCount > 0 {
		summary.ExistingYCMInstance = true
		for _, line := range processLines {
			ctx.Logger.Warn("YCM process already running: %s", line)
		}
	}

	// systemd
	summary.SystemdService = ServiceNameFromContext(ctx)
	if commonos.CheckSystemdAvailable(ctx) {
		unitPath := AutostartUnitPath(summary.SystemdService)
		unitQ := commonos.ShellSingleQuote(unitPath)
		r, _ := ctx.Execute(fmt.Sprintf("test -f %s", unitQ), false)
		summary.SystemdUnitExists = r != nil && r.GetExitCode() == 0
		summary.SystemdActive = systemdProp(ctx, summary.SystemdService, "is-active")
		if summary.SystemdActive == "active" {
			summary.ExistingYCMInstance = true
		}
	}

	// 安装目录
	dirExists := false
	r, _ := ctx.Execute(fmt.Sprintf("test -d %s", homeQ), false)
	if r != nil && r.GetExitCode() == 0 {
		dirExists = true
	}
	if dirExists {
		checkCmd := fmt.Sprintf("find %s -mindepth 1 -maxdepth 1 2>/dev/null | head -1", homeQ)
		r, _ = ctx.Execute(checkCmd, false)
		hasContent := r != nil && r.GetExitCode() == 0 && strings.TrimSpace(r.GetStdout()) != ""
		if hasContent {
			summary.DirNonEmpty = true
			artifactCmd := fmt.Sprintf("test -f %s/etc/deploy.yml || test -f %s/ycm-init || test -f %s/bin/ycm-init",
				homeQ, homeQ, homeQ)
			// also check parent install dir for ycm-init tarball layout
			installQ := commonos.ShellSingleQuote(installDir)
			artifactCmd = fmt.Sprintf("%s || test -f %s/ycm-init", artifactCmd, installQ)
			ar, _ := ctx.Execute(artifactCmd, false)
			summary.DirHasYCMArtifacts = ar != nil && ar.GetExitCode() == 0
			if summary.DirHasYCMArtifacts {
				summary.ExistingYCMInstance = true
			}
		}
	}

	allowDirForce := ctx.IsForceStepID(StepIDByName("Extract YCM Package"))
	var blockers []string

	if len(summary.UsedPorts) > 0 {
		blockers = append(blockers, fmt.Sprintf("ports in use: %s", strings.Join(summary.UsedPorts, ", ")))
	}
	if summary.ProcessCount > 0 {
		blockers = append(blockers, fmt.Sprintf("%d YCM process(es) under %s", summary.ProcessCount, ycmHome))
	}
	if summary.SystemdActive == "active" {
		blockers = append(blockers, fmt.Sprintf("systemd unit %s is active", summary.SystemdService))
	}
	if summary.DirNonEmpty {
		if !summary.DirHasYCMArtifacts {
			msg := fmt.Sprintf("directory %s is not empty and does not look like YCM; use a different --ycm-install-dir", ycmHome)
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Check YCM Pre-install Prerequisites",
				Host:        host,
				Severity:    runner.PrecheckSeverityError,
				Code:        "PC.YCM.INSTALL_DIR.FOREIGN_ENTRIES",
				Message:     msg,
				Remediation: "move or remove foreign files, or choose another install path",
			})
			blockers = append(blockers, msg)
		} else if allowDirForce {
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Check YCM Pre-install Prerequisites",
				Host:        host,
				Severity:    runner.PrecheckSeverityWarn,
				Code:        "PC.YCM.INSTALL_DIR.FORCE_DELETE",
				Message:     fmt.Sprintf("YCM install directory exists and is not empty: %s; -f G-003 detected; G-003 will rm -rf before extract", ycmHome),
				Remediation: "ensure no important data remains; for running instance use clean instead",
			})
		} else {
			msg := fmt.Sprintf("YCM install directory %s is not empty", ycmHome)
			cleanCmd := FormatCleanRemediation(host, summary.installLayout())
			ctx.ReportPrecheckIssue(runner.PrecheckIssue{
				StepName:    "Check YCM Pre-install Prerequisites",
				Host:        host,
				Severity:    runner.PrecheckSeverityError,
				Code:        "PC.YCM.INSTALL_DIR.NOT_EMPTY",
				Message:     msg,
				Remediation: cleanCmd + " ; or " + FormatYCMWipeCommand(host, summary.installLayout()) + " to wipe directory only (requires no running YCM)",
			})
			blockers = append(blockers, msg)
		}
	} else if !dirExists {
		ctx.ReportPrecheckIssue(runner.PrecheckIssue{
			StepName:    "Check YCM Pre-install Prerequisites",
			Host:        host,
			Severity:    runner.PrecheckSeverityInfo,
			Code:        "PC.YCM.INSTALL_DIR.MISSING",
			Message:     fmt.Sprintf("YCM install directory does not exist: %s; apply will create/extract", ycmHome),
			Remediation: "",
		})
	}

	if summary.SystemdUnitExists && summary.SystemdActive != "active" {
		ctx.ReportPrecheckIssue(runner.PrecheckIssue{
			StepName:    "Check YCM Pre-install Prerequisites",
			Host:        host,
			Severity:    runner.PrecheckSeverityWarn,
			Code:        "PC.YCM.SYSTEMD.UNIT_EXISTS",
			Message:     fmt.Sprintf("systemd unit %s exists but is not active", summary.SystemdService),
			Remediation: "clean will remove the unit; or ignore if intentional",
		})
	}

	if len(blockers) == 0 {
		return summary, nil
	}
	return summary, fmt.Errorf("%s", formatYCMPrecheckError(summary))
}

func ensurePortProbeTools(ctx *runner.StepContext) error {
	result, _ := ctx.Execute("which ss 2>/dev/null || which netstat 2>/dev/null", false)
	if result == nil || result.GetExitCode() != 0 {
		return fmt.Errorf("neither ss nor netstat command found")
	}
	return nil
}

// stepCheckPreinstall YCM 装前门禁
func stepCheckPreinstall() *runner.Step {
	return &runner.Step{
		Name:        "Check YCM Pre-install Prerequisites",
		Description: "Verify ports, install directory, processes, and systemd before installation",
		Tags:        []string{"ycm", "precheck", "network"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			if err := ensurePortProbeTools(ctx); err != nil {
				return err
			}
			_, err := checkYCMPreinstall(ctx)
			return err
		},

		Action: func(ctx *runner.StepContext) error {
			ycmLogPhase(ctx, "plan", "G-001: Check YCM Pre-install Prerequisites")
			if err := ensurePortProbeTools(ctx); err != nil {
				return err
			}

			summary, err := checkYCMPreinstall(ctx)
			if err != nil {
				return err
			}

			ctx.Logger.Info("OK: YCM pre-install checks passed on %s (ycm_home=%s)", summary.Host, YCMHomeFromContext(ctx))
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
