package main

import (
	"context"
	"fmt"
	"io"

	"github.com/idcu/ngm/internal/vendor"
)

const cacheUsage = `ngm cache — maintain the (disposable) cache layer

USAGE:
  ngm cache <subcommand>

SUBCOMMANDS:
  clean   empty ~/.ngm/cache (mirror and content store are NOT touched)

NOTES:
  The cache layer never holds the only copy of anything: provability is
  guaranteed by mirror + content store + lock. Deleting it only costs speed.
`

// runCache 处理 `ngm cache <subcommand>`。
func runCache(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("cache")
	fs.Usage = func() { fmt.Fprint(stderr, cacheUsage) }
	if err := fs.Parse(normalizeArgs(args, nil)); err != nil {
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
