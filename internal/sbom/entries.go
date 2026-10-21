// entries.go — 导出层的共同输入视图（v0.70 从 cmd 层搬进来）。
//
// SBOM 与 attestation 是两种出口，但它们的**输入**是同一份东西：
// lock 里每个依赖的 (slug, canonical, commit, archiveDigest, ref, subPath)。
// 这段映射此前在 `cmd/ngm/sbom.go` 里，v0.70 加 `ngm attest` 时搬到这里——
// **规则不写在第二个地方**（本项目的老纪律）。
package sbom

import (
	"sort"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/resolve"
)

// FromLock 把 lock 条目映射为导出条目。
//
// canonical 由 slug 归一化而来（`github:x/dep` → `github.com/x/dep`）：
// purl 与 vcs URL 都需要它，而 lock 里存的只有 slug 与 vendorPath——
// 后者的根可能与 canonical 不同（global vendor 模式），因此**不用**它推导。
func FromLock(lf *lock.File) ([]Entry, error) {
	out := make([]Entry, 0, len(lf.Dependencies))
	for i := range lf.Dependencies {
		d := &lf.Dependencies[i]
		repo, err := resolve.ParseSlug(d.Name)
		if err != nil {
			return nil, err
		}
		// v0.81：digest 必须是**完整形态**才能进产物。
		//
		// lock 的 Validate 只校验 `sha256:` 前缀（原因见 lock.IsArchiveDigest 的注释：
		// 被篡改的 lock 必须是 exit 2 的完整性事件，不能在读取时被判成配置错误）。
		// 但导出层不同——这里要把 digest **写进产物**，一个空哈希会让
		// CycloneDX 输出 `content: ""`、attestation 写出 `sha256: ""`：
		// 结构完全合法，内容却是空的。而本包开头写着
		// "**没有哈希的 SBOM 只是清单**"。
		//
		// 位置选在 FromLock：它是 SBOM 与 attestation **共用**的唯一映射处，
		// 规则不写在第二个地方。
		contentDigest := d.ContentDigest()
		if !lock.IsArchiveDigest(contentDigest) {
			return nil, errs.New(errs.CodeConfigInvalid,
				"dependency "+d.Name+" has no usable content digest (got "+contentDigest+")",
				"an SBOM without hashes is only a list; re-run `ngm install` to regenerate the digest")
		}
		out = append(out, Entry{
			Slug:      d.Name,
			Ref:       d.Ref,
			RefType:   d.RefType,
			Commit:    d.Commit,
			Digest:    contentDigest,
			Canonical: repo.String(),
			SubPath:   d.SubPath,
			// v0.73：直接边随条目一起进导出层（依赖段的唯一来源）。
			RequiredBy: d.RequiredBy,
		})
	}
	return out, nil
}

// edgeIndex 把条目的直接边整理成"根的孩子"与"父 → 子们"两张表（v0.73，ADR-028）。
//
// 依赖段只有一个来源——lock 的 requiredBy。顺序在这里归一（排序 + 去重），
// 因此同一份 lock 导出同一份字节，**哪怕 lock 被人手改过顺序**
// （Validate 只保证引用存在，不保证顺序）。
//
// 根标记用 lock.RootRequirer：项目本身不是一个 Git 包，它在导出里的位置是
// "文档的根"，而不是一个组件。
//
// 两张表都存**条目 Key**（`name` 或 `name#subPath`），不是 purl：purl 是
// CycloneDX 的引用方式、SPDXID 是 SPDX 的，边本身与格式无关——
// 一份边、两种映射，才不会出现两棵树。
func edgeIndex(entries []Entry) (root []string, children map[string][]string) {
	known := make(map[string]bool, len(entries))
	for _, e := range entries {
		known[e.Key()] = true
	}
	children = map[string][]string{}
	for _, e := range entries {
		seen := map[string]bool{}
		for _, from := range e.RequiredBy {
			if seen[from] {
				continue
			}
			seen[from] = true
			if from == lock.RootRequirer {
				root = append(root, e.Key())
				continue
			}
			// 幽灵边由 lock.Validate 拒绝在**读取时**；这里只是防御性跳过——
			// 导出层不该在"已经读进来的 lock"上再判一次错（也不该静默编一条边出来）。
			if !known[from] {
				continue
			}
			children[from] = append(children[from], e.Key())
		}
	}
	sort.Strings(root)
	for k := range children {
		sort.Strings(children[k])
	}
	return root, children
}

// Sort 返回按 (slug, subPath) 排序的副本——"同一份 lock 导出两次字节相同"
// 这条纪律的唯一实现处。
//
// 为什么在导出器内部排序而不是信任调用方：两种出口的**所有** ID/引用
// 都派生自顺序，顺序不确定 ⇒ 整份文档不确定。
func Sort(entries []Entry) []Entry {
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Slug != sorted[j].Slug {
			return sorted[i].Slug < sorted[j].Slug
		}
		return sorted[i].SubPath < sorted[j].SubPath
	})
	return sorted
}
