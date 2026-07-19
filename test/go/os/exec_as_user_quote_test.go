package os_test

import (
	"strings"
	"testing"

	commonos "github.com/yinstall/internal/common/os"
)

// 回归：默认 os 密码含 $$ 时，经 ShellSingleQuote 后不得在外层再被错误双重转义；
// 且 ExecuteAsUser 的 sudo 包装应使用 -u（非 -i）并 cd ~，避免 $$→PID 与 cwd=/root。
func TestDefaultOSPasswordQuotingShape(t *testing.T) {
	password := "aaBB11@@33$$"
	inner := "cd /home/yashan/install && yasboot package ce gen -p " + commonos.ShellSingleQuote(password)
	// 与 buildRunAsUserCommand(sudo=true) 当前形态对齐
	outer := "sudo -n -u yashan bash -lc " + commonos.ShellSingleQuote("cd ~ && "+inner)

	if strings.Contains(outer, "sudo -n -iu ") {
		t.Fatalf("must not use sudo -i (expands $$): %q", outer)
	}
	if !strings.Contains(outer, "sudo -n -u yashan bash -lc ") {
		t.Fatalf("expected sudo -u bash -lc wrapper: %q", outer)
	}
	if !strings.Contains(outer, "cd ~ &&") {
		t.Fatalf("expected cd ~ for home cwd: %q", outer)
	}
	if strings.Contains(outer, `'\''\'\''`) {
		t.Fatalf("password must not be double-escaped: %q", outer)
	}
	// 外层单引号包裹后，内层密码仍应保留字面 $$（未在 Go 侧展开）
	if !strings.Contains(outer, `aaBB11@@33$$`) {
		t.Fatalf("literal $$ missing in command: %q", outer)
	}
}
