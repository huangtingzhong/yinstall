package standby

import (
	"strings"
)

// YasbootCombinedOutput joins stdout and stderr for log / error messages.
func YasbootCombinedOutput(stdout, stderr string) string {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)
	switch {
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	default:
		return stdout + "\n" + stderr
	}
}

// ExplainYasbootHostAddFailure returns a short hint when yasboot host add fails.
func ExplainYasbootHostAddFailure(combined string) string {
	low := strings.ToLower(combined)
	switch {
	case strings.Contains(low, "should be empty"):
		return "Hint: yasboot requires the standby software home (including the versioned subdir, e.g. .../yasdb_home/23.x.x.x) to be empty. If you ran yinstall clean, ensure --yasdb-home/--yasdb-data match the paths used for standby install, and that clean did not fail on permissions (see clean logs)."
	case strings.Contains(low, "yashandb.env") && strings.Contains(low, "already exist"):
		return "Hint: stale ~/.yasboot/<cluster>.env (or similar) on the standby conflicts with this cluster. Remove it and retry, or follow yasboot --force guidance (this step may already pass --force; if it still fails, manually clean ~/.yasboot on the standby)."
	case strings.Contains(low, "permission denied"):
		return "Hint: permission denied on a path on the standby. Run clean/install as the correct OS user or fix ownership on the target directories."
	default:
		return "Hint: see the full yasboot host add stdout/stderr above. Other common causes: SSH to standby failed, disk full, or paths not matching hosts_add.toml."
	}
}

// ExplainYasbootNodeAddFailure returns a short hint when yasboot node add fails.
func ExplainYasbootNodeAddFailure(combined string) string {
	low := strings.ToLower(combined)
	switch {
	case strings.Contains(low, "connection is shut down"):
		return "Hint: RPC/agent connection was closed (often yasboot talking to yasagent on a cluster host). Check yasagent on primary and standby, port reachability (e.g. agent port), firewall, and that the previous host add step left agents healthy; retry after agents are stable."
	case strings.Contains(low, "get host") && strings.Contains(low, "host info failed"):
		return "Hint: primary cannot load host metadata for that hostid—usually host add did not fully succeed or the standby was cleaned while still registered. Fix any E-012 failure and re-run; after clean, ensure versioned dirs under --db-home-path are removed and ~/.yasboot on the standby is cleared if needed."
	case strings.Contains(low, "scale failed"):
		return "Hint: a scale-failed node is present; try yasboot node remove --clean as prompted, then retry."
	default:
		return "Hint: see the full yasboot node add output above."
	}
}
