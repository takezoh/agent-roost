package plastic

import "testing"

func TestParseBranchFromStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
	}{
		{
			name:  "standard changeset spec",
			input: "cs:42@br:/main/feature-x@myrepo@localhost:8084\n",
			want:  "/main/feature-x",
		},
		{
			name:  "root branch",
			input: "cs:1@br:/main@repo@server:8084\n",
			want:  "/main",
		},
		{
			name:  "deep branch path",
			input: "cs:100@br:/main/release/v2.0/hotfix@repo@server:8084\n",
			want:  "/main/release/v2.0/hotfix",
		},
		{
			name:  "multiple lines with header",
			input: "STATUS cs:10@br:/main/dev@repo@server:8084\nsome other output\n",
			want:  "/main/dev",
		},
		{
			name:  "no branch spec",
			input: "some random output\n",
			want:  "",
		},
		{
			name:  "empty output",
			input: "",
			want:  "",
		},
		{
			name:  "branch spec without trailing @",
			input: "cs:5@br:/main/solo",
			want:  "/main/solo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseBranchFromStatus(tt.input)
			if got != tt.want {
				t.Errorf("ParseBranchFromStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
