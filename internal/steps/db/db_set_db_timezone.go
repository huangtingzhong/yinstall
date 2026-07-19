package db

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

const (
	c035ResultResolved = "db_timezone_resolved"
)

var (
	reGroupConfigHeader = regexp.MustCompile(`^(\s*)\[group\.config\]\s*(#.*)?\s*$`)
	reTimeZoneLine      = regexp.MustCompile(`^\s*TIME_ZONE\s*=`)
)

// StepSetDBTimezone 在 yasboot gen 后的集群 TOML [group.config] 中设置 TIME_ZONE（建库前生效）。
func StepSetDBTimezone() *runner.Step {
	return &runner.Step{
		Name:        "Set DB Timezone",
		Description: "Configure TIME_ZONE in cluster TOML before database deployment",
		Tags:        []string{"db", "config", "time"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			res, _ := ctx.Execute(fmt.Sprintf("test -f %s", strconv.Quote(configPath)), false)
			if res == nil || res.GetExitCode() != 0 {
				return skipPrecheckDryRunWhenUpstreamDBArtifactMissing(ctx, fmt.Errorf("cluster config not found at %s (run C-014 first)", configPath))
			}

			dbRaw := strings.TrimSpace(ctx.GetParamString("db_timezone", ""))
			if dbRaw == "" {
				if err := validateHostsShareIANATimezone(ctx); err != nil {
					return err
				}
			}

			tz, err := resolveDBTimeZoneForInstall(ctx)
			if err != nil {
				return err
			}
			ctx.SetResult(c035ResultResolved, tz)
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			dbLogPhase(ctx, "plan", "C-035: Set DB Timezone")
			tz, err := resolveDBTimeZoneForInstall(ctx)
			if err != nil {
				return err
			}
			ctx.SetResult(c035ResultResolved, tz)
			dbLogPhase(ctx, "timezone-resolved", fmt.Sprintf("TIME_ZONE=%s", tz))

			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			ctx.Logger.Info("Setting TIME_ZONE to %s in %s", tz, configPath)
			if err := ensureTimeZoneInClusterTOML(ctx, configPath, tz); err != nil {
				return err
			}

			result, _ := ctx.Execute(fmt.Sprintf(`grep '^[[:space:]]*TIME_ZONE[[:space:]]*=' %s`, strconv.Quote(configPath)), false)
			if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
				ctx.Logger.Info("Config updated: %s", strings.TrimSpace(result.GetStdout()))
			}
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			tz, _ := ctx.Results[c035ResultResolved].(string)
			if strings.TrimSpace(tz) == "" {
				var err error
				tz, err = resolveDBTimeZoneForInstall(ctx)
				if err != nil {
					return err
				}
			}

			stageDir := ctx.GetParamString("db_stage_dir", "/home/yashan/install")
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			configPath := path.Join(stageDir, clusterName+".toml")

			content, err := readRemoteTextFile(ctx, configPath)
			if err != nil {
				return err
			}
			if err := verifyTimeZoneInClusterTOML(normalizeTomlLines(content), tz); err != nil {
				return err
			}
			ctx.Logger.Info("Verified TIME_ZONE = %s in cluster config", tz)
			return nil
		},
	}
}

func resolveDBTimeZoneForInstall(ctx *runner.StepContext) (string, error) {
	dbRaw := strings.TrimSpace(ctx.GetParamString("db_timezone", ""))
	if dbRaw != "" {
		return commonos.ParseDBTimeZoneInput(dbRaw)
	}

	iana, err := commonos.ReadHostIANATimezone(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot read OS timezone via timedatectl: %w; set --db-timezone explicitly (e.g. +08:00 or Asia/Shanghai)", err)
	}
	return commonos.IANAToYashanTimeZone(iana)
}

func validateHostsShareIANATimezone(ctx *runner.StepContext) error {
	hosts := ctx.HostsToRun()
	if len(hosts) == 0 {
		return nil
	}
	var ref string
	for _, th := range hosts {
		hctx := ctx.ForHost(th)
		tz, err := commonos.ReadHostIANATimezone(hctx)
		if err != nil {
			return fmt.Errorf("host %s: cannot read OS timezone via timedatectl: %w; set --db-timezone explicitly or fix OS timezone tooling", th.Host, err)
		}
		if ref == "" {
			ref = tz
			continue
		}
		if tz != ref {
			return fmt.Errorf("hosts have inconsistent OS timezones (%s vs %s on %s); unify OS timezone or set --db-timezone explicitly", ref, tz, th.Host)
		}
	}
	return nil
}

func stripTimeZoneLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if reTimeZoneLine.MatchString(ln) {
			continue
		}
		out = append(out, ln)
	}
	return out
}

func insertTimeZoneUnderEachGroupConfig(lines []string, tz string) []string {
	line := fmt.Sprintf(`TIME_ZONE = "%s"`, tz)
	out := make([]string, 0, len(lines)+8)
	for i := 0; i < len(lines); {
		ln := lines[i]
		m := reGroupConfigHeader.FindStringSubmatch(ln)
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
				out = append(out, keyPrefix+line)
				out = append(out, nl)
				j++
				inserted = true
				break
			}
			pref := leadingWhitespacePrefix(nl)
			out = append(out, pref+line)
			out = append(out, nl)
			j++
			inserted = true
			break
		}
		if !inserted {
			out = append(out, keyPrefix+line)
		}
		i = j
	}
	return out
}

func countGroupConfigHeaders(lines []string) int {
	n := 0
	for _, ln := range lines {
		if reGroupConfigHeader.MatchString(ln) {
			n++
		}
	}
	return n
}

func countTimeZoneLines(lines []string) int {
	n := 0
	for _, ln := range lines {
		if reTimeZoneLine.MatchString(ln) {
			n++
		}
	}
	return n
}

func verifyTimeZoneInClusterTOML(lines []string, tz string) error {
	sections := countGroupConfigHeaders(lines)
	if sections == 0 {
		return verifyLegacyDBTimeZone(lines, tz)
	}
	want := fmt.Sprintf(`TIME_ZONE = "%s"`, tz)
	found := 0
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == want {
			found++
		}
	}
	if found != sections {
		return fmt.Errorf("[group.config] sections=%d TIME_ZONE=%q lines=%d (must match every group.config)", sections, tz, found)
	}
	return nil
}

func verifyLegacyDBTimeZone(lines []string, tz string) error {
	wantSub := fmt.Sprintf(`TIME_ZONE = "%s"`, tz)
	for _, ln := range lines {
		if strings.TrimSpace(ln) == wantSub {
			return nil
		}
	}
	return fmt.Errorf("TIME_ZONE = %q not found in cluster config", tz)
}

func containsGroupConfigSection(lines []string) bool {
	return countGroupConfigHeaders(lines) > 0
}

func ensureTimeZoneInClusterTOML(ctx *runner.StepContext, configPath, tz string) error {
	q := strconv.Quote(configPath)
	content, err := readRemoteTextFile(ctx, configPath)
	if err != nil {
		return err
	}
	lines := normalizeTomlLines(content)

	if containsGroupConfigSection(lines) {
		stripped := stripTimeZoneLines(lines)
		updated := insertTimeZoneUnderEachGroupConfig(stripped, tz)
		if err := verifyTimeZoneInClusterTOML(updated, tz); err != nil {
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
		return verifyTimeZoneInClusterTOML(normalizeTomlLines(after), tz)
	}

	if !containsLegacyDBSection(lines) {
		return fmt.Errorf("cluster config %s has no [group.config] and no [db] section; cannot set TIME_ZONE", configPath)
	}

	result, err := ctx.Execute(fmt.Sprintf(`grep -q '^[[:space:]]*TIME_ZONE[[:space:]]*=' %s`, q), false)
	if err != nil {
		return err
	}
	if result != nil && result.GetExitCode() == 0 {
		cmd := fmt.Sprintf(`sed -i 's/^[[:space:]]*TIME_ZONE[[:space:]]*=.*/TIME_ZONE = "%s"/' %s`, tz, q)
		if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
			return fmt.Errorf("failed to update TIME_ZONE: %w", err)
		}
	} else {
		cmd := fmt.Sprintf(`sed -i '/^\[db\]/a TIME_ZONE = "%s"' %s`, tz, q)
		if _, err := ctx.ExecuteWithCheck(cmd, false); err != nil {
			return fmt.Errorf("failed to append TIME_ZONE under [db]: %w", err)
		}
	}
	after, err := readRemoteTextFile(ctx, configPath)
	if err != nil {
		return err
	}
	return verifyLegacyDBTimeZone(normalizeTomlLines(after), tz)
}
