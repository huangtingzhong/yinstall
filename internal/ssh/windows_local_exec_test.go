package ssh

import "testing"

func TestParseQuotedWindowsExeCommand(t *testing.T) {
	t.Parallel()
	cmd := `"D:/mysql/app/mysql/product/8.0.44/dbhome_1/bin/mysqld.exe" --defaults-file="D:/mysql/app/mysql/oradata/3306/other/my.ini" --initialize-insecure --console`
	exe, args, ok := parseQuotedWindowsExeCommand(cmd)
	if !ok {
		t.Fatal("expected ok")
	}
	if exe != `D:/mysql/app/mysql/product/8.0.44/dbhome_1/bin/mysqld.exe` {
		t.Fatalf("exe=%q", exe)
	}
	if len(args) != 3 {
		t.Fatalf("args=%v", args)
	}
	if args[0] != `--defaults-file=D:/mysql/app/mysql/oradata/3306/other/my.ini` {
		t.Fatalf("arg0=%q", args[0])
	}
}

func TestSplitWindowsCommandArgs(t *testing.T) {
	t.Parallel()
	got := splitWindowsCommandArgs(`--defaults-file="D:/a/my.ini" --console`)
	if len(got) != 2 || got[0] != `--defaults-file=D:/a/my.ini` || got[1] != `--console` {
		t.Fatalf("got=%v", got)
	}
}
