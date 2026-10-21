package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/vendor"
)

const storeUsage = `ngm store — inspect and tidy ngm's on-disk layers

USAGE:
  ngm store <subcommand>

SUBCOMMANDS:
  usage   report what ngm holds on disk, layer by layer (read-only)
  prune   remove residue left by interrupted clones and builds (--dry-run available)
  gc      consolidate the git mirrors with "git gc" (--dry-run available)

PRUNE FLAGS:
  --dry-run              print what would be removed without removing anything
  --orphans              ALSO remove blob bytes that no manifest references
                         (age-guarded: --older-than, default 24h)
  --older-than=<dur>     age guard for --orphans (e.g. 24h, 30m); only valid with --orphans

GC FLAGS:
  --dry-run              print which mirrors would be collected without running git

NOTES:
  Two layers live under ngm's home, and they grow for different reasons:

    mirror (layer 1)   bare git clones. Every fetch can add objects, and only
                       "git gc" consolidates them — that is what "ngm store gc" runs.
                       An interrupted clone leaves a temporary pack
                       (objects/pack/tmp_*) which git itself keeps for TWO WEEKS
                       (measured: 13 days survive, 15 days are removed); that residue
                       is what "ngm store prune" reclaims after one hour.
    content (layer 2)  derived data: it can always be rebuilt from the mirror by
                       re-running "ngm install".

  There is deliberately **no** reachability-based deletion in either layer ("this
  digest / this commit is no longer wanted anywhere"): that needs a project registry
  ngm does not have, and getting it wrong makes some other project fail verification
  one day — see docs/adr/adr-018-store-reclaim.md and docs/adr/adr-030-mirror-reclaim.md.

  --orphans is a different thing: it removes bytes that NO manifest in this store
  references at all. That is decidable by construction (no registry needed), and it is
  age-guarded because "ngm install" writes bytes before it publishes the manifest they
  belong to. See docs/adr/adr-023-orphan-reclaim.md.

EXIT CODES:
  0  reported (usage) or finished (prune / gc) — "nothing to do" is still 0
  3  configuration error: unknown subcommand, or a manifest could not be read
     (prune refuses to delete anything in that case)
  4  gc could not collect a mirror: git failed on it (the other mirrors were left
     collected; run "git gc" in that mirror by hand to see git's own message)
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
	case "gc":
		return runStoreGC(ctx, rest, stdout, stderr)
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
	// 两层都读：**层 1 不能挂在层 2 的提前返回后面**。
	//
	// v0.100 之前的形状是"content store 不存在 ⇒ 打印 nothing to report 并 return 0"。
	// 那条路在只有层 1 的机器上是错的：一个有 mirror、没有 content store 的用户
	// （例如刚 clone 过、还没 install）会被告知"没有可报告的东西"——
	// 而他打开这个命令正是想看那些 mirror 占了多大。
	mirror, err := vendor.NewMirror(layout.MirrorRoot(), git.Options{}).Usage()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	mc, err := vendor.NewModuleCache(layout.ModuleCacheRoot(), git.Options{}).Usage()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	u, err := vendor.NewContentStore(layout.ContentRoot()).Usage()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if !mirror.Exists && !mc.Exists && !u.Exists {
		fmt.Fprintf(stdout,
			"nothing to report: none of %s, %s or %s exists yet\n",
			mirror.Root, mc.Root, u.Root)
		return 0
	}

	// 层 1 先报：它是**根**（content store 是它的派生物），体积通常也是最大的一块。
	fmt.Fprintf(stdout, "mirror (layer 1): %s\n", mirror.Root)
	if !mirror.Exists {
		fmt.Fprintf(stdout, "  not created yet (%s does not exist)\n", mirror.Root)
	} else {
		fmt.Fprintf(stdout,
			"  %d repo(s), %s on disk — of which %s is packed history\n",
			mirror.Repos, storeBytes(mirror.Bytes), storeBytes(mirror.PackBytes))
		// 逐仓库一行：只列**非零**的那些维度，避免把"没有的事"印成 0。
		//
		// 上限 10 与层 2 那段（`storeEntryLines` 里同样是 10）取同一个口径：
		// 报告要能一眼看完，而"还有多少个"必须说出来——一个被静默截断的清单
		// 会让人以为"就这么多"。
		const showMirrors = 10
		for i, e := range mirror.Entries {
			if i == showMirrors {
				fmt.Fprintf(stdout, "    ... and %d more (sorted by path)\n", len(mirror.Entries)-showMirrors)
				break
			}
			fmt.Fprintf(stdout, "    %-52s %10s", e.Repo, storeBytes(e.Bytes))
			if e.ResidueBytes > 0 {
				fmt.Fprintf(stdout, "  residue: %d (%s)", e.ResidueCount, storeBytes(e.ResidueBytes))
			}
			if e.NotMirror {
				fmt.Fprintf(stdout, "  NOT A MIRROR")
			}
			fmt.Fprintln(stdout)
		}
		if mirror.NotMirrorCount > 0 {
			// 这一句是这个报告里最值钱的一条：有体积、却解析不出任何 commit 的目录。
			fmt.Fprintf(stdout,
				"  note: %d path(s) are NOT mirrors — a clone that was interrupted leaves a\n"+
					"        usable-looking bare repo whose config has no %s refspec,\n"+
					"        and `git fetch` on it succeeds while fetching NOTHING. To rebuild:\n"+
					"        rm -rf that directory + run \"ngm install\" again (it re-clones).\n",
				mirror.NotMirrorCount, git.MirrorRefspec)
		}
		if len(mirror.Landings) > 0 {
			// 第二值钱的一条：**正在落地**的克隆（v0.101）。它是上面那条的**成因**——
			// 从前克隆直接落在最终路径上，一次中断就留下一个"长得像能用的 mirror"。
			fmt.Fprintf(stdout,
				"  unfinished clone(s): %d (%s) — ngm clones into a temporary directory and\n"+
					"        renames it into place, so these are a clone that failed or was killed.\n"+
					"        \"ngm store prune\" reclaims them after one hour:\n",
				len(mirror.Landings), storeBytes(mirror.LandingBytes))
			for i, rel := range mirror.Landings {
				if i == showMirrors {
					fmt.Fprintf(stdout, "          ... and %d more (sorted by path)\n", len(mirror.Landings)-showMirrors)
					break
				}
				fmt.Fprintf(stdout, "          %s\n", rel)
			}
		}
		if mirror.ResidueCount > 0 {
			fmt.Fprintf(stdout,
				"  residue: %d temporary pack(s) (%s) — left by an interrupted clone or fetch;\n"+
					"        git itself keeps these for TWO WEEKS, \"ngm store prune\" removes them\n"+
					"        after one hour. Run \"git gc\" for the history itself: ngm store gc\n",
				mirror.ResidueCount, storeBytes(mirror.ResidueBytes))
		} else {
			fmt.Fprintln(stdout,
				"  no residue: nothing was left behind by an interrupted clone (ngm store prune would do nothing)")
		}
	}

	fmt.Fprintf(stdout, "module cache (layer 1.5): %s\n", mc.Root)
	if !mc.Exists {
		fmt.Fprintf(stdout, "  not created yet (%s does not exist)\n", mc.Root)
	} else {
		fmt.Fprintf(stdout,
			"  %d repo(s), %s on disk — of which %s is packed history\n",
			mc.Repos, storeBytes(mc.Bytes), storeBytes(mc.PackBytes))
		const showModuleCaches = 10
		for i, e := range mc.Entries {
			if i == showModuleCaches {
				fmt.Fprintf(stdout, "    ... and %d more (sorted by path)\n", len(mc.Entries)-showModuleCaches)
				break
			}
			fmt.Fprintf(stdout, "    %-52s %10s", e.Repo, storeBytes(e.Bytes))
			if e.NotPartial {
				fmt.Fprintf(stdout, "  NOT A PARTIAL CLONE")
			}
			fmt.Fprintln(stdout)
		}
		if mc.NotPartialCount > 0 {
			fmt.Fprintf(stdout,
				"  note: %d path(s) are NOT partial clones — the filter was not applied or its\n"+
					"        config changed. They are disposable: delete them and run `ngm install`\n"+
					"        again (ngm will re-create the module cache).\n",
				mc.NotPartialCount)
		}
	}

	fmt.Fprintf(stdout, "content store (layer 2): %s\n", u.Root)
	if !u.Exists {
		fmt.Fprintf(stdout, "  not created yet (%s does not exist)\n", u.Root)
	}
	if u.Exists && (u.V2Trees > 0 || u.Blobs > 0) {
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
	if u.Exists && u.UnreadableManifests > 0 {
		// 方向要说清，否则这三个数会被当成事实：读不出来的清单，它的 blob 会**失去引用**，
		// 于是孤儿被**多算**、共享被**少算**。两种偏差方向相反，所以不能说"是下界"。
		fmt.Fprintf(stdout,
			"  note: %d manifest(s) unreadable — an unread manifest's blobs look unreferenced,\n"+
				"        so the orphan count above is an over-count and \"shared\" is an under-count\n",
			u.UnreadableManifests)
	}
	if u.Exists {
		// v1 那一行在 store 存在时**总是**打印：它同时是"迁移收敛到什么程度"的读数——
		// 全是 0 就说明这个 store 已经不再有旧布局的残留（ADR-019 的收敛目标）。
		// （v0.100 起多了一个前置：store 不存在时不打印它。此前是靠上面那条
		// 提前返回来保证这一行不会在"没有 store"时出现，而那条返回会连层 1 一起吞掉。）
		fmt.Fprintf(stdout, "  content trees (v1 legacy): %d (%s)\n", len(u.Trees), storeBytes(u.TreeBytes))
	}
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
		"note: read-only, nothing was changed. Deleting content trees (layer 2) and deleting\n"+
			"      packed history (layer 1) are both deliberately not offered (ADR-018/030);\n"+
			"      the module cache (layer 1.5) is a disposable cache — deleting it only costs\n"+
			"      a re-fetch, not provenance. The reclaim paths on offer are \"ngm store prune\"\n"+
			"      (residue) and \"ngm store gc\" (consolidate mirror packfiles).")
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
	//
	// v0.66：判定"用户是否显式给过"必须用 `fs.Visit`——此前比较的是 Duration 的
	// 字符串形态，而 `--older-than=24h` 解析后与默认值归一化后完全相同，
	// "改过"与"没改过"分不开（评估报告 P3：字符串比较 Duration 判断默认值）。
	olderThanSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "older-than" {
			olderThanSet = true
		}
	})
	if !*orphans && olderThanSet {
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
	if res.KeptYoung > 0 {
		// 与 --orphans 的那条同源：把"没删的那些"说出来，它是并发安装的安全阀
		// （v0.66 起 Prune 有年龄门槛——`Put` 的临时清单目录用同一个 `.unpack-` 前缀）。
		fmt.Fprintf(stdout,
			"  kept %d unpack residue directory(ies) younger than %s — a concurrent install\n"+
				"  creates them before publishing its manifest, so recent ones are never removed\n",
			res.KeptYoung, vendor.UnpackResidueGrace)
	}

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

	// ---- 层 1（mirror）的残骸（v0.100 / ADR-030）----
	//
	// 这一段**追加**在既有输出之后，而不是插进层 2 的几段之间：既有那几行的顺序
	// 属于对外契约（文档与脚本在读它），加一层新东西没有理由去动它。
	// 代价是这里的顺序（层 2 先、层 1 后）与 `store usage` 的（层 1 先）相反——
	// 写在这里，免得后来者以为是排错了。
	mres, merr := vendor.NewMirror(layout.MirrorRoot(), git.Options{}).
		PruneResidue(vendor.MirrorResidueGrace, time.Now(), *dryRun)
	if merr != nil {
		return runErr(ctx, stdout, stderr, merr)
	}
	mverb := "removed"
	if mres.DryRun {
		mverb = "would remove"
	}
	if len(mres.Removed) == 0 && len(mres.LandingsRemoved) == 0 {
		fmt.Fprintf(stdout, "no mirror residue and no unfinished clone in %s (nothing to do)\n", mres.Root)
	} else {
		if len(mres.Removed) > 0 {
			fmt.Fprintf(stdout, "%s %d mirror residue file(s), freeing %s:\n",
				mverb, len(mres.Removed), storeBytes(mres.BytesFreed))
			for _, n := range mres.Removed {
				fmt.Fprintf(stdout, "  %s\n", n)
			}
		}
		if len(mres.LandingsRemoved) > 0 {
			// 与残骸文件分开报：删掉一个**目录**与删掉一个临时文件不是同一件事，
			// 而"删了 N 个"这个数如果不分类，就说不出删掉的是什么。
			fmt.Fprintf(stdout, "%s %d unfinished clone(s), freeing %s:\n",
				mverb, len(mres.LandingsRemoved), storeBytes(mres.LandingBytesFreed))
			for _, n := range mres.LandingsRemoved {
				fmt.Fprintf(stdout, "  %s\n", n)
			}
		}
	}
	fmt.Fprintf(stdout, "kept %d mirror(s) untouched\n", mres.Scanned-mres.Touched)
	if mres.KeptYoung > 0 {
		// 与层 2 那条同源：把"没删的那些"说出来，它是并发克隆的安全阀
		// （mirror 层没有跨进程锁，见 MirrorResidueGrace 的说明）。
		//
		// 措辞是 item(s) 而不是 file(s)：这个数从 v0.101 起**两类都算**
		// （临时包与没落地的克隆目录），因为保护它们的是同一个东西——一次进行中的克隆。
		fmt.Fprintf(stdout,
			"  kept %d mirror residue item(s) younger than %s (%s) — another clone or\n"+
				"  fetch may be writing them right now, so recent ones are never removed\n",
			mres.KeptYoung, vendor.MirrorResidueGrace, storeBytes(mres.KeptYoungBytes))
	}
	return 0
}

// runStoreGC 对层 1 的每个 mirror 跑一次 `git gc`（整理历史本体）。
//
// 为什么它值得是一条命令（v0.100 / ADR-030）：`store usage` 已经把每个 mirror
// 的体积摆出来了，而"看得见却点不动"是一种坏形状——用户拿到"这个 mirror 占了
// 18 MB"之后，下一步只能自己去猜 `~/.ngm/mirror` 在哪、该跑什么。
func runStoreGC(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("store gc", stderr)
	dryRun := fs.Bool("dry-run", false,
		"print which mirrors would be collected without running git")
	fs.Usage = func() { fmt.Fprint(stderr, storeUsage) }
	if err := fs.Parse(normalizeArgs(args, nil)); err != nil {
		return 3
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, storeUsage)
		return 3
	}

	// 这里要 `newProjectEnv` 而不是只取 `layout`：本命令**要跑 git**，
	// 因此 `run:git` 的判定与凭证脱敏必须来自与别的命令同一处，而不是在本函数里
	// 现拼一个空的 `git.Options`（那会让权限配置在这条路上静默失效）。
	// `newProjectEnv` 不建目录、也不读项目清单，对全局命令是安全的。
	//
	// 代价是**坏掉的全局配置会在这里退 3**（`store usage` 则不受影响）：
	// 这是有意的——跑 git 之前必须先把"允不允许跑、哪些值要脱敏"读对，
	// 而把它当成"没配"会让用户以为自己写的 deny 已经生效（env.go 那条注释同源）。
	env, err := newProjectEnv("")
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	res, gerr := env.Mirror.GC(ctx, *dryRun)

	// 先报**已经做完的那部分**，再报失败：`GC` 在某个 mirror 上失败时会继续
	// 整理其余的，把结果丢掉会让"哪些已经整理过了"变成不可知——而用户接下来
	// 要不要重跑，正取决于这个。
	verb := "collected"
	if res.DryRun {
		verb = "would collect"
	}
	switch {
	case len(res.Collected) > 0:
		fmt.Fprintf(stdout, "%s %d mirror(s), reclaiming %s:\n",
			verb, len(res.Collected), storeBytes(res.Reclaimed()))
		for _, n := range res.Collected {
			fmt.Fprintf(stdout, "  %s\n", n)
		}
	case gerr == nil:
		fmt.Fprintf(stdout, "no mirror to collect in %s (nothing to do)\n", res.Root)
	}
	if len(res.SkippedIncomplete) > 0 {
		// 跳过要**说清为什么**：这些目录有体积、却不是一个可用的 mirror
		// （config 里没有镜像 refspec），因此 `git gc` 对它们没有意义
		// （而且它的失败会伪装成"整理失败"）。
		fmt.Fprintf(stdout,
			"  skipped %d mirror(s) whose clone never finished (no mirror refspec): %s\n",
			len(res.SkippedIncomplete), strings.Join(res.SkippedIncomplete, ", "))
	}
	if len(res.SkippedNotMirror) > 0 {
		fmt.Fprintf(stdout,
			"  skipped %d path(s) that are not bare mirrors (ngm did not touch them): %s\n",
			len(res.SkippedNotMirror), strings.Join(res.SkippedNotMirror, ", "))
	}
	if !res.DryRun && len(res.Collected) > 0 {
		fmt.Fprintln(stdout,
			"note: git keeps unreachable objects for 2 weeks by default, and ngm does not\n"+
				"      shorten that window (a branch the upstream deleted stays recoverable).")
	}
	if gerr != nil {
		return runErr(ctx, stdout, stderr, gerr)
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
