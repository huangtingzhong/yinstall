// e008_check_archive_dest.go - 检查归档路径是否已包含目标端
// 本步骤检查主库的归档路径配置，如果已包含目标端IP，说明备库已配置，直接报错退出

package standby

import (
	"fmt"
	"regexp"
	"strings"

	commonsql "github.com/yinstall/internal/common/sql"
	"github.com/yinstall/internal/runner"
)

// archiveDestHit 归档参数命中某 standby IP 的一条记录（用于与 yasboot cluster status 交叉校验）。
type archiveDestHit struct {
	target    string
	paramName string
	value     string
}

// clusterStatusRow yasboot cluster status -d 表格中的一行（按表头列名解析）。
type clusterStatusRow struct {
	Hostid           string
	Nodeid           string
	InstanceStatus   string
	ListenAddress    string
	DatabaseStatus   string
	DatabaseRole     string
}

var archiveNodeIDRE = regexp.MustCompile(`(?i)NODE_ID\s*=\s*(\S+)`)

func extractNodeIDFromArchiveValue(value string) (string, bool) {
	m := archiveNodeIDRE.FindStringSubmatch(value)
	if len(m) < 2 {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

func instanceStatusIsOpen(s string) bool {
	return strings.EqualFold(strings.TrimSpace(s), "open")
}

// parseClusterStatusTable 解析 yasboot cluster status -c <cluster> -d 的表格输出（管道对齐格式）。
func parseClusterStatusTable(output string) []clusterStatusRow {
	var rows []clusterStatusRow
	hostCol, nodeCol, instCol, listenCol, dbStatCol, roleCol := -1, -1, -1, -1, -1, -1

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "+") {
			continue
		}
		parts := strings.Split(line, "|")
		fields := make([]string, 0, len(parts))
		for _, p := range parts {
			fields = append(fields, strings.TrimSpace(p))
		}
		if hostCol < 0 || nodeCol < 0 || instCol < 0 || listenCol < 0 {
			for i, f := range fields {
				switch f {
				case "hostid":
					hostCol = i
				case "nodeid":
					nodeCol = i
				case "instance_status":
					instCol = i
				case "listen_address", "listen_addr":
					listenCol = i
				case "database_status":
					dbStatCol = i
				case "database_role":
					roleCol = i
				}
			}
			if hostCol >= 0 && nodeCol >= 0 && instCol >= 0 && listenCol >= 0 {
				continue
			}
		}
		if hostCol < 0 || nodeCol < 0 || instCol < 0 || listenCol < 0 {
			continue
		}
		if len(fields) <= nodeCol || len(fields) <= instCol {
			continue
		}
		hostid := fields[hostCol]
		nodeid := fields[nodeCol]
		if hostid == "" || strings.EqualFold(hostid, "hostid") {
			continue
		}
		if nodeid == "" || strings.EqualFold(nodeid, "nodeid") {
			continue
		}
		r := clusterStatusRow{
			Hostid:         hostid,
			Nodeid:         nodeid,
			InstanceStatus: fields[instCol],
			ListenAddress:  fields[listenCol],
		}
		if dbStatCol >= 0 && len(fields) > dbStatCol {
			r.DatabaseStatus = fields[dbStatCol]
		}
		if roleCol >= 0 && len(fields) > roleCol {
			r.DatabaseRole = fields[roleCol]
		}
		rows = append(rows, r)
	}
	return rows
}

func clusterStatusByNodeid(output string) map[string]clusterStatusRow {
	by := make(map[string]clusterStatusRow)
	for _, r := range parseClusterStatusTable(output) {
		key := strings.TrimSpace(r.Nodeid)
		if key != "" {
			by[key] = r
		}
	}
	return by
}

// parseYasqlNameValueLine 解析 yasql 两列输出的一行。
// yasql 常见为固定宽度、空格分列（无 |）；部分环境可能为 "NAME | VALUE"。
func parseYasqlNameValueLine(line string) (name, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	if strings.Contains(line, "|") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			return "", "", false
		}
		name = strings.TrimSpace(parts[0])
		value = strings.TrimSpace(parts[1])
		return name, value, name != "" && value != ""
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], strings.Join(fields[1:], " "), true
}

func isYasqlNameValueHeader(line string) bool {
	f := strings.Fields(strings.TrimSpace(line))
	if len(f) < 2 {
		return false
	}
	return strings.EqualFold(f[0], "NAME") && strings.EqualFold(f[1], "VALUE")
}

func isYasqlTableSeparator(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" || len(t) < 3 {
		return false
	}
	for _, r := range t {
		if r != '-' && r != '=' && r != '+' && r != ' ' && r != '_' {
			return false
		}
	}
	return true
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... (truncated)"
}

func skipYasqlNoiseLine(line string) bool {
	low := strings.ToLower(strings.TrimSpace(line))
	if low == "" {
		return true
	}
	if strings.HasPrefix(low, "disconnected from:") || strings.HasPrefix(low, "disconnected from") {
		return true
	}
	if strings.Contains(low, "row fetched") {
		return true
	}
	if strings.Contains(line, "YashanDB Server") {
		return true
	}
	return false
}

// StepE008CheckArchiveDest 检查归档路径是否已包含目标端步骤
func StepE008CheckArchiveDest() *runner.Step {
	return &runner.Step{
		ID:          "E-008",
		Name:        "Check Archive Destination",
		Description: "Check archive dest for standby IPs; if hit, cross-check yasboot cluster status nodeid/instance_status (only block when open)",
		Tags:        []string{"standby", "archive", "check"},

		PreCheck: func(ctx *runner.StepContext) error {
			targets := ctx.GetParamStringSlice("standby_targets")
			if len(targets) == 0 {
				return fmt.Errorf("standby_targets is required")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			primaryUser := GetPrimaryOSUser(ctx)
			targets := ctx.GetParamStringSlice("standby_targets")

			ctx.Logger.Info("Checking if archive destination already contains standby targets")
			ctx.Logger.Info("  Standby targets: %v", targets)
			ctx.Logger.Info("  Primary user: %s", primaryUser)

			// Get primary environment file path
			envFile, err := GetPrimaryEnvFile(ctx)
			if err != nil {
				return fmt.Errorf("failed to get primary environment file: %w", err)
			}
			ctx.Logger.Info("Using primary environment file: %s", envFile)
			if err := SyncPrimaryClusterNameFromEnvFile(ctx, envFile); err != nil {
				return err
			}
			clusterName := ctx.GetParamString("db_cluster_name", "yashandb")
			ctx.Logger.Info("  Cluster: %s", clusterName)

			// 查询归档是否引用各 target IP；命中后再用 yasboot cluster status 按 NODE_ID 校验 instance_status 是否为 open
			var archiveHits []archiveDestHit

			for _, target := range targets {
				target = strings.TrimSpace(target)
				if target == "" {
					continue
				}

				ctx.Logger.Info("Checking if archive destination contains target IP: %s", target)
				sql := fmt.Sprintf("SELECT name, value FROM v$parameter WHERE name LIKE 'ARCHIVE_DEST%%' AND value LIKE '%%%s%%';", target)

				result, err := commonsql.ExecuteSQLAsSysdbaCtx(ctx, primaryUser, envFile, clusterName, sql, true)
				if err != nil {
					ctx.Logger.Warn("Failed to query archive destination for target %s: %v", target, err)
					continue
				}

				if result == nil || result.Stdout == "" {
					ctx.Logger.Info("No archive destination found containing target IP: %s", target)
					continue
				}

				ctx.Logger.Info("Archive destination query result for target %s:", target)
				ctx.Logger.Info("%s", result.Stdout)

				lines := strings.Split(result.Stdout, "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if skipYasqlNoiseLine(line) {
						continue
					}
					if isYasqlNameValueHeader(line) || isYasqlTableSeparator(line) {
						continue
					}
					paramName, value, ok := parseYasqlNameValueLine(line)
					if !ok {
						continue
					}
					upper := strings.ToUpper(paramName)
					if !strings.HasPrefix(upper, "ARCHIVE_DEST") {
						continue
					}
					if value != "" && !strings.EqualFold(value, "null") && !strings.EqualFold(value, "none") &&
						strings.Contains(value, target) {
						archiveHits = append(archiveHits, archiveDestHit{target: target, paramName: paramName, value: value})
						ctx.Logger.Info("Found target IP %s in archive destination: %s = %s", target, paramName, value)
						break
					}
				}
			}

			if len(archiveHits) == 0 {
				ctx.Logger.Info("✓ No standby targets found in archive destination configuration")
				return nil
			}

			// 归档已引用 IP：拉取集群状态，用归档中的 NODE_ID 与 nodeid 列对应，仅 instance_status=open 时视为已正常配置备库并阻塞
			clusterCmd := fmt.Sprintf("yasboot cluster status -c %s -d", clusterName)
			ctx.Logger.Info("Cross-checking with primary: source env && %s", clusterCmd)
			csRes, csErr := runYasbootOnPrimaryWithEnvFileNoCheck(ctx, primaryUser, envFile, clusterCmd)
			csStdout := ""
			if csRes != nil {
				csStdout = csRes.GetStdout()
			}
			if csRes == nil || csRes.GetExitCode() != 0 {
				exit := -1
				if csRes != nil {
					exit = csRes.GetExitCode()
				}
				detail := ""
				if csRes != nil {
					detail = YasbootCombinedOutput(csRes.GetStdout(), csRes.GetStderr())
				}
				ctx.Logger.Error("yasboot cluster status cross-check failed (exit=%d, err=%v); archive already references target IP — aborting conservatively", exit, csErr)
				return fmt.Errorf("archive destination references standby target(s) but cluster status check failed (exit=%d): %v\n%s", exit, csErr, strings.TrimSpace(detail))
			}
			if strings.TrimSpace(csStdout) == "" {
				return fmt.Errorf("archive destination references standby target(s) but cluster status output is empty")
			}
			ctx.Logger.Info("Cluster status output (excerpt for cross-check):\n%s", truncateForLog(csStdout, 8000))
			byNode := clusterStatusByNodeid(csStdout)

			var foundTargets []string
			var allArchiveDests []string
			seenBlock := make(map[string]struct{})

			for _, hit := range archiveHits {
				nodeID, ok := extractNodeIDFromArchiveValue(hit.value)
				if !ok {
					ctx.Logger.Error("archive parameter value has no NODE_ID=...; cannot align with cluster status for target %s: %s = %s",
						hit.target, hit.paramName, hit.value)
					if _, dup := seenBlock[hit.target]; !dup {
						foundTargets = append(foundTargets, hit.target)
						seenBlock[hit.target] = struct{}{}
					}
					allArchiveDests = append(allArchiveDests, fmt.Sprintf("%s = %s", hit.paramName, hit.value))
					continue
				}
				row, hasRow := byNode[nodeID]
				if !hasRow {
					ctx.Logger.Warn("archive NODE_ID=%s has no matching nodeid row in yasboot cluster status (possible stale metadata); target IP=%s — not blocking expansion; clean up archive if needed",
						nodeID, hit.target)
					continue
				}
				if !instanceStatusIsOpen(row.InstanceStatus) {
					ctx.Logger.Warn("archive references target %s (NODE_ID=%s) but cluster instance_status=%q (not open); treating as abnormal/not ready — not blocking expansion; hostid=%s listen=%s db_status=%q role=%q",
						hit.target, nodeID, row.InstanceStatus, row.Hostid, row.ListenAddress, row.DatabaseStatus, row.DatabaseRole)
					continue
				}
				ctx.Logger.Error("archive and cluster status agree: NODE_ID=%s has instance_status=open; target %s is already a healthy instance", nodeID, hit.target)
				if _, dup := seenBlock[hit.target]; !dup {
					foundTargets = append(foundTargets, hit.target)
					seenBlock[hit.target] = struct{}{}
				}
				allArchiveDests = append(allArchiveDests, fmt.Sprintf("%s = %s", hit.paramName, hit.value))
			}

			if len(foundTargets) > 0 {
				ctx.Logger.Error("╔════════════════════════════════════════════════════════════════╗")
				ctx.Logger.Error("║  ERROR: Standby targets already configured in archive destination! ║")
				ctx.Logger.Error("║  (cluster status: matching nodeid has instance_status=open)   ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  The following standby target(s) are already configured:      ║")
				for _, target := range foundTargets {
					ctx.Logger.Error("║    - %s", target)
				}
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  Archive destination configuration:                            ║")
				for _, dest := range allArchiveDests {
					ctx.Logger.Error("║    %s", dest)
				}
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  Action required:                                                ║")
				ctx.Logger.Error("║  1. If you want to reconfigure standby, first remove the        ║")
				ctx.Logger.Error("║     existing archive destination configuration.                 ║")
				ctx.Logger.Error("║                                                                ║")
				ctx.Logger.Error("║  2. Or use a different standby target IP address.              ║")
				ctx.Logger.Error("╚════════════════════════════════════════════════════════════════╝")
				return fmt.Errorf("standby targets %v already configured in archive destination (cluster instance open)", foundTargets)
			}

			ctx.Logger.Info("✓ archive referenced target(s) but cluster status shows instance not open or missing nodeid row — not blocking as already-configured standby")
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			return nil
		},
	}
}
