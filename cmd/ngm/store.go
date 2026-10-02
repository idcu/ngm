package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/vendor"
)

const storeUsage = `ngm store — inspect and tidy the content store (layer 2)

USAGE:
  ngm store <subcommand>

SUBCOMMANDS:
  usage   report what the content store is holding (read-only)
  prune   remove unpack residue left by interrupted builds (--dry-run available)

NOTES:
  The content store is derived data: it can always be rebuilt from the mirror by
  re-running "ngm install". There is deliberately **no** subcommand that deletes a
  content tree — see docs/adr/adr-018-store-reclaim.md.
`

// runStore 处理 `ngm store <subcommand>`。
//
// 与 `ngm cache` 同形：**不要求项目目录存在**——层 2 属于用户态，与具体项目无关
// （同一个 store 被所有项目共享，这也正是"不能按项目可达性删除"的原因，见 ADR-018）。
func runStore(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	// 子命令**直接取首个非 flag 元素**，不在本层先做一次 flag 解析。
	//
	// 为什么不能按"先解析再取 Arg(0)"的常规写法：本项目的 `normalizeArgs` 会把 flag
	// **提到最前面**（为了让 `ngm add x --dir=y` 这类写法成立），于是
	// `ngm store prune --dry-run` 会变成 `--dry-run prune`，本层随即报
	// "flag provided but not defined"（实测：exit 3）。子命令各自的 flag
	// 交给各自的 flagset 解析——"哪个 flag 属于哪一层"因此只在一处定义。
	sub, rest := "", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, rest = args[0], args[1:]
	}
	switch sub {
	case "usage":
		return runStoreUsage(ctx, rest, stdout, stderr)
	case "prune":
		return runStorePrune(ctx, rest, stdout, stderr)
	default:
		fmt.Fprint(stderr, storeUsage)
		return 3
	}
}

// runStoreUsage 报告 content store 的占用（**只读**）。
//
// 为什么会需要它：v0.6 量出层 2 **只增不减**、每个 commit 复制一整棵树
// （源码真实增量的 20×），而 ADR-018 裁定不做按可达性删除。在此之前，
// 用户对这个目录只有"越用越大"的印象——连"被什么占了"都无从回答。
func runStoreUsage(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("store usage")
	fs.Usage = func() { fmt.Fprint(stderr, storeUsage) }
	if err := fs.Parse(normalizeArgs(args, nil)); err != nil {
		return 3
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, storeUsage)
		return 3
	}

	layout, err := vendor.DefaultLayout()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	u, err := vendor.NewContentStore(layout.ContentRoot()).Usage()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if !u.Exists {
		fmt.Fprintf(stdout, "no content store at %s (nothing to report)\n", u.Root)
		return 0
	}

	fmt.Fprintf(stdout, "content store: %s\n", u.Root)
	if u.V2Trees > 0 || u.Blobs > 0 {
		fmt.Fprintf(stdout, "  blobs (deduplicated content): %d (%s)\n", u.Blobs, storeBytes(u.BlobBytes))
		fmt.Fprintf(stdout, "  tree manifests (v2): %d (%s)\n", u.V2Trees, storeBytes(u.V2TreeBytes))
	}
	// v1 那一行**总是**打印：它同时是"迁移收敛到什么程度"的读数——
	// 全是 0 就说明这个 store 已经不再有旧布局的残留（ADR-019 的收敛目标）。
	fmt.Fprintf(stdout, "  content trees (v1 legacy): %d (%s)\n", len(u.Trees), storeBytes(u.TreeBytes))
	if len(u.Trees) > 0 {
		// **它不会自己变好**（ADR-019 §修订 5）：健康的 v1 条目 `Has` 为真，install 因此
		// 直接短路、不会重写它们——而那条短路是"无网络也能安装"的承诺，不能为了迁移去动它。
		// 所以这里把出路写出来，而不是让用户以为"升级之后空间会自己回收"。
		fmt.Fprintf(stdout,
			"    └ pre-v0.8 layout: still readable, but not migrated automatically "+
				"(only newly installed dependencies use the v2 layout).\n"+
				"      to reclaim them: rm -rf %s && ngm install   (layer 2 is derived; it rebuilds)\n",
			filepath.Join(u.Root, digest.Algorithm))
	}

	const show = 10
	if len(u.V2Entries) > 0 {
		// 体积是**逻辑体积**（条目字节之和）：多棵树共享 blob 时会重复计算，
		// 真实占用看上面那行 blobs。两个数都报，免得把逻辑体积读成磁盘占用。
		fmt.Fprintln(stdout, "  trees (v2, logical size; shared blobs counted once above):")
		storeEntryLines(stdout, u.V2Entries, show)
	}
	if len(u.Trees) > 0 {
		fmt.Fprintln(stdout, "  trees (v1 legacy, whole tree per digest):")
		storeEntryLines(stdout, u.Trees, show)
	}
	if u.TempCount > 0 {
		fmt.Fprintf(stdout,
			"  unpack residue: %d (%s) — leftovers from interrupted builds; \"ngm store prune\" removes them\n",
			u.TempCount, storeBytes(u.TempBytes))
	}
	fmt.Fprintln(stdout,
		"note: read-only, nothing was changed. Deleting a content tree is deliberately not offered (ADR-018).")
	return 0
}

// storeEntryLines 打印一行一条内容树（v2 的清单与 v1 的整棵树共用同一格式）。
//
// 顺序由 `Usage` 负责**确定**（体积降序、同体积按 digest 升序）：
// 依赖目录遍历顺序的输出会让人误以为"内容变了"。
func storeEntryLines(stdout io.Writer, entries []vendor.ContentStoreEntry, show int) {
	for i, e := range entries {
		if i == show {
			fmt.Fprintf(stdout, "    ... and %d more (sorted by size)\n", len(entries)-show)
			break
		}
		who := "meta unreadable"
		if e.MetaReadable {
			who = fmt.Sprintf("%s@%s", e.Repo, git.ShortSHA(e.Commit))
		}
		fmt.Fprintf(stdout, "    %-71s %10s  %s\n", e.Digest, storeBytes(e.Bytes), who)
	}
}

// runStorePrune 只清解包残骸；**绝不碰内容树**。
//
// 输出里刻意写上"留下了 N 份内容树没动"：一个删除类命令如果只说"删了什么"，
// 用户就得自己猜"它是不是顺手删了别的东西"。把留下来的数量写出来，
// 就是把安全声明放在它该在的地方。
func runStorePrune(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("store prune")
	dryRun := fs.Bool("dry-run", false, "print what would be removed without removing anything")
	fs.Usage = func() { fmt.Fprint(stderr, storeUsage) }
	if err := fs.Parse(normalizeArgs(args, nil)); err != nil {
		return 3
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, storeUsage)
		return 3
	}

	layout, err := vendor.DefaultLayout()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	res, err := vendor.NewContentStore(layout.ContentRoot()).Prune(*dryRun)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	verb := "removed"
	if res.DryRun {
		verb = "would remove"
	}
	if len(res.Removed) == 0 {
		fmt.Fprintf(stdout, "no unpack residue in %s (nothing to do)\n", res.Root)
	} else {
		fmt.Fprintf(stdout, "%s %d unpack residue directory(ies), freeing %s:\n",
			verb, len(res.Removed), storeBytes(res.BytesFreed))
		for _, n := range res.Removed {
			fmt.Fprintf(stdout, "  %s\n", n)
		}
	}
	fmt.Fprintf(stdout, "kept %d content tree(s) untouched\n", res.KeptTrees)
	return 0
}

// storeBytes 把字节数印成人能读的形态（与仓库其余输出一致：不引入新单位）。
func storeBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
