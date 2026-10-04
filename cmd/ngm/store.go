package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/vendor"
)

const storeUsage = `ngm store — inspect and tidy the content store (layer 2)

USAGE:
  ngm store <subcommand>

SUBCOMMANDS:
  usage   report what the content store is holding (read-only)
  prune   remove unpack residue left by interrupted builds (--dry-run available)

PRUNE FLAGS:
  --dry-run              print what would be removed without removing anything
  --orphans              ALSO remove blob bytes that no manifest references
                         (age-guarded: --older-than, default 24h)
  --older-than=<dur>     age guard for --orphans (e.g. 24h, 30m); only valid with --orphans

NOTES:
  The content store is derived data: it can always be rebuilt from the mirror by
  re-running "ngm install".

  There is deliberately **no** reachability-based deletion ("this digest is no longer
  wanted anywhere"): that needs a project registry ngm does not have, and getting it
  wrong makes some other project fail verification one day — see
  docs/adr/adr-018-store-reclaim.md.

  --orphans is a different thing: it removes bytes that NO manifest in this store
  references at all. That is decidable by construction (no registry needed), and it is
  age-guarded because "ngm install" writes bytes before it publishes the manifest they
  belong to. See docs/adr/adr-023-orphan-reclaim.md.

  EXIT CODES:
  0  reported (usage) or finished (prune) — "nothing to do" is still 0
  3  configuration error: unknown subcommand, or a manifest could not be read
  (prune refuses to delete anything in that case)
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
	fs := newFlagSet("store usage", stderr)
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
		// 三个数回答三个不同的问题：去重省了多少（共享）、丢掉某一棵树能回收多少（独占）、
		// 有多少空间**没有任何人需要**（孤儿）。最后那个不是垃圾的同义词，也不会被 prune 清掉
		// （ADR-018 决策 5），所以它必须出现在这里——否则"没人回收"只剩一句印象。
		fmt.Fprintf(stdout,
			"    └ shared by 2+ trees: %s · exclusive to one tree: %s · orphaned: %d (%s)\n",
			storeBytes(u.BlobSharedBytes), storeBytes(u.BlobExclusiveBytes),
			u.BlobOrphans, storeBytes(u.BlobOrphanBytes))
		if u.BlobOrphans > 0 {
			// v0.11 起这一行必须改：孤儿**已经可以**回收了（ADR-023），
			// 继续写"NOT removed"会让用户以为这些字节拿不回来——
			// 而它们恰恰是唯一一类"能安全删掉"的字节（无人引用，由构造可判定）。
			// 措辞要说清**为什么现在能删**：与"按可达性删除"是两件事。
			fmt.Fprintln(stdout,
				"      orphaned blobs have NO manifest referencing them, so they are reclaimable:\n"+
					"        `ngm store prune --orphans` (age-guarded; see ADR-023).\n"+
					"      This is NOT reachability-based deletion, which is deliberately not\n"+
					"      implemented — it would need a project registry ngm does not have (ADR-018).")
		}
		fmt.Fprintf(stdout, "  tree manifests (v2): %d (%s)\n", u.V2Trees, storeBytes(u.V2TreeBytes))
	}
	if u.UnreadableManifests > 0 {
		// 方向要说清，否则这三个数会被当成事实：读不出来的清单，它的 blob 会**失去引用**，
		// 于是孤儿被**多算**、共享被**少算**。两种偏差方向相反，所以不能说"是下界"。
		fmt.Fprintf(stdout,
			"  note: %d manifest(s) unreadable — an unread manifest's blobs look unreferenced,\n"+
				"        so the orphan count above is an over-count and \"shared\" is an under-count\n",
			u.UnreadableManifests)
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
		// 两列数**必须一起报**，因为它们回答的是不同的问题，而只看一列都会读错：
		//   logical   = 这棵树里所有文件的字节和（共享的 blob 会被重复计算）
		//   exclusive = **丢掉这棵树能回收多少**（只被它引用的 blob + 它自己的清单）
		// 所有树的 exclusive 加起来 + 共享字节 = blob 池总量；logical 没有这个性质。
		fmt.Fprintln(stdout, "  trees (v2; \"logical\" double-counts shared blobs, \"exclusive\" is what dropping it frees):")
		storeEntryLines(stdout, u.V2Entries, show, true)
	}
	if len(u.Trees) > 0 {
		// v1 没有这个区别：一份内容树就是一块磁盘，报出来的就是真实字节。
		fmt.Fprintln(stdout, "  trees (v1 legacy, whole tree per digest):")
		storeEntryLines(stdout, u.Trees, show, false)
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
//
// showExclusive 只对 v2 有意义（v1 的 Bytes 本来就是真实字节，没有"共享"这回事）。
func storeEntryLines(stdout io.Writer, entries []vendor.ContentStoreEntry, show int, showExclusive bool) {
	for i, e := range entries {
		if i == show {
			fmt.Fprintf(stdout, "    ... and %d more (sorted by size)\n", len(entries)-show)
			break
		}
		who := "meta unreadable"
		if e.MetaReadable {
			who = fmt.Sprintf("%s@%s", e.Repo, git.ShortSHA(e.Commit))
		}
		if showExclusive {
			fmt.Fprintf(stdout, "    %-71s %10s logical  %10s exclusive  %s\n",
				e.Digest, storeBytes(e.Bytes), storeBytes(e.ExclusiveBytes), who)
			continue
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
	fs := newFlagSet("store prune", stderr)
	dryRun := fs.Bool("dry-run", false, "print what would be removed without removing anything")
	orphans := fs.Bool("orphans", false,
		"also remove blob bytes that NO manifest references (and that are older than --older-than)")
	olderThan := fs.Duration("older-than", 24*time.Hour,
		"age guard for --orphans: blobs newer than this are never removed (a concurrent install writes bytes before publishing its manifest)")
	fs.Usage = func() { fmt.Fprint(stderr, storeUsage) }
	if err := fs.Parse(normalizeArgs(args, nil)); err != nil {
		return 3
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, storeUsage)
		return 3
	}
	// `--older-than` 只在 `--orphans` 下有意义：给了却不带 `--orphans` 就是"以为它会做点什么"
	// ——那种沉默比报错更糟（本项目对"看起来生效"的一贯态度）。
	if !*orphans && fs.Lookup("older-than").Value.String() != (24*time.Hour).String() {
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"--older-than 只在 --orphans 下有效",
			"加上 --orphans，或去掉 --older-than"))
	}

	layout, err := vendor.DefaultLayout()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	store := vendor.NewContentStore(layout.ContentRoot())

	res, err := store.Prune(*dryRun)
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

	if *orphans {
		ores, oerr := store.PruneOrphans(*olderThan, time.Now(), *dryRun)
		if oerr != nil {
			return runErr(ctx, stdout, stderr, oerr)
		}
		overb := "removed"
		if ores.DryRun {
			overb = "would remove"
		}
		fmt.Fprintf(stdout, "%s %d orphaned blob(s), freeing %s (older than %s)\n",
			overb, len(ores.Removed), storeBytes(ores.BytesFreed), ores.OlderThan)
		if ores.KeptYoung > 0 {
			// 把"没删的那些"也说出来：它是并发安装的安全阀，也是"为什么没全清掉"的答案。
			fmt.Fprintf(stdout,
				"  kept %d orphaned blob(s) younger than %s (%s) — a concurrent install\n"+
					"  writes bytes before it publishes its manifest, so recent ones are never removed\n",
				ores.KeptYoung, ores.OlderThan, storeBytes(ores.KeptYoungBytes))
		}
		fmt.Fprintf(stdout, "  %d referenced blob(s) untouched\n", ores.Referenced)
	}
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
