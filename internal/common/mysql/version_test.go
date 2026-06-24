package mysql

import "testing"

func TestReplicaVersionMatchesPrimary(t *testing.T) {
	ok, err := ReplicaVersionMatchesPrimary("8.0.44", "8.0.44")
	if err != nil || !ok {
		t.Fatalf("8.0.44 == 8.0.44: ok=%v err=%v", ok, err)
	}
	ok, err = ReplicaVersionMatchesPrimary("8.0.46", "8.0.44")
	if err != nil || ok {
		t.Fatalf("8.0.46 != 8.0.44: ok=%v err=%v", ok, err)
	}
	ok, err = ReplicaVersionOK("8.0.46", "8.0.44")
	if err != nil || !ok {
		t.Fatalf("ReplicaVersionOK still allows >=")
	}
}
