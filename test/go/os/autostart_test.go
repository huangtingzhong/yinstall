package os_test

import (
	"testing"

	commonos "github.com/yinstall/internal/common/os"
)

func TestDetermineServiceName_nonDefaultPortAlwaysPortUnit(t *testing.T) {
	t.Parallel()
	name, arg := commonos.DetermineServiceName(1, 3688)
	if name != "yashan_monit_3688" || arg != "3688" {
		t.Fatalf("got service=%s arg=%s; want yashan_monit_3688 / 3688", name, arg)
	}
	name, arg = commonos.DetermineServiceName(5, 3788)
	if name != "yashan_monit_3788" || arg != "3788" {
		t.Fatalf("got service=%s arg=%s; want yashan_monit_3788 / 3788", name, arg)
	}
}

func TestDetermineServiceName_defaultPortUsesBashrcWhenSingle(t *testing.T) {
	t.Parallel()
	name, arg := commonos.DetermineServiceName(1, 1688)
	if name != "yashan_monit" || arg != "bashrc" {
		t.Fatalf("got service=%s arg=%s; want yashan_monit / bashrc", name, arg)
	}
	name, arg = commonos.DetermineServiceName(2, 1688)
	if name != "yashan_monit_1688" || arg != "1688" {
		t.Fatalf("got service=%s arg=%s; want yashan_monit_1688 / 1688", name, arg)
	}
}
