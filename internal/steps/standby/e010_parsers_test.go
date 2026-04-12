package standby

import (
	"testing"
)

func TestParseYasagentStatusIPToHostID(t *testing.T) {
	out := `+----------+-------+----------+------------------+
| hostid   | pid   | run_user | listen_address   |
+----------+-------+----------+------------------+
| host0001 | 12345 | yashan   | 10.10.10.130:1676 |
| host0002 | 23456 | yashan   | 10.10.10.135:1676 |
+----------+-------+----------+------------------+`
	m := parseYasagentStatusIPToHostID(out)
	if m["10.10.10.130"] != "host0001" || m["10.10.10.135"] != "host0002" {
		t.Fatalf("unexpected map: %#v", m)
	}
}

func TestParseClusterStatusIPToHostID(t *testing.T) {
	out := `+----------+-----------+--------+---------+-----------------+-----------------+---------------+-------------------+
| hostid   | node_type | nodeid | pid     | instance_status | database_status | database_role | listen_address    |
+----------+-----------+--------+---------+-----------------+-----------------+---------------+-------------------+
| host0001 | db        | 1-1    | 100     | open            | open            | primary       | 10.10.10.130:1688 |
| host0002 | db        | 1-2    | 0       | closed          | closed          | standby       | 10.10.10.135:1688 |
+----------+-----------+--------+---------+-----------------+-----------------+---------------+-------------------+`
	m := parseClusterStatusIPToHostID(out)
	if m["10.10.10.130"] != "host0001" || m["10.10.10.135"] != "host0002" {
		t.Fatalf("unexpected map: %#v", m)
	}
}

func TestParseClusterStatusListenAddrAlias(t *testing.T) {
	out := `| hostid   | listen_addr       |
| host0009 | 192.168.1.9:1676 |`
	m := parseClusterStatusIPToHostID(out)
	if m["192.168.1.9"] != "host0009" {
		t.Fatalf("unexpected map: %#v", m)
	}
}
