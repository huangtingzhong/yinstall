// r029_finalize.go - 采集结束：生成 manifest.json 和 summary.md
// Global 步骤，聚合所有主机的错误/警告，输出英文摘要文件。
// manifest.json 记录每步执行状态；summary.md 仅用英文，不含 CJK 字符。
package collect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yinstall/internal/common/archive"
	"github.com/yinstall/internal/runner"
)

// stepFinalize 返回 R-029 步骤：生成最终摘要文件。
// 此步骤由 collect.go 在所有 per-host 步骤完成后单独驱动（后置步骤），
// 不使用框架的 Global 机制（Global 步骤先于 per-host 步骤执行，顺序相反）。
func stepFinalize() *runner.Step {
	return &runner.Step{
		Name: "Finalize manifest and summary",
		Action: func(ctx *runner.StepContext) error {
			rootDir := collectRootDir(ctx)

			// 聚合所有主机的错误/警告（从 Results 读取）
			errors, _ := ctx.Results[keyCollectErrors].([]map[string]string)
			warnings, _ := ctx.Results[keyCollectWarnings].([]map[string]string)

			manifest := map[string]interface{}{
				"version":       "1",
				"collected_at":  time.Now().UTC().Format(time.RFC3339),
				"hosts":         buildHostsList(ctx),
				"error_count":   len(errors),
				"warning_count": len(warnings),
				"errors":        errors,
				"warnings":      warnings,
				"output_dir":    rootDir,
			}

			if err := writeJSON(filepath.Join(rootDir, "manifest.json"), manifest); err != nil {
				return fmt.Errorf("write manifest.json: %w", err)
			}

			// 生成英文 summary.md（不含 CJK 字符）
			if err := writeSummaryMD(rootDir, manifest, ctx); err != nil {
				appendWarning(ctx, fmt.Sprintf("write summary.md: %v", err))
			}

			if ctx.GetParamBool("archive_no_pack", false) {
				collectLogPhase(ctx, "pack-skip", "no-pack")
			} else {
				collectLogPhase(ctx, "pack-start", fmt.Sprintf("dir=%s try=tar.gz,zip", rootDir))
				res, err := archive.PackDirAuto(rootDir)
				if err != nil {
					appendWarning(ctx, fmt.Sprintf("pack archive: %v", err))
					archive.NotifyPackOutcome(err.Error())
				} else if res.Skipped {
					appendWarning(ctx, res.Message)
					ctx.Logger.Warn("[R-029] %s", res.Message)
					archive.NotifyPackOutcome(res.Message)
					collectLogPhase(ctx, "pack-skip", "all formats failed")
				} else {
					if res.Message != "" {
						appendWarning(ctx, res.Message)
						ctx.Logger.Warn("[R-029] %s", res.Message)
						archive.NotifyPackOutcome(res.Message)
					}
					manifest["archive_path"] = res.ArchivePath
					manifest["archive_format"] = res.Format
					if err := writeJSON(filepath.Join(rootDir, "manifest.json"), manifest); err != nil {
						appendWarning(ctx, fmt.Sprintf("update manifest with archive_path: %v", err))
					}
					ctx.SetResult("archive_path", res.ArchivePath)
					collectLogPhase(ctx, "pack-done", fmt.Sprintf("format=%s path=%s", res.Format, res.ArchivePath))
					ctx.Logger.Info("[R-029] archive written: %s", res.ArchivePath)
				}
			}

			ctx.Logger.Info("[R-029] manifest and summary written to %s", rootDir)
			return nil
		},
	}
}

// buildHostsList 从 TargetHosts 构建主机列表。
func buildHostsList(ctx *runner.StepContext) []string {
	hosts := make([]string, 0, len(ctx.TargetHosts))
	for _, th := range ctx.TargetHosts {
		hosts = append(hosts, th.Host)
	}
	if len(hosts) == 0 && ctx.Executor != nil {
		hosts = append(hosts, ctx.Executor.Host())
	}
	return hosts
}

// writeSummaryMD 生成英文 Markdown 摘要文件（所有文本必须为 ASCII 英文）。
func writeSummaryMD(rootDir string, manifest map[string]interface{}, ctx *runner.StepContext) error {
	hosts, _ := manifest["hosts"].([]string)
	errorCount, _ := manifest["error_count"].(int)
	warningCount, _ := manifest["warning_count"].(int)
	collectedAt, _ := manifest["collected_at"].(string)

	var sb strings.Builder
	sb.WriteString("# YashanDB Collect Summary\n\n")
	sb.WriteString(fmt.Sprintf("**Collected At**: %s\n\n", collectedAt))
	sb.WriteString(fmt.Sprintf("**Hosts**: %s\n\n", strings.Join(hosts, ", ")))
	sb.WriteString(fmt.Sprintf("**Output Directory**: %s\n\n", rootDir))
	sb.WriteString(fmt.Sprintf("**Errors**: %d  |  **Warnings**: %d\n\n", errorCount, warningCount))

	if errorCount > 0 {
		sb.WriteString("## Errors\n\n")
		if errors, ok := manifest["errors"].([]map[string]string); ok {
			for _, e := range errors {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", e["step"], e["message"]))
			}
		}
		sb.WriteString("\n")
	}

	if warningCount > 0 {
		sb.WriteString("## Warnings\n\n")
		if warnings, ok := manifest["warnings"].([]map[string]string); ok {
			for _, w := range warnings {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", w["step"], w["message"]))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Directory Structure\n\n")
	sb.WriteString("```\n")
	sb.WriteString(rootDir + "/\n")
	sb.WriteString("  manifest.json        - This manifest\n")
	sb.WriteString("  summary.md           - This summary\n")
	if arc, _ := manifest["archive_path"].(string); arc != "" {
		sb.WriteString(fmt.Sprintf("  %s  - Packaged archive (sibling of this directory)\n", filepath.Base(arc)))
	}
	sb.WriteString("  hosts/\n")
	for _, h := range hosts {
		safe := strings.NewReplacer(":", "_").Replace(h)
		sb.WriteString(fmt.Sprintf("    %s/\n", safe))
		sb.WriteString("      meta.json\n")
		sb.WriteString("      os/           - OS baseline info\n")
		sb.WriteString("      db/           - Database info\n")
	}
	sb.WriteString("```\n\n")
	sb.WriteString("*Generated by yinstall collect*\n")

	summaryPath := filepath.Join(rootDir, "summary.md")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(summaryPath, []byte(sb.String()), 0o644)
}
