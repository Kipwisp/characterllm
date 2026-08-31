package version

import "testing"

func TestString(t *testing.T) {
	originalVersion, originalCommit := Version, Commit
	t.Cleanup(func() {
		Version, Commit = originalVersion, originalCommit
	})

	cases := []struct {
		name     string
		version  string
		commit   string
		expected string
	}{
		{"release version only", "v1.0.0", "abc1234", "v1.0.0"},
		{"dev with commit", "dev", "abc1234", "dev (abc1234)"},
		{"dev without commit", "dev", "none", "dev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Version, Commit = tc.version, tc.commit
			if got := String(); got != tc.expected {
				t.Errorf("String() = %q, want %q", got, tc.expected)
			}
		})
	}
}
