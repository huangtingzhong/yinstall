package standby

import "testing"

func TestClusterNameFromEnvFileContent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "port wrapper sources bashrc",
			content: `foo=1
source /home/yashan/.yasboot/yashandb_3988_yasdb_home/conf/yashandb_3988.bashrc
`,
			want: "yashandb_3988",
		},
		{
			name: "leading spaces and tabs",
			content: "  source\t/home/yashan/.yasboot/mydb_1688_yasdb_home/conf/mydb_1688.bashrc",
			want: "mydb_1688",
		},
		{
			name: "first matching yasboot line wins",
			content: `source /home/yashan/.bashrc
source /home/yashan/.yasboot/prod_yasdb_home/conf/prod.bashrc
`,
			want: "prod",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ClusterNameFromEnvFileContent(tc.content)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClusterNameFromEnvFileContentErrors(t *testing.T) {
	t.Parallel()
	_, err := ClusterNameFromEnvFileContent("# no source line\nexport X=1\n")
	if err == nil {
		t.Fatal("expected error")
	}
}
