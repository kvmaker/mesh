package version

import "testing"

// TestGet 直接读写包级 Version 变量。Version 的设计本身就是构建期注入的
// 全局变量（-ldflags -X），因此这里通过运行时改写它来模拟不同注入值；
// 同一进程内 t.Run 顺序执行，无并发访问，无需 mutex。
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
