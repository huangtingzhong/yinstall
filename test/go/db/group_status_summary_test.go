package db_test

import (
	"strings"
	"testing"

	dbsteps "github.com/yinstall/internal/steps/db"
)

func TestSummarizeDBGroupStatusPreservesEmptyGroupNameColumn(t *testing.T) {
	raw := strings.Join([]string{
		"+----+",
		"| group_name | node_type | nodeid | pid   |",
		"+----+",
		"| ceg1       | ce        | 1-1:1  | 36044 |",
		"+    +",
		"|            | ce        | 1-2:2  | 34424 |",
		"+----+",
	}, "\n")

	lines := dbsteps.SummarizeDBGroupStatus(raw)
	if len(lines) != 3 {
		t.Fatalf("len=%d want 3: %#v", len(lines), lines)
	}
	header := lines[0]
	row1 := lines[1]
	row2 := lines[2]

	for i, line := range lines {
		if !strings.HasPrefix(line, "|") {
			t.Fatalf("line %d must keep leading pipe for ConsoleNotice TrimSpace: %q", i, line)
		}
	}
	if !strings.Contains(header, "group_name") || !strings.Contains(header, "node_type") {
		t.Fatalf("header=%q", header)
	}
	if !strings.Contains(row1, "ceg1") {
		t.Fatalf("row1=%q", row1)
	}

	// 第二行 group_name 为空，但各列分隔 "|" 位置应与 header/row1 一致
	pipeOffsets := func(s string) []int {
		var out []int
		for i, r := range s {
			if r == '|' {
				out = append(out, i)
			}
		}
		return out
	}
	hOff, r1Off, r2Off := pipeOffsets(header), pipeOffsets(row1), pipeOffsets(row2)
	if len(hOff) != len(r1Off) || len(hOff) != len(r2Off) {
		t.Fatalf("pipe count mismatch:\n  header=%q %v\n  row1  =%q %v\n  row2  =%q %v",
			header, hOff, row1, r1Off, row2, r2Off)
	}
	for i := range hOff {
		if hOff[i] != r1Off[i] || hOff[i] != r2Off[i] {
			t.Fatalf("column misaligned at pipe %d:\n  header=%q\n  row1  =%q\n  row2  =%q",
				i, header, row1, row2)
		}
	}

	// yasboot 常见续行：空 group_name 写成相邻 "||"（无填充空格）
	compact := strings.Join([]string{
		"| group_name | node_type | nodeid |",
		"| ceg1       | ce        | 1-1:1  |",
		"|| ce        | 1-2:2  |",
	}, "\n")
	compactLines := dbsteps.SummarizeDBGroupStatus(compact)
	if len(compactLines) != 3 {
		t.Fatalf("compact len=%d: %#v", len(compactLines), compactLines)
	}
	cOff := pipeOffsets(compactLines[2])
	h2 := pipeOffsets(compactLines[0])
	if len(cOff) != len(h2) {
		t.Fatalf("compact pipe count: header=%q row2=%q", compactLines[0], compactLines[2])
	}
	for i := range h2 {
		if h2[i] != cOff[i] {
			t.Fatalf("compact misaligned:\n  %q\n  %q\n  %q", compactLines[0], compactLines[1], compactLines[2])
		}
	}
}
