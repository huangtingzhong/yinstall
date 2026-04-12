package standby

import "testing"

func TestPortFromListenAddr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int
	}{
		{"10.10.10.130:3988", 3988},
		{"*:3988", 3988},
		{"0.0.0.0:1688", 1688},
		{"[::1]:1234", 1234},
	}
	for _, tc := range cases {
		got, err := PortFromListenAddr(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %d want %d", tc.in, got, tc.want)
		}
	}
}

func TestPortFromListenAddrErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "nocolon", "host:", ":"} {
		if _, err := PortFromListenAddr(in); err == nil {
			t.Fatalf("expected error for %q", in)
		}
	}
}

func TestParseListenAddrPortFromYasqlStdout(t *testing.T) {
	t.Parallel()
	sample := `VALUE                                                            
---------------------------------------------------------------- 
10.10.10.130:3988                                                

1 row fetched.
`
	p, err := parseListenAddrPortFromYasqlStdout(sample)
	if err != nil {
		t.Fatal(err)
	}
	if p != 3988 {
		t.Fatalf("got %d", p)
	}
}

func TestParseListenAddrPortFromYasqlStdoutPipe(t *testing.T) {
	t.Parallel()
	sample := `NAME            | VALUE             
----------------+-------------------
LISTEN_ADDR     | 10.10.10.130:3988
`
	p, err := parseListenAddrPortFromYasqlStdout(sample)
	if err != nil {
		t.Fatal(err)
	}
	if p != 3988 {
		t.Fatalf("got %d", p)
	}
}
