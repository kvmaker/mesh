package version

import "testing"

func TestGet(t *testing.T) {
	cases := []struct {
		name string
		set  string
		want string
	}{
		{"default", "dev", "dev"},
		{"empty falls back to dev", "", "dev"},
		{"semver", "v2.2.0", "v2.2.0"},
		{"git describe output", "v2.2.0-32-g9025ff4-dirty", "v2.2.0-32-g9025ff4-dirty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Version = tc.set
			defer func() { Version = "dev" }()
			if got := Get(); got != tc.want {
				t.Errorf("Get() = %q, want %q", got, tc.want)
			}
		})
	}
}
