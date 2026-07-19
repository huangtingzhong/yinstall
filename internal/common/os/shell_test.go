package os

import (
	"fmt"
	"strings"
	"testing"
)

// 模拟 C-014：genCmd 内 -p 密码，再经 sudo bash -lc 外层 ShellSingleQuote。
func wrapAsBashLc(inner string) string {
	return fmt.Sprintf("sudo -n -u yashan bash -lc %s", ShellSingleQuote("cd ~ && "+inner))
}

func TestYasbootPasswordQuotingForBashLc(t *testing.T) {
	password := "aaBB11@@33$$"
	inner := fmt.Sprintf("cd /tmp && yasboot package ce gen -p %s --ip 1.2.3.4", ShellSingleQuote(password))
	outer := wrapAsBashLc(inner)

	if strings.Contains(outer, `'\''\'\''`) {
		t.Fatalf("password must not be double-escaped for bash -lc: %q", outer)
	}
}

func TestShellEscapeForSuCDoubleWrapBreaks(t *testing.T) {
	password := "aaBB11@@33$$"
	inner := fmt.Sprintf("yasboot -p %s", ShellEscapeForSuC(password))
	outer := wrapAsBashLc(inner)
	if !strings.Contains(outer, `'\''\'\''`) {
		t.Fatal("expected double-escape artifact when misusing ShellEscapeForSuC under bash -lc")
	}
}
