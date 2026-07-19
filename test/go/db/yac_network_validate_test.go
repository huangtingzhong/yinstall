package db_test

import (
	"testing"

	"github.com/yinstall/internal/steps/db"
)

func TestValidateYACNetworksOnHostsRequiresInter(t *testing.T) {
	err := db.ValidateYACNetworksOnHosts(nil, "", "", nil)
	if err == nil {
		t.Fatal("want error for empty hosts")
	}
	err = db.ValidateYACNetworksOnHosts([]db.HostExec{{Host: "10.0.0.1"}}, "", "10.0.0.0/24", nil)
	if err == nil {
		t.Fatal("want error for empty inter-cidr")
	}
}

func TestValidateHostCIDRInvalidFormat(t *testing.T) {
	_, err := db.ValidateHostCIDR([]db.HostExec{{Host: "10.0.0.1"}}, "not-a-cidr", "inter-cidr", nil)
	if err == nil {
		t.Fatal("want invalid cidr error")
	}
}
