// Package version 提供构建时注入的版本元数据。
//
// 真实值通过 ldflags 在构建时注入：
//
//	go build -ldflags "\
//	  -X github.com/idcu/ngm/internal/version.Version=0.1.0 \
//	  -X github.com/idcu/ngm/internal/version.GitCommit=abc1234 \
//	  -X github.com/idcu/ngm/internal/version.BuildTime=2026-09-29T00:00:00Z" \
//	  ./cmd/ngm
//
// 默认值用于 `go run` 或未注入的本地构建；正式发布流水线必须注入三字段。
package version

// 下列变量通过 -ldflags -X 在构建时覆盖。
var (
	// Version 语义化版本（如 "0.1.0"）。
	Version = "0.0.0-dev"
	// GitCommit 短哈希或完整哈希；"dev" 表示未在 git 仓内构建。
	GitCommit = "dev"
	// BuildTime RFC3339 时间戳。
	BuildTime = "unknown"
)

// String 返回格式化字符串，对齐 installation.md 的示例输出：
// `ngm 0.1.0 (git:abc1234, built: 2026-09-29)`。
func String() string {
	return Version + " (git:" + GitCommit + ", built: " + BuildTime + ")"
}
