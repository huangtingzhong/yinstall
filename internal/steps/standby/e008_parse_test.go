package standby

import "testing"

func TestExtractNodeIDFromArchiveValue(t *testing.T) {
	id, ok := extractNodeIDFromArchiveValue("SERVICE=10.10.10.135:1689 NODE_ID=1-2:2")
	if !ok || id != "1-2:2" {
		t.Fatalf("got %q ok=%v", id, ok)
	}
	_, ok = extractNodeIDFromArchiveValue("SERVICE=10.10.10.135:1689")
	if ok {
		t.Fatal("expected no NODE_ID")
	}
}

func TestParseClusterStatusTable_userSample(t *testing.T) {
	out := `+----------------------------------------------------------------------------------------------------------------------------------------------------------------+
| hostid   | node_type | nodeid | pid     | instance_status | database_status | database_role | listen_address    | source_node | data_path                      |
+----------------------------------------------------------------------------------------------------------------------------------------------------------------+
| host0001 | db        | 1-1:1  | 1574082 | open            | normal          | primary       | 10.10.10.130:1688 | -           | /data/yashan/yasdb_data/db-1-1 |
+----------+-----------+--------+---------+-----------------+-----------------+---------------+-------------------+             +--------------------------------+
| host0002 | db        | 1-2:2  | -       | -               | -               | -             | -                 |             | /data/yashan/yasdb_data/db-1-2 |
+----------+-----------+--------+---------+-----------------+-----------------+---------------+-------------------+-------------+--------------------------------+`
	by := clusterStatusByNodeid(out)
	r, ok := by["1-2:2"]
	if !ok {
		t.Fatalf("missing node 1-2:2, keys=%v", keysOfCluster(by))
	}
	if r.Hostid != "host0002" || instanceStatusIsOpen(r.InstanceStatus) {
		t.Fatalf("unexpected row: %+v", r)
	}
	r1, ok := by["1-1:1"]
	if !ok || !instanceStatusIsOpen(r1.InstanceStatus) {
		t.Fatalf("primary row: %+v ok=%v", r1, ok)
	}
}

func keysOfCluster(m map[string]clusterStatusRow) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestParseYasqlNameValueLine_fixedWidthArchiveDest(t *testing.T) {
	line := "ARCHIVE_DEST_1                                                   SERVICE=10.10.10.135:1689 NODE_ID=1-2:2"
	n, v, ok := parseYasqlNameValueLine(line)
	if !ok || n != "ARCHIVE_DEST_1" || v != "SERVICE=10.10.10.135:1689 NODE_ID=1-2:2" {
		t.Fatalf("got n=%q v=%q ok=%v", n, v, ok)
	}
}

func TestParseYasqlNameValueLine_pipeFormat(t *testing.T) {
	line := "ARCHIVE_DEST_1 | SERVICE=10.10.10.135:1689"
	n, v, ok := parseYasqlNameValueLine(line)
	if !ok || n != "ARCHIVE_DEST_1" || v != "SERVICE=10.10.10.135:1689" {
		t.Fatalf("got n=%q v=%q ok=%v", n, v, ok)
	}
}

func TestIsYasqlNameValueHeader(t *testing.T) {
	if !isYasqlNameValueHeader("NAME                                                             VALUE") {
		t.Fatal("expected header")
	}
	if isYasqlNameValueHeader("ARCHIVE_DEST_1  x") {
		t.Fatal("unexpected header")
	}
}
