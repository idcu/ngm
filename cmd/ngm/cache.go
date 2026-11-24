package main

import (
	"context"
	"fmt"
	"io"

	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/receipt"
)

const cacheUsage = `ngm cache — maintain the (disposable) cache layer

USAGE:
  ngm cache <subcommand>

SUBCOMMANDS:
  clean   empty ~/.ngm/cache (mirror and content store are NOT touched)

NOTES:
  The cache layer never holds the only copy of anything: provability is
  guaranteed by mirror + content store + lock. Deleting it only costs speed.

EXIT CODES:
  0  the cache was emptied (also when it was already empty)
  3  configuration error: unknown subcommand
`

// runCache 处理 `ngm cache <subcommand>`。
func runCache(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("cache", stderr)

	c := newCmdContextNoJSON(ctx, args, stdout, stderr, fs)
	c.usage(cacheUsage)
	if !c.parse(nil, nil) {
		return 3
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, cacheUsage)
		return 3
	}
	switch fs.Arg(0) {
	case "clean":
		return runCacheClean(ctx, stdout, stderr)
	default:
		fmt.Fprint(stderr, cacheUsage)
		return 3
	}
}

// runCacheClean 清空缓存层。
//
// 刻意**不**要求项目目录存在：cache 属于用户态，与具体项目无关。
func runCacheClean(ctx context.Context, stdout, stderr io.Writer) int {
	layout, err := vendor.DefaultLayout()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	cache := vendor.NewCache(layout.CacheRoot())
	res, err := cache.Clean()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	if !res.Existed {
		fmt.Fprintf(stdout, "cache does not exist at %s (nothing to do)\n", res.Root)
		return 0
	}
	fmt.Fprintf(stdout, "cleaned %s\n", res.Root)
	if len(res.RemovedEntries) > 0 {
		fmt.Fprintf(stdout, "  removed entries: %d\n", len(res.RemovedEntries))
	}
	fmt.Fprintln(stdout, "note: the cache is disposable — mirror and content store were not touched")
	return 0
}

// buildCacheHome 返回 ngm 用户态根目录（NGM_HOME 可覆盖，与测试隔离同源）。
func buildCacheHome() string {
	layout, err := vendor.DefaultLayout()
	if err != nil {
		return ""
	}
	return layout.Home
}

// buildCacheDir 是 `ngm build --cache` 唯一使用的本地构建缓存目录。
//
// 它就在用户态 ngm 根目录下（默认 ~/.ngm/build-cache），与 `ngm cache clean`
// 清掉的层 4 缓存分开：构建缓存的键由回执字段算出（internal/receipt.CacheKey），
// 语义只有 build 一处，不能另起第二套缓存。
func buildCacheDir() string {
	return receipt.CacheDir(buildCacheHome())
}
