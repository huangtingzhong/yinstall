// s011_finalize.go - 压测结束：生成 manifest.json 和 summary.md
// 聚合所有主机的压测结果、错误/警告，输出英文摘要文件。
// 所有文本必须为 ASCII 英文，不含 CJK 字符。
package stressos

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yinstall/internal/common/archive"
	"github.com/yinstall/internal/runner"
)

// stepFinalize 返回 S-11 步骤：生成最终摘要文件。
func stepFinalize() *runner.Step {
	return &runner.Step{
		Name: "Finalize stress report",
		Action: func(ctx *runner.StepContext) error {
			rootDir := stressRootDir(ctx)

			errors, _ := ctx.Results[keyStressErrors].([]map[string]string)
			warnings, _ := ctx.Results[keyStressWarnings].([]map[string]string)

			hosts := s11BuildHostsList(ctx)

			manifest := map[string]interface{}{
				"version":       "1",
				"stress_at":     time.Now().UTC().Format(time.RFC3339),
				"hosts":         hosts,
				"error_count":   len(errors),
				"warning_count": len(warnings),
				"errors":        errors,
				"warnings":      warnings,
				"output_dir":    rootDir,
				"stress_tests":  s11BuildTestsList(ctx),
			}

			if err := writeJSON(filepath.Join(rootDir, "manifest.json"), manifest); err != nil {
				return fmt.Errorf("write manifest.json: %w", err)
			}

			if err := s11WriteSummaryMD(rootDir, manifest, ctx); err != nil {
				appendWarning(ctx, fmt.Sprintf("write summary.md: %v", err))
			}

			if ctx.GetParamBool("archive_no_pack", false) {
				stressLogPhase(ctx, "pack-skip", "no-pack")
			} else {
				stressLogPhase(ctx, "pack-start", fmt.Sprintf("dir=%s try=tar.gz,zip", rootDir))
				res, err := archive.PackDirAuto(rootDir)
				if err != nil {
					appendWarning(ctx, fmt.Sprintf("pack archive: %v", err))
					archive.NotifyPackOutcome(err.Error())
				} else if res.Skipped {
					appendWarning(ctx, res.Message)
					ctx.Logger.Warn("[S-11] %s", res.Message)
					archive.NotifyPackOutcome(res.Message)
					stressLogPhase(ctx, "pack-skip", "all formats failed")
				} else {
					if res.Message != "" {
						appendWarning(ctx, res.Message)
						ctx.Logger.Warn("[S-11] %s", res.Message)
						archive.NotifyPackOutcome(res.Message)
					}
					manifest["archive_path"] = res.ArchivePath
					manifest["archive_format"] = res.Format
					if err := writeJSON(filepath.Join(rootDir, "manifest.json"), manifest); err != nil {
						appendWarning(ctx, fmt.Sprintf("update manifest with archive_path: %v", err))
					}
					ctx.SetResult("archive_path", res.ArchivePath)
					stressLogPhase(ctx, "pack-done", fmt.Sprintf("format=%s path=%s", res.Format, res.ArchivePath))
					ctx.Logger.Info("[S-11] archive written: %s", res.ArchivePath)
				}
			}

			ctx.Logger.Info("[S-11] manifest and summary written to %s", rootDir)
			return nil
		},
	}
}

// s11BuildHostsList 从 TargetHosts 构建主机列表。
func s11BuildHostsList(ctx *runner.StepContext) []string {
	hosts := make([]string, 0, len(ctx.TargetHosts))
	for _, th := range ctx.TargetHosts {
		hosts = append(hosts, th.Host)
	}
	if len(hosts) == 0 && ctx.Executor != nil {
		hosts = append(hosts, ctx.Executor.Host())
	}
	return hosts
}

// s11BuildTestsList 从 ctx.Params 构建实际启用的测试列表。
func s11BuildTestsList(ctx *runner.StepContext) []string {
	var tests []string
	if getBool(ctx, "stress_cpu", true) {
		tests = append(tests, "cpu")
	}
	if getBool(ctx, "stress_mem", true) {
		tests = append(tests, "mem")
	}
	if getBool(ctx, "stress_io", true) {
		tests = append(tests, "io")
	}
	if getBool(ctx, "stress_net", false) {
		switch getStr(ctx, "stress_net_mode", "ping") {
		case "yac":
			tests = append(tests, "net-yac-ping-mesh", "net-iperf3-yac")
		default:
			tests = append(tests, "net-ping")
		}
	}
	return tests
}

// s11WriteSummaryMD 生成英文 Markdown 摘要文件（所有文本必须为 ASCII 英文）。
func s11WriteSummaryMD(rootDir string, manifest map[string]interface{}, ctx *runner.StepContext) error {
	hosts, _ := manifest["hosts"].([]string)
	errorCount, _ := manifest["error_count"].(int)
	warningCount, _ := manifest["warning_count"].(int)
	stressAt, _ := manifest["stress_at"].(string)
	tests, _ := manifest["stress_tests"].([]string)

	var sb strings.Builder
	sb.WriteString("# yinstall stressos Summary\n\n")
	sb.WriteString(fmt.Sprintf("**Stress Test Time**: %s\n\n", stressAt))
	sb.WriteString(fmt.Sprintf("**Hosts**: %s\n\n", strings.Join(hosts, ", ")))
	sb.WriteString(fmt.Sprintf("**Tests Enabled**: %s\n\n", strings.Join(tests, ", ")))
	sb.WriteString(fmt.Sprintf("**Output Directory**: %s\n\n", rootDir))
	sb.WriteString(fmt.Sprintf("**Errors**: %d  |  **Warnings**: %d\n\n", errorCount, warningCount))

	if errorCount > 0 {
		sb.WriteString("## Errors\n\n")
		if errs, ok := manifest["errors"].([]map[string]string); ok {
			for _, e := range errs {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", e["step"], e["message"]))
			}
		}
		sb.WriteString("\n")
	}

	if warningCount > 0 {
		sb.WriteString("## Warnings\n\n")
		if warns, ok := manifest["warnings"].([]map[string]string); ok {
			for _, w := range warns {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", w["step"], w["message"]))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Result Directory Structure\n\n")
	sb.WriteString("```\n")
	sb.WriteString(rootDir + "/\n")
	sb.WriteString("  manifest.json      - this manifest\n")
	sb.WriteString("  summary.md         - this summary\n")
	if arc, _ := manifest["archive_path"].(string); arc != "" {
		sb.WriteString(fmt.Sprintf("  %s  - packaged archive (sibling of this directory)\n", filepath.Base(arc)))
	}
	sb.WriteString("  hosts/\n")
	for _, h := range hosts {
		safe := strings.NewReplacer(":", "_").Replace(h)
		sb.WriteString(fmt.Sprintf("    %s/\n", safe))
		sb.WriteString("      meta.json           - host/OS identity (from S-01 + OSInfo)\n")
		sb.WriteString("      os/identity/summary.json\n")
		sb.WriteString("      deps/install_report.txt\n")
		sb.WriteString("      cpu/          - sysbench cpu (single, nproc, 2*nproc, NUMA node bind)\n")
		sb.WriteString("      mem/          - sysbench memory results\n")
		sb.WriteString("      io/           - fio results (3 scenarios)\n")
		if getStr(ctx, "stress_net_mode", "ping") == "yac" {
			sb.WriteString("  yac/net/        - iperf3: first -t=server, others=clients (iperf3_client_*.txt)\n")
			sb.WriteString("      net/          - ping_<peer>.txt between all YAC nodes\n")
		} else {
			sb.WriteString("      net/          - ping latency results\n")
		}
		sb.WriteString("      runtime/      - S-09 snapshot metrics\n")
		sb.WriteString("      runtime/bg/   - S-04/S-10 continuous perf logs\n")
	}
	sb.WriteString("```\n\n")
	sb.WriteString("*Generated by yinstall stressos*\n")

	summaryPath := filepath.Join(rootDir, "summary.md")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(summaryPath, []byte(sb.String()), 0o644)
}
