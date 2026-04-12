package db

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

var (
	reGroupNodeConfigHeader = regexp.MustCompile(`^(\s*)\[group\.node\.config\]\s*(#.*)?\s*$`)
	reUseNativeLine         = regexp.MustCompile(`^\s*USE_NATIVE_TYPE\s*=`)
	reTableHeaderLine       = regexp.MustCompile(`^\s*\[`)
	reLegacyDBSection       = regexp.MustCompile(`^\[db\]\s*(#.*)?\s*$`)
)

// StepC018SetNativeType sets USE_NATIVE_TYPE in the cluster TOML (legacy [db] or yasboot [[group]] / [group.node.config]).
func StepC018SetNativeType() *runner.Step {
	return &runner.Step{
		ID:          "C-018",
		Name:        "Set Native Type",
		Description: "Configure USE_NATIVE_TYPE setting",
		Tags:        []string{"db", "config"},
		Optional:    true,

		PreCheck: func(ctx *runner.StepContext) error {
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			useNativeType := ctx.GetParamBool("db_use_native_type", true)
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")

			configPath := path.Join(stageDir, clusterName+".toml")

			var value string
			if useNativeType {
				value = "true"
			} else {
				value = "false"
			}

			ctx.Logger.Info("Setting USE_NATIVE_TYPE to: %s", value)

			if err := ensureUSENativeTypeInClusterTOML(ctx, configPath, value); err != nil {
				return err
			}

			result, _ := ctx.Execute(fmt.Sprintf(`grep '^[[:space:]]*USE_NATIVE_TYPE[[:space:]]*=' %s`, strconv.Quote(configPath)), false)
			if result != nil && result.GetStdout() != "" {
				ctx.Logger.Info("Config updated: %s", result.GetStdout())
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}

func normalizeTomlLines(content string) []string {
	s := strings.ReplaceAll(content, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

func leadingWhitespacePrefix(s string) string {
	i := 0
	for _, r := range s {
		if r == ' ' || r == '\t' {
			i++
			continue
		}
		break
	}
	return s[:i]
}

func containsGroupNodeConfigSection(lines []string) bool {
	for _, ln := range lines {
		if reGroupNodeConfigHeader.MatchString(ln) {
			return true
		}
	}
	return false
}

func containsLegacyDBSection(lines []string) bool {
	for _, ln := range lines {
		if reLegacyDBSection.MatchString(ln) {
			return true
		}
	}
	return false
}

func stripUseNativeLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if reUseNativeLine.MatchString(ln) {
			continue
		}
		out = append(out, ln)
	}
	return out
}

// insertUseNativeUnderEachGroupNodeConfig strips must be done first. Inserts one USE_NATIVE_TYPE per [group.node.config] table.
func insertUseNativeUnderEachGroupNodeConfig(lines []string, value string) []string {
	out := make([]string, 0, len(lines)+8)
	for i := 0; i < len(lines); {
		ln := lines[i]
		m := reGroupNodeConfigHeader.FindStringSubmatch(ln)
		if m == nil {
			out = append(out, ln)
			i++
			continue
		}
		headerSpaces := m[1]
		keyPrefix := headerSpaces + "  "
		out = append(out, ln)
		i++
		inserted := false
		j := i
		for j < len(lines) {
			nl := lines[j]
			ts := strings.TrimSpace(nl)
			if ts == "" {
				out = append(out, nl)
				j++
				continue
			}
			if strings.HasPrefix(ts, "#") {
				out = append(out, nl)
				j++
				continue
			}
			if reTableHeaderLine.MatchString(nl) {
				out = append(out, keyPrefix+fmt.Sprintf("USE_NATIVE_TYPE = %s", value))
				out = append(out, nl)
				j++
				inserted = true
				break
			}
			pref := leadingWhitespacePrefix(nl)
			out = append(out, pref+fmt.Sprintf("USE_NATIVE_TYPE = %s", value))
			out = append(out, nl)
			j++
			inserted = true
			break
		}
		if !inserted {
			out = append(out, keyPrefix+fmt.Sprintf("USE_NATIVE_TYPE = %s", value))
		}
		i = j
	}
	return out
}

func countGroupNodeConfigHeaders(lines []string) int {
	n := 0
	for _, ln := range lines {
		if reGroupNodeConfigHeader.MatchString(ln) {
			n++
		}
	}
	return n
}

func countUseNativeLines(lines []string) int {
	n := 0
	for _, ln := range lines {
		if reUseNativeLine.MatchString(ln) {
			n++
		}
	}
	return n
}

func verifyUseNativePerNodeConfigLines(lines []string) error {
	s := countGroupNodeConfigHeaders(lines)
	k := countUseNativeLines(lines)
	if s == 0 || k != s {
		return fmt.Errorf("[group.node.config] sections=%d USE_NATIVE_TYPE lines=%d (must be equal and non-zero)", s, k)
	}
	return nil
}

func readRemoteTextFile(ctx *runner.StepContext, filePath string) (string, error) {
	q := strconv.Quote(filePath)
	r, err := ctx.Execute(fmt.Sprintf("cat %s", q), false)
	if err != nil {
		return "", err
	}
	if r == nil || r.GetExitCode() != 0 {
		msg := ""
		if r != nil {
			msg = strings.TrimSpace(r.GetStderr())
			if msg == "" {
				msg = strings.TrimSpace(r.GetStdout())
			}
		}
		return "", fmt.Errorf("read %s failed: %s", filePath, msg)
	}
	return r.GetStdout(), nil
}

func writeRemoteTextViaUpload(ctx *runner.StepContext, dstPath, content string) error {
	f, err := os.CreateTemp("", "yinstall-cluster-*.toml")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpLocal := f.Name()
	defer func() { _ = os.Remove(tmpLocal) }()

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	remoteTmp := dstPath + ".yinstalltmp"
	if err := ctx.Executor.Upload(tmpLocal, remoteTmp); err != nil {
		return fmt.Errorf("upload temp cluster toml: %w", err)
	}
	q := strconv.Quote(dstPath)
	qtmp := strconv.Quote(remoteTmp)
	if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("mv %s %s", qtmp, q), false); err != nil {
		return err
	}
	return nil
}

// ensureUSENativeTypeInClusterTOML updates or inserts USE_NATIVE_TYPE for yasboot-generated layouts.
func ensureUSENativeTypeInClusterTOML(ctx *runner.StepContext, configPath, value string) error {
	q := strconv.Quote(configPath)

	content, err := readRemoteTextFile(ctx, configPath)
	if err != nil {
		return err
	}
	lines := normalizeTomlLines(content)

	if containsGroupNodeConfigSection(lines) {
		stripped := stripUseNativeLines(lines)
		updated := insertUseNativeUnderEachGroupNodeConfig(stripped, value)
		if err := verifyUseNativePerNodeConfigLines(updated); err != nil {
			return fmt.Errorf("internal: %w", err)
		}
		newContent := strings.Join(updated, "\n")
		if err := writeRemoteTextViaUpload(ctx, configPath, newContent); err != nil {
			return err
		}
		after, err := readRemoteTextFile(ctx, configPath)
		if err != nil {
			return err
		}
		if err := verifyUseNativePerNodeConfigLines(normalizeTomlLines(after)); err != nil {
			return fmt.Errorf("verify after write: %w", err)
		}
		return nil
	}

	// Legacy layout: top-level [db] section.
	if !containsLegacyDBSection(lines) {
		return fmt.Errorf("cluster config %s has no [group.node.config] and no [db] section; cannot set USE_NATIVE_TYPE", configPath)
	}

	result, err := ctx.Execute(fmt.Sprintf(`grep -q '^[[:space:]]*USE_NATIVE_TYPE[[:space:]]*=' %s`, q), false)
	if err != nil {
		return err
	}
	if result != nil && result.GetExitCode() == 0 {
		cmd := fmt.Sprintf(`sed -i 's/^\([[:space:]]*\)USE_NATIVE_TYPE.*/\1USE_NATIVE_TYPE = %s/' %s`, value, q)
		if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
			return fmt.Errorf("failed to update USE_NATIVE_TYPE: %w", err)
		}
	} else {
		cmd := fmt.Sprintf(`sed -i '/^\[db\]/a USE_NATIVE_TYPE = %s' %s`, value, q)
		if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
			return fmt.Errorf("failed to append USE_NATIVE_TYPE under [db]: %w", err)
		}
	}
	return verifyUSENativeTypeAny(ctx, q)
}

func verifyUSENativeTypeAny(ctx *runner.StepContext, quotedPath string) error {
	result, err := ctx.Execute(fmt.Sprintf(`grep -q '^[[:space:]]*USE_NATIVE_TYPE[[:space:]]*=' %s`, quotedPath), false)
	if err != nil {
		return err
	}
	if result == nil || result.GetExitCode() != 0 {
		return fmt.Errorf("USE_NATIVE_TYPE missing in cluster config after update (grep verification failed)")
	}
	return nil
}
