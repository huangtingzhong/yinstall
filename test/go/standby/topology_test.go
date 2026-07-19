package standby_test

import (
	"testing"

	"github.com/yinstall/internal/steps/standby"
)

func TestParseOmAddrHost(t *testing.T) {
	t.Parallel()
	h, err := standby.ParseOmAddrHost(`10.10.10.130:1675`)
	if err != nil || h != "10.10.10.130" {
		t.Fatalf("got %q %v", h, err)
	}
	h, err = standby.ParseOmAddrHost(`"10.10.10.130:1675"`)
	if err != nil || h != "10.10.10.130" {
		t.Fatalf("quoted: %q %v", h, err)
	}
}

func TestOmHostFromEnvFileContent(t *testing.T) {
	t.Parallel()
	content := `
cluster="yashandb"
om_addr="10.10.10.130:1675"
version="23.5.2.101"
`
	h, err := standby.OmHostFromEnvFileContent(content)
	if err != nil || h != "10.10.10.130" {
		t.Fatalf("got %q %v", h, err)
	}
	content2 := `
om_addr = "10.10.10.135:2675"
om_addr = "10.10.10.130:1675"
`
	h, err = standby.OmHostFromEnvFileContent(content2)
	if err != nil || h != "10.10.10.130" {
		t.Fatalf("last wins: %q %v", h, err)
	}
}

func TestPrimaryIPFromClusterStatus(t *testing.T) {
	t.Parallel()
	table := `
| hostid   | node_type | nodeid | pid | instance_status | database_status | database_role | listen_address    | source_node | data_path |
| host0001 | db        | 1-1:1  | 1   | open            | normal          | standby       | 10.10.10.130:1688 | 1-2         | /data/a   |
| host0002 | db        | 1-2:2  | 2   | open            | normal          | primary       | 10.10.10.131:1688 | -           | /data/b   |
`
	ip := standby.PrimaryIPFromClusterStatus(table)
	if ip != "10.10.10.131" {
		t.Fatalf("got %q", ip)
	}
}

func TestSameHostIP(t *testing.T) {
	t.Parallel()
	if !standby.SameHostIP("10.10.10.130", "10.10.10.130") {
		t.Fatal("expected same")
	}
	if standby.SameHostIP("10.10.10.130", "10.10.10.131") {
		t.Fatal("expected different")
	}
}
