// 本文件是**层 1（mirror）的占用读数与回收**——与 `content_maintain.go`
// 对层 2 做的事同形，理由也同源：一个只增不减的目录，至少要回答"被什么占了"
// 与"哪些字节是残骸"。
//
// 为什么到现在才有（v0.100 / [ADR-030](../docs/adr/adr-030-mirror-reclaim.md)）：
// v0.7 起 `store usage` / `store prune` 只为层 2 存在，而**层 1 的出路一直写在文档里**：
// `docs/internals/metrics.md` 的"两层的分工"表说"mirror 的出路是 `git gc`，用户随时可跑"。
// 那句话在当时是对的，代价是用户得自己知道 `~/.ngm/mirror` 里有什么、以及该跑什么。
// 本文件把那个出路变成 ngm 自己的一等公民：**读数（Usage）、残骸（PruneResidue）、
// 整理（GC）**，并把"哪些字节是纯浪费"从印象变成数字。
package vendor

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
)

// mirrorDirSuffix 是 mirror 里每个仓库的目录后缀（`PathFor` 的约定，见 mirror.go）。
//
// 它在这里出现是为了**遍历**：`Usage` / `PruneResidue` / `GC` 都要从磁盘上找出
// "有哪些 mirror"，而不能只看某个 lock 里提到的那些——一个依赖被移除之后，
// 它的 mirror 仍然占着磁盘，而那正是用户打开 `store usage` 想看的东西。
const mirrorDirSuffix = ".git"

// mirrorResiduePrefix 是 git 在 `objects/pack/` 下写**临时**包与临时索引时用的前缀。
//
// 范围是刻意收窄的：只认 `objects/pack/tmp_*`。理由是实测到的形态——
// 一次被中断的 `git clone --mirror` 留下的就是一个 `tmp_pack_XXXXXXXX`
// （本机量到 **66.82 MiB**、对象库为空、`packed-refs` 不存在，见 ADR-030）。
// git 还会在 `objects/<shard>/tmp_obj_*` 下留单个对象的临时文件，那是 KB 级的，
// 不进这一版的口径——**口径写在这里，是为了让"没清干净"永远有一个可核对的边界**，
// 而不是靠"应该没事"。
const mirrorResiduePrefix = "tmp_"

// MirrorResidueGrace 是 mirror 残骸的年龄门槛。
//
// 为什么必须有：`git gc` / `git fetch` 的临时包由**另一个进程**持有，而 mirror
// 层**没有跨进程锁**（mirror.go 的 `Mirror` 文档如实写了这一点）。并发跑
// `ngm store prune` 与另一个 `ngm install` 时，无门槛的删除会砍掉正在进行中的克隆。
//
// 取 1 小时的依据：临时包的生命周期就是那次 `git clone --mirror` / `git fetch`
// 本身。JS 依赖的仓库在这个量级上远短于一小时，所以门槛对真实的并发足够宽松；
// 同时它比 git 自己的门槛（实测 14 天，见 `Mirror.PruneResidue` 的说明）
// 短得多——那正是这条路径存在的理由：**中断一次克隆，用户不该等两周才拿回磁盘**。
const MirrorResidueGrace = time.Hour

// MirrorEntry 是 `ngm store usage` 里的一行（一个 mirror 仓库）。
type MirrorEntry struct {
	// Repo 是相对 mirror 根的路径（斜杠分隔），如 `github.com/org/repo.git`。
	Repo string
	// Bytes 是该目录占用的字节合计（**真实占用**）。
	Bytes int64
	// PackBytes 是 `objects/pack/*.pack` 的字节和——**历史本体**。
	//
	// 它与 Bytes 的差是索引、refs、日志与松散对象：报出来是为了让
	// "一个 18 MiB 的 mirror 里哪些是历史"成为一个可回答的问题。
	PackBytes int64
	// ResidueCount / ResidueBytes 是被中断的克隆/fetch 留下的临时包。
	//
	// 它们**不是历史**，是残骸：git 自己不认它们（`git count-objects -v`
	// 把它们记在 `garbage` 里），而 `store prune` 会清掉它们。
	ResidueCount int
	ResidueBytes int64
	// NotMirror 为 true 表示这个目录**不是一个可用的 mirror**（`git.IsMirror` 为假）。
	//
	// 这种仓库解析不出任何 commit：它是一次被中断的克隆，或者被人删过 refs。
	// 它有体积、却做不了任何事——`store usage` 必须把它报出来，因为它是
	// "磁盘被占了但不是可用的 mirror"这种情形里最容易被误读成"历史很大"的一种。
	//
	// v0.100 这一格叫 `Refless`（判据是"一个 ref 都没有"），v0.101 换成了成因判据
	// （config 里没有镜像 refspec）：实测**从空上游成功克隆**出来的 mirror 也一个 ref
	// 都没有，而它是完全正常的——留着旧判据会让 `usage` 对着一台好 mirror 说
	// "这是一次被中断的克隆，删掉它"，也就是**给错建议**。
	NotMirror bool
}

// MirrorUsage 是层 1 的只读报告（`ngm store usage` 的 mirror 段）。
//
// **它不修改任何东西**（与 `ContentStoreUsage` 同一条纪律）：所有字段都来自
// 目录遍历与 `Lstat` 口径的字节数，不跑 git、不触网。
type MirrorUsage struct {
	// Root 是 mirror 根目录（通常是 `~/.ngm/mirror`）。
	Root string
	// Exists 为 false 表示 mirror 根还不存在（没有可报告的东西，这不是错误）。
	Exists bool
	// Entries 按 Repo 升序——顺序必须**确定**：依赖目录遍历顺序的输出会让人
	// 误以为"内容变了"（与 `ContentStoreUsage.Trees` 同一条理由）。
	Entries []MirrorEntry
	// Repos / Bytes / PackBytes / ResidueCount / ResidueBytes / NotMirrorCount
	// 是上面各行的小计。不变量：Repos == len(Entries)，且各项等于逐行之和
	// （`Σ Bytes` 这一条由测试守着——小计与明细对不上，是这种报告最坏的一种错）。
	Repos          int
	Bytes          int64
	PackBytes      int64
	ResidueCount   int
	ResidueBytes   int64
	NotMirrorCount int
	// Landings 是**没落地的克隆**（`<repo>.git.clone-<hex>`，见 CloneLandingInfix）。
	//
	// 它们不是仓库行，因此**不进** Entries/Bytes（小计的语义是"逐行之和"）；
	// 单独报，是因为它们正是一次失败/被中断的克隆在磁盘上留下的东西——
	// 而 v0.100 的实测里，**一个 66.82 MiB 的**就是这种，且当时谁也看不见它。
	Landings     []string
	LandingBytes int64
}

// Usage 只读地统计 mirror 层的占用。
func (m *Mirror) Usage() (MirrorUsage, error) {
	out := MirrorUsage{Root: m.root}
	if _, err := os.Stat(m.root); err == nil {
		out.Exists = true
	} else if !os.IsNotExist(err) {
		return out, errs.Wrap(errs.CodeConfigInvalid, "read the mirror root", "", err)
	}

	repos, err := m.repoDirs()
	if err != nil {
		return out, err
	}
	out.Repos = len(repos)
	for _, rel := range repos {
		dir := filepath.Join(m.root, filepath.FromSlash(rel))
		e := MirrorEntry{Repo: rel, NotMirror: !git.IsMirror(dir)}
		if e.Bytes, err = dirBytes(dir); err != nil {
			return out, err
		}
		if e.PackBytes, err = packFileBytes(dir); err != nil {
			return out, err
		}
		res, rerr := residueFiles(dir)
		if rerr != nil {
			return out, rerr
		}
		for _, f := range res {
			e.ResidueCount++
			e.ResidueBytes += f.bytes
		}
		if e.NotMirror {
			out.NotMirrorCount++
		}
		out.Bytes += e.Bytes
		out.PackBytes += e.PackBytes
		out.ResidueCount += e.ResidueCount
		out.ResidueBytes += e.ResidueBytes
		out.Entries = append(out.Entries, e)
	}

	landings, lerr := m.landingDirs()
	if lerr != nil {
		return out, lerr
	}
	for _, l := range landings {
		out.Landings = append(out.Landings, l.rel)
		out.LandingBytes += l.bytes
	}
	return out, nil
}

// repoDirs 列出 mirror 根下所有仓库目录（相对根的斜杠路径，升序）。
//
// 按后缀识别而不按 `<host>/<org>/<repo>.git` 这个深度：布局将来收窄或放宽
// （例如带分组的 host）时，遍历不会跟着失效。找到 `.git` 目录后**不再往下走**——
// 裸仓库内部不是仓库，继续遍历只会把 `objects/` 里的东西当成候选。
func (m *Mirror) repoDirs() ([]string, error) {
	root := m.root
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read the mirror root", "", err)
	}
	var out []string
	werr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || p == root {
			return nil
		}
		if !strings.HasSuffix(d.Name(), mirrorDirSuffix) {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return fs.SkipDir
	})
	if werr != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "walk the mirror root", "", werr)
	}
	sort.Strings(out)
	return out, nil
}

// residueFile 是一个残骸文件。
type residueFile struct {
	path    string
	name    string
	bytes   int64
	modTime time.Time
}

// residueFiles 返回 `objects/pack/` 下的残骸（按名字升序）。
//
// "什么算残骸"只有这一处定义：`Usage` 用它报数，`PruneResidue` 用它决定删什么。
// 两处一旦各写一份判断，后果不是数字对不上，而是**删掉正在进行中的克隆**——
// 与 `unpackPrefix` 单点定义同一条纪律（content_maintain.go 那条注释）。
func residueFiles(repoDir string) ([]residueFile, error) {
	packDir := filepath.Join(repoDir, "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		if os.IsNotExist(err) {
			// 没有 `objects/pack` 不是错误：中断得足够早的克隆正是这个形状。
			return nil, nil
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read a mirror pack directory", "", err)
	}
	var out []residueFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), mirrorResiduePrefix) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			return nil, errs.Wrap(errs.CodeConfigInvalid, "stat mirror residue "+e.Name(),
				"check permissions on the mirror", ierr)
		}
		out = append(out, residueFile{
			path:    filepath.Join(packDir, e.Name()),
			name:    e.Name(),
			bytes:   info.Size(),
			modTime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// packFileBytes 求和 `objects/pack/*.pack`（历史本体；`.idx` 与 `.rev` 不计——
// 它们是索引，比包本身小一到两个数量级，混进来只会让"历史有多大"这个读数变糊）。
//
// **0 是合法的读数**（实测）：`git clone` 落的是松散对象，因此一个刚建的 mirror
// 完全可以是 "PackBytes=0 而 Bytes>0"——那不是"没有历史"，是"历史还没被收成包"。
// 把它读成后者，就会得出"这个 mirror 是空的"这种错结论；`store usage` 的措辞
// 因此是 "packed history" 而不是 "history"。
func packFileBytes(repoDir string) (int64, error) {
	packDir := filepath.Join(repoDir, "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, errs.Wrap(errs.CodeConfigInvalid, "read a mirror pack directory", "", err)
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pack") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			return 0, errs.Wrap(errs.CodeConfigInvalid, "stat mirror pack "+e.Name(), "", ierr)
		}
		total += info.Size()
	}
	return total, nil
}

// landingDir 是一个**没落地的克隆**目录（`<repo>.git.clone-<hex>`）。
type landingDir struct {
	rel     string // 相对 mirror 根的斜杠路径
	path    string
	bytes   int64
	modTime time.Time
}

// isLandingDirName 报告一个目录名是不是**我们自己**留下的落地目录。
//
// 判据是**名字的形状**：`<非空的 X>.git` + [CloneLandingInfix] + 十六进制后缀
// （`randHex` 给的后缀；取不到随机数时退回的十进制 pid 也是十六进制字符的子集，同样落在这里）。
//
// 这条判据同时是**归属声明**，也是它与"没有 ref 的 `<repo>.git`"处置不同的原因：
//
//   - `<repo>.git`：名字是**通用**的（任何工具、任何人都可以这么命名），因此 ngm
//     **不删**它，只报出来 + 给出路（[ADR-030] 的决定）；
//   - `<repo>.git.clone-<hex>`：只有 ngm 会生成这个名字，所以它一定是我们自己建的，
//     删它没有争议。
//
// 边界如实写明：在这个根下、名字符合这个形状的目录会被当成我们的。**名字是唯一的
// 归属证据**——这是"不写跨进程锁"（[ADR-031]）的对价。
func isLandingDirName(name string) bool {
	i := strings.LastIndex(name, CloneLandingInfix)
	if i < 0 {
		return false
	}
	base, suffix := name[:i], name[i+len(CloneLandingInfix):]
	if !strings.HasSuffix(base, mirrorDirSuffix) || len(base) <= len(mirrorDirSuffix) {
		return false // 必须是"非空的 <X> + .git"的延伸
	}
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// landingDirs 列出 mirror 根下所有没落地的克隆目录（相对路径升序，顺序确定）。
//
// 遍历与 `repoDirs` 同一条路，但**跳过 `<repo>.git` 内部**：裸仓库里不会有落地目录，
// 而进去一趟只会让一次 usage 变慢（`objects/` 下可能是几万个文件）。
func (m *Mirror) landingDirs() ([]landingDir, error) {
	root := m.root
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read the mirror root", "", err)
	}
	var out []landingDir
	werr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || p == root {
			return nil
		}
		if strings.HasSuffix(d.Name(), mirrorDirSuffix) {
			return fs.SkipDir // 一个仓库的内部不是候选
		}
		if !isLandingDirName(d.Name()) {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		bytes, berr := dirBytes(p)
		if berr != nil {
			return berr
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		out = append(out, landingDir{
			rel:     filepath.ToSlash(rel),
			path:    p,
			bytes:   bytes,
			modTime: info.ModTime(),
		})
		return fs.SkipDir // 落地目录的内部不是候选
	})
	if werr != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "walk the mirror root", "", werr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

// MirrorPruneResult 描述一次 mirror 残骸回收（`ngm store prune` 的层 1 那半）。
type MirrorPruneResult struct {
	Root string
	// Removed 是被删掉的残骸**文件**（`objects/pack/tmp_*` 相对 mirror 根的斜杠路径），按名字升序。
	Removed []string
	// BytesFreed 是释放的字节数（dry-run 时是"将会释放"）。
	BytesFreed int64
	// LandingsRemoved / LandingBytesFreed 是被删掉的**没落地的克隆目录**（v0.101）。
	//
	// 与 Removed 分开报，因为它们是**另一种东西**：临时包是 git 的临时文件，
	// 落地目录是 ngm 自己建的目录。分开也让"删了 N 个文件"与"删了 N 个目录"
	// 在输出里各自可核对（一个混在一起的计数说不出删掉的是什么）。
	LandingsRemoved   []string
	LandingBytesFreed int64
	// KeptYoung / KeptYoungBytes 是"算残骸、但比门槛新"所以没删的东西——
	// **两类都算在内**（临时包与落地目录），因为它们被同一个东西保护着：
	// 一次正在进行中的克隆（mirror 层没有跨进程锁）。
	//
	// 报出来是**安全声明**：那是另一个进程可能正在写的东西，而不是"我们漏了"。
	KeptYoung      int
	KeptYoungBytes int64
	// Scanned 是扫到的 mirror 仓库数；Touched 是其中有残骸被清掉的仓库数。
	// 于是"没被动过的仓库"= Scanned - Touched，可以照原话说出来。
	Scanned int
	Touched int
	// OlderThan 是本次使用的年龄门槛。
	OlderThan time.Duration
	// DryRun 为 true 表示本次没有真的删除。
	DryRun bool
}

// PruneResidue 删掉 mirror 里**比门槛更旧**的临时包（`objects/pack/tmp_*`）。
//
// # 为什么这条删除是安全的
//
// 三类字节必须分清，否则"清理"会和"删内容"混起来：
//
//  1. **packfile / 松散对象**是内容，`PruneResidue` 绝不碰——层 1 没有
//     "按可达性删除"这回事（[ADR-030] 沿用 [ADR-018] 的判定）；
//  2. **`tmp_*` 是 git 自己的临时文件**：git 在任何时候都不引用它们
//     （`git count-objects -v` 把它们记成 `garbage`），因此删掉不会让任何
//     已记录的 commit 变得不可读；
//  3. 它们**只**由正在进行中的 clone/fetch 写入 ⇒ 唯一的风险是并发，
//     由年龄门槛处置（见 `MirrorResidueGrace`）。
//
// # 为什么不由 `git gc` 代劳
//
// 实测：`git gc` 会删 `objects/pack/tmp_*`，但**只删超过 14 天**的
// （13 天存活、15 天被清——阈值即 `gc.pruneExpire` 的默认值 `2.weeks`）。
// "中断一次克隆要等两周才拿回磁盘"不是一个可接受的出路，所以这条路径存在，
// 而 `git gc` 的那条留给 `Mirror.GC`（它管的是历史本体的整理）。
//
// 门槛必须为**正**：0 会让并发安全窗完全失效（与 `PruneOrphans` 拒绝 0 同理）。
func (m *Mirror) PruneResidue(olderThan time.Duration, now time.Time, dryRun bool) (MirrorPruneResult, error) {
	out := MirrorPruneResult{Root: m.root, OlderThan: olderThan, DryRun: dryRun}
	if olderThan <= 0 {
		return out, errs.New(errs.CodeConfigInvalid,
			"mirror residue grace must be positive (0 would remove the temporary pack of a clone that is running right now)",
			"use vendor.MirrorResidueGrace")
	}

	repos, err := m.repoDirs()
	if err != nil {
		return out, err
	}
	out.Scanned = len(repos)
	cutoff := now.Add(-olderThan)
	landings, err := m.landingDirs()
	if err != nil {
		return out, err
	}

	for _, rel := range repos {
		dir := filepath.Join(m.root, filepath.FromSlash(rel))
		res, rerr := residueFiles(dir)
		if rerr != nil {
			return out, rerr
		}
		removedHere := false
		for _, f := range res {
			if f.modTime.After(cutoff) {
				out.KeptYoung++
				out.KeptYoungBytes += f.bytes
				continue
			}
			out.Removed = append(out.Removed, rel+"/objects/pack/"+f.name)
			out.BytesFreed += f.bytes
			removedHere = true
			if dryRun {
				continue
			}
			if rmErr := os.Remove(f.path); rmErr != nil {
				return out, errs.Wrap(errs.CodeConfigInvalid,
					"remove mirror residue "+f.name, "check permissions on the mirror", rmErr)
			}
		}
		if removedHere {
			out.Touched++
		}
	}

	// ---- 没落地的克隆（v0.101 / ADR-031）----
	//
	// 与临时包共用**同一个门槛**：它们被同一个东西保护着（一次进行中的克隆）——
	// 两个门槛意味着"该用哪个"要在一处之外再决定一次。
	for _, l := range landings {
		if l.modTime.After(cutoff) {
			out.KeptYoung++
			out.KeptYoungBytes += l.bytes
			continue
		}
		out.LandingsRemoved = append(out.LandingsRemoved, l.rel)
		out.LandingBytesFreed += l.bytes
		if dryRun {
			continue
		}
		// `RemoveAll` 是全项目最重的一次删除，因此它的**前提**在这里再核一遍
		// （而不是只依赖遍历那一处）：名字的形状必须仍然是我们自己的落地目录。
		// 这是防"某天有人改了遍历、删除范围跟着悄悄变大"的第二道闸。
		if !isLandingDirName(filepath.Base(l.path)) {
			return out, errs.New(errs.CodeConfigInvalid,
				"refusing to remove "+l.rel+" — it is not a name ngm creates",
				"report this as a bug: the walk and the removal must agree")
		}
		if rmErr := os.RemoveAll(l.path); rmErr != nil {
			return out, errs.Wrap(errs.CodeConfigInvalid,
				"remove the unfinished clone "+l.rel, "check permissions on the mirror", rmErr)
		}
	}
	return out, nil
}

// MirrorGCResult 描述一次 mirror 整理（`ngm store gc`）。
type MirrorGCResult struct {
	Root string
	// Collected 是被 `git gc` 整理过的仓库（相对根的斜杠路径），升序。
	Collected []string
	// SkippedIncomplete / SkippedNotMirror 是**没被动过**的目录，分两类各说各的理由：
	//
	//   - `SkippedIncomplete`：形状是裸仓库、但**克隆没走完**（config 里没有镜像 refspec）。
	//     它不是"需要整理的历史"，它是一个没写完的仓库；对它跑 `git gc` 没有意义，
	//     而且它的失败会伪装成"整理失败了"；
	//   - `SkippedNotMirror`：连裸仓库都不是。mirror 根下不该有这种东西，
	//     但它是用户的磁盘——ngm 不删不属于自己的东西，只如实报出来。
	//
	// （v0.100 这一格叫 `SkippedNoRefs`，判据是"一个 ref 都没有"。v0.101 换成成因判据
	// [git.IsMirror]，理由见 `MirrorEntry.NotMirror`：**从空上游成功克隆**的 mirror
	// 也一个 ref 都没有，而它是正常的——旧判据会把一台好 mirror 跳过并说成废墟。）
	//
	// 两类**都必须出现在输出里**：一个"跳过了 N 个"的沉默，会让用户以为
	// 那些目录已经被整理过了。
	SkippedIncomplete []string
	SkippedNotMirror  []string
	// BytesBefore / BytesAfter 是该批 mirror 整理前后的字节合计。
	BytesBefore int64
	BytesAfter  int64
	// DryRun 为 true 表示本次没有真的跑 git。
	DryRun bool
}

// Reclaimed 是本次回收的字节。
//
// **可正可负**：`git gc` 用不同的 delta 窗口重打包时，理论上有产出略大包的余地。
// 负数是信息（"整理之后更大了"），不是错误——所以如实返回，不 clamp 到 0。
func (r MirrorGCResult) Reclaimed() int64 { return r.BytesBefore - r.BytesAfter }

// GC 对 mirror 逐个执行 `git gc`（历史本体的整理）。
//
// # 为什么这件事必须由 ngm 做（而不是留给用户）
//
// 层 1 是 `git clone --mirror` 的产物，每次 fetch 都会长出新对象；它的出路
// 一直写着"用户自己跑 git gc"（docs/internals/metrics.md 的两层分工表）。
// 那句话把"知道 `~/.ngm/mirror` 在哪、以及该跑什么"变成用户的前提条件——
// 而 `store usage` 已经把 mirror 的体积摆出来了，看得见却点不动是一种坏形状。
//
// # 边界
//
//   - **不触网**：`git gc` 只在本机对象库上工作，因此这里不做 `net:` 判定；
//   - **不删内容**：默认的 2 周不可达对象保留期照旧（见 `git.GC` 的说明）；
//   - **部分失败不伪装成成功**：某一个 mirror 上的 git 失败时，继续整理其余的，
//     然后把失败原样返回（`Result` 仍然有效）。"整理了一部分"必须能被读出来，
//     否则用户会以为磁盘已经收拾干净了。
func (m *Mirror) GC(ctx context.Context, dryRun bool) (MirrorGCResult, error) {
	out := MirrorGCResult{Root: m.root, DryRun: dryRun}
	repos, err := m.repoDirs()
	if err != nil {
		return out, err
	}

	var failures []string
	for _, rel := range repos {
		dir := filepath.Join(m.root, filepath.FromSlash(rel))
		switch {
		case !git.IsBareMirror(dir):
			// 只有这一层知道"什么才是一个能跑的 mirror"（快速路径，不起子进程）。
			out.SkippedNotMirror = append(out.SkippedNotMirror, rel)
			continue
		case !git.IsMirror(dir):
			out.SkippedIncomplete = append(out.SkippedIncomplete, rel)
			continue
		}

		before, berr := dirBytes(dir)
		if berr != nil {
			return out, berr
		}
		out.BytesBefore += before
		after := before
		if !dryRun {
			if gerr := git.GC(ctx, m.opts, dir); gerr != nil {
				failures = append(failures, rel+": "+gerr.Error())
				out.BytesAfter += before
				continue
			}
			if after, berr = dirBytes(dir); berr != nil {
				return out, berr
			}
		}
		out.BytesAfter += after
		out.Collected = append(out.Collected, rel)
	}

	if len(failures) > 0 {
		return out, errs.New(errs.CodeGitFetch,
			"git gc failed on "+strings.Join(failures, "; "),
			"run `git gc` in that mirror by hand to see git's own message; "+
				"the other mirrors were left collected")
	}
	return out, nil
}
