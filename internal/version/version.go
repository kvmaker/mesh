// Package version 提供构建期注入的软件版本号。
//
// 编译时通过 -ldflags 注入：
//
//	go build -ldflags "-X github.com/maxyu/mesh/internal/version.Version=v1.0.0" ./cmd/mesh
//
// 未注入时默认为 "dev"。
package version

// Version 由构建脚本通过 -ldflags 注入；本地开发或未走 release 流程时为 "dev"。
var Version = "dev"

// Get 返回当前软件版本字符串。
func Get() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
