package runner

import "testing"

func TestIsUpstreamArtifactMissingMessage(t *testing.T) {
	cases := []struct {
		msg    string
		expect bool
	}{
		{"yasboot not found at /x", true},
		{"cluster config not found at /a/yashandb.toml", true},
		{"hosts_add.toml not found, run E-011 first", true},
		{"deploy config not found: /opt/ycm/etc/deploy.yml (run G-004 first)", true},
		{"ycm-init not found in any of: [/a]", true},
		{"sysctl config file not found", true},
		{"db_admin_password is required", false},
		{"user yashan does not exist", false},
		{"systemctl not found on host", false},
	}
	for _, c := range cases {
		got := isUpstreamArtifactMissingMessage(c.msg)
		if got != c.expect {
			t.Errorf("msg=%q: got %v want %v", c.msg, got, c.expect)
		}
	}
}
