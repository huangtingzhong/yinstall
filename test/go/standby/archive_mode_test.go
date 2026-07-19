package standby_test

import (
	"testing"

	"github.com/yinstall/internal/steps/standby"
)

func TestIsArchiveLogModeOutput(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{
			name: "archivelog table",
			out: `
LOG_MODE          
----------------- 
ARCHIVELOG     

1 row fetched.
`,
			want: true,
		},
		{
			name: "noarchivelog must not false-positive",
			out: `
LOG_MODE          
----------------- 
NOARCHIVELOG     

1 row fetched.
`,
			want: false,
		},
		{
			name: "empty",
			out:  "",
			want: false,
		},
		{
			name: "substring trap in prose",
			out:  "status is noarchivelog currently",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := standby.IsArchiveLogModeOutput(tc.out); got != tc.want {
				t.Fatalf("IsArchiveLogModeOutput()=%v want %v for %q", got, tc.want, tc.out)
			}
		})
	}
}
