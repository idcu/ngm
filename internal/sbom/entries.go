// entries.go — 导出层的共同输入视图（v0.70 从 cmd 层搬进来）。
//
// SBOM 与 attestation 是两种出口，但它们的**输入**是同一份东西：
// lock 里每个依赖的 (slug, canonical, commit, archiveDigest, ref, subPath)。
// 这段映射此前在 `cmd/ngm/sbom.go` 里，v0.70 加 `ngm attest` 时搬到这里——
// **规则不写在第二个地方**（本项目的老纪律）。
package sbom

import (
	"sort"

	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/resolve"
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
		out = append(out, Entry{
			Slug:      d.Name,
			Ref:       d.Ref,
			RefType:   d.RefType,
			Commit:    d.Commit,
			Digest:    d.ArchiveDigest,
			Canonical: repo.String(),
			SubPath:   d.SubPath,
		})
	}
	return out, nil
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
