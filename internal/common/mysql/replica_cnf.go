package mysql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ConfigFileName returns my.cnf or my.ini for platform.
func ConfigFileName(platform string) string {
	if platform == PlatformWindows {
		return "my.ini"
	}
	return "my.cnf"
}

// PatchPlan drives incremental cnf edits for a replica.
type PatchPlan struct {
	ServerID        int
	Port            *int
	PrimaryPort     int
	PathOverrides   map[string]string
	PlatformFixups  []string // e.g. "drop_socket"
	ExplicitParams  map[string]string
	PrimaryPlatform string
	ReplicaPlatform string
}

// PatchReplicaCnf applies sparse patches; unlisted keys stay as in primary content.
func PatchReplicaCnf(primaryContent string, plan PatchPlan) (string, error) {
	out := primaryContent
	if plan.ServerID > 0 {
		out = setOrAppendCnfKey(out, "server_id", strconv.Itoa(plan.ServerID))
	}
	if plan.Port != nil {
		out = setOrReplaceAllCnfKeys(out, "port", strconv.Itoa(*plan.Port))
		if plan.PrimaryPort > 0 && *plan.Port != plan.PrimaryPort {
			out = replacePortInPaths(out, plan.PrimaryPort, *plan.Port)
			out = setOrReplaceAllCnfKeys(out, "mysqlx_port", strconv.Itoa(*plan.Port*10))
		}
	}
	for k, v := range plan.PathOverrides {
		out = setOrAppendCnfKey(out, k, v)
	}
	for k, v := range plan.ExplicitParams {
		out = setOrAppendCnfKey(out, k, v)
	}
	for _, fix := range plan.PlatformFixups {
		if fix == "drop_socket" {
			out = dropCnfKey(out, "socket")
		}
	}
	return out, nil
}

var cnfKeyLine = regexp.MustCompile(`(?m)^(\s*)([a-zA-Z0-9_-]+)\s*=`)

func cnfKeyAliases(key string) []string {
	switch strings.ToLower(key) {
	case "server_id":
		return []string{"server_id", "server-id"}
	default:
		return []string{key}
	}
}

func cnfKeyMatches(key string, aliases []string) bool {
	for _, alias := range aliases {
		if strings.EqualFold(key, alias) {
			return true
		}
	}
	return false
}

func setOrAppendCnfKey(content, key, value string) string {
	lines := strings.Split(content, "\n")
	aliases := cnfKeyAliases(key)
	found := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, ";") {
			continue
		}
		if eq := strings.IndexByte(trim, '='); eq > 0 {
			k := strings.TrimSpace(trim[:eq])
			if cnfKeyMatches(k, aliases) {
				lines[i] = fmt.Sprintf("%s = %s", k, value)
				found = true
				break
			}
		}
	}
	if !found {
		if !strings.HasSuffix(content, "\n") && content != "" {
			content += "\n"
		}
		content += fmt.Sprintf("%s = %s\n", key, value)
		return content
	}
	return strings.Join(lines, "\n")
}

func setOrReplaceAllCnfKeys(content, key, value string) string {
	lines := strings.Split(content, "\n")
	aliases := cnfKeyAliases(key)
	found := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, ";") {
			continue
		}
		if eq := strings.IndexByte(trim, '='); eq > 0 {
			k := strings.TrimSpace(trim[:eq])
			if cnfKeyMatches(k, aliases) {
				lines[i] = fmt.Sprintf("%s = %s", k, value)
				found = true
			}
		}
	}
	if !found {
		return setOrAppendCnfKey(content, key, value)
	}
	return strings.Join(lines, "\n")
}

func dropCnfKey(content, key string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	keyLower := strings.ToLower(key)
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if eq := strings.IndexByte(trim, '='); eq > 0 {
			k := strings.TrimSpace(trim[:eq])
			if strings.EqualFold(k, keyLower) {
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func replacePortInPaths(content string, fromPort, toPort int) string {
	from := strconv.Itoa(fromPort)
	to := strconv.Itoa(toPort)
	content = strings.ReplaceAll(content, "/"+from+"/", "/"+to+"/")
	content = strings.ReplaceAll(content, `\`+from+`\`, `\`+to+`\`)
	content = strings.ReplaceAll(content, "/oradata/"+from, "/oradata/"+to)
	return content
}

// LayoutFromParams builds layout for primary or replica.
func LayoutFromParams(platform string, base string, port int, version string) Layout {
	params := map[string]interface{}{
		"target_platform": platform,
		"mysql_base":      base,
		"mysql_port":      port,
		"mysql_version":   version,
	}
	l, _ := ResolveLayout(params)
	return l
}
