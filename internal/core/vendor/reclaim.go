package vendor

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
)

// StoreReclaimItem 是 content store 中一个不可达的 digest。
type StoreReclaimItem struct {
	// Digest 是 `sha256:<hex>` 形式的存储键。
	Digest string
	// Layout 是 "v2"（清单 + blob 池）或 "v1"（遗留的整棵树）。
	Layout string
	// Bytes 是删除它**现在**能回收的字节（目录 + 只被候选树引用的旧 blob）。
	Bytes int64
}

// StoreReclaimPlan 是一次 content store 可达性扫描的结果。
//
// 只读：Plan 不删任何东西。真正的删除在 ApplyStoreReclaim。
type StoreReclaimPlan struct {
	Root string
	// Exists 为 false 表示 store 根还不存在（没有候选，也不是错误）。
	Exists bool
	// Items 按 digest 升序，保证输出可 diff。
	Items []StoreReclaimItem
	// TotalBytes 是**现在**能回收的字节合计（不含 YoungBlobBytes）。
	TotalBytes int64
	// YoungBlobs / YoungBlobBytes 是"已确定无人引用、但比安全窗新"的 blob。
	//
	// 它们不删：`Put` 先写 blob、后发布清单，因此刚写下的字节可能是另一个
	// 正在进行的 install。等它们变旧后重跑即可（或交给 `store prune --orphans`）。
	YoungBlobs     int
	YoungBlobBytes int64

	// blobPaths 是本次可安全删除的 blob 绝对路径（只在 Apply 内部使用）。
	blobPaths []string
}

// Empty 报告计划里有没有候选对象。
func (p StoreReclaimPlan) Empty() bool { return len(p.Items) == 0 }

// PlanStoreReclaim 标记可达 digest 之外的 store 条目。
//
// 可达性口径：`reachable` 是**当前项目 lock** 里所有依赖的 archiveDigest /
// moduleDigest。这不是"全机器所有项目"的可达性——ngm 没有项目注册表
// （ADR-018）。因此它只回答"这个项目还要不要"，不回答"别的项目还要不要"。
// 调用方必须把这一取舍写进输出（`ngm gc` 的帮助与提示就是这么做的）。
func (s *ContentStore) PlanStoreReclaim(reachable map[string]bool, now time.Time) (StoreReclaimPlan, error) {
	plan := StoreReclaimPlan{Root: s.root}
	if info, err := os.Stat(s.root); err != nil {
		if os.IsNotExist(err) {
			return plan, nil
		}
		return plan, errs.Wrap(errs.CodeConfigInvalid, "stat the content store", "", err)
	} else if !info.IsDir() {
		return plan, errs.New(errs.CodeConfigInvalid, s.root+" is not a directory", "")
	}
	plan.Exists = true

	scan, err := s.scanManifests()
	if err != nil {
		return plan, err
	}
	if scan.unreadable > 0 {
		// 与 PruneOrphans 同一条纪律：读不出的清单引用了什么是看不见的，
		// 那些 blob 会被误算成"只被候选树引用"。宁可什么都不删。
		return plan, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("%d content manifest(s) are unreadable; cannot tell which blobs are shared", scan.unreadable),
			"run `ngm store usage` to list them, then repair or delete those entries")
	}

	reachableBlobs := map[string]bool{}
	for dg := range reachable {
		for _, sha := range scan.treeBlobs[dg] {
			reachableBlobs[sha] = true
		}
	}

	// v2 候选：清单存在、但 digest 不在 lock 里。
	v2 := map[string]bool{}
	for dg := range scan.treeBlobs {
		if !reachable[dg] {
			v2[dg] = true
		}
	}
	// v1 候选：`content/sha256/<hex>` 目录存在且不在 lock 里。
	v1 := map[string]bool{}
	dirs, err := os.ReadDir(filepath.Join(s.root, digest.Algorithm))
	if err != nil && !os.IsNotExist(err) {
		return plan, errs.Wrap(errs.CodeConfigInvalid, "read the content store", "", err)
	}
	for _, e := range dirs {
		if !e.IsDir() || strings.HasPrefix(e.Name(), unpackPrefix) {
			continue
		}
		dg := digest.Algorithm + ":" + e.Name()
		if !reachable[dg] && !v2[dg] {
			v1[dg] = true
		}
	}

	all := make([]string, 0, len(v2)+len(v1))
	for dg := range v2 {
		all = append(all, dg)
	}
	for dg := range v1 {
		all = append(all, dg)
	}
	sort.Strings(all)

	cutoff := now.Add(-UnpackResidueGrace)
	seenBlob := map[string]bool{}
	for _, dg := range all {
		item := StoreReclaimItem{Digest: dg, Layout: "v1"}
		// v2 的清单目录与 digest 目录**都要**删：前者是 manifest/meta，
		// 后者可能留着一次成功迁移前的 v1 残留（v2 写入成功后旧树会被删，
		// 但中断或手工操作可能留下它）。
		dirsToSize := []string{s.PathForDigest(dg)}
		if v2[dg] {
			item.Layout = "v2"
			dirsToSize = append(dirsToSize, s.V2Dir(dg))
		}
		for _, dir := range dirsToSize {
			n, derr := dirBytesIfPresent(dir)
			if derr != nil {
				return plan, derr
			}
			item.Bytes += n
			plan.TotalBytes += n
		}

		for _, sha := range scan.treeBlobs[dg] {
			if reachableBlobs[sha] || seenBlob[sha] {
				continue
			}
			seenBlob[sha] = true
			blob := s.BlobPath(sha)
			if blob == "" {
				continue // 畸形 sha：BlobPath 已 fail-closed，这里也不去猜路径
			}
			info, serr := os.Stat(blob)
			if serr != nil {
				if os.IsNotExist(serr) {
					continue // 已经不在磁盘上：没有可回收的字节
				}
				return plan, errs.Wrap(errs.CodeConfigInvalid, "stat blob "+sha, "", serr)
			}
			if info.ModTime().After(cutoff) {
				plan.YoungBlobs++
				plan.YoungBlobBytes += info.Size()
				continue
			}
			plan.blobPaths = append(plan.blobPaths, blob)
			item.Bytes += info.Size()
			plan.TotalBytes += info.Size()
		}

		plan.Items = append(plan.Items, item)
	}
	return plan, nil
}

// StoreReclaimResult 描述一次真正的删除。
type StoreReclaimResult struct {
	Root string
	// Removed 是被删掉的 digest（升序）。
	Removed []string
	// BytesFreed 是实际释放的字节。
	BytesFreed int64
	// KeptYoung / KeptYoungBytes 与计划里的同名字段同义：没有删除、留给下次。
	KeptYoung      int
	KeptYoungBytes int64
	// Errors 是逐对象的删除失败；任何一条都不阻止其余对象继续删。
	Errors []string
}

// ApplyStoreReclaim 删除计划里的 digest 与可回收 blob。
//
// 删除失败**汇总**而不是中途退出：一个被占用/权限不足的对象不该让其余
// 已经能回收的字节留在磁盘上。调用方据 Errors 决定退出码。
func (s *ContentStore) ApplyStoreReclaim(plan StoreReclaimPlan) StoreReclaimResult {
	res := StoreReclaimResult{
		Root:           plan.Root,
		KeptYoung:      plan.YoungBlobs,
		KeptYoungBytes: plan.YoungBlobBytes,
	}
	for _, item := range plan.Items {
		dirs := []string{s.PathForDigest(item.Digest)}
		if item.Layout == "v2" {
			dirs = append(dirs, s.V2Dir(item.Digest))
		}
		var freed int64
		ok := true
		for _, dir := range dirs {
			n, err := dirBytesIfPresent(dir)
			if err != nil {
				res.Errors = append(res.Errors, dir+": "+err.Error())
				ok = false
				continue
			}
			if n == 0 {
				if _, serr := os.Stat(dir); os.IsNotExist(serr) {
					continue
				}
			}
			if rerr := os.RemoveAll(dir); rerr != nil {
				res.Errors = append(res.Errors, dir+": "+rerr.Error())
				ok = false
				continue
			}
			freed += n
		}
		if ok {
			res.Removed = append(res.Removed, item.Digest)
			res.BytesFreed += freed
		}
	}
	for _, blob := range plan.blobPaths {
		info, err := os.Stat(blob)
		if err != nil {
			if !os.IsNotExist(err) {
				res.Errors = append(res.Errors, blob+": "+err.Error())
			}
			continue
		}
		if rerr := os.Remove(blob); rerr != nil {
			res.Errors = append(res.Errors, blob+": "+rerr.Error())
			continue
		}
		res.BytesFreed += info.Size()
	}
	return res
}

// dirBytesIfPresent 是 dirBytes 的"不存在即 0"版本（候选对象可能只有清单或只有树）。
func dirBytesIfPresent(dir string) (int64, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, errs.Wrap(errs.CodeConfigInvalid, "stat "+dir, "", err)
	}
	if !info.IsDir() {
		return 0, nil
	}
	return dirBytes(dir)
}

// VendorReclaimItem 是 vendor 树里一个不可达的路径。
type VendorReclaimItem struct {
	// Path 是相对 vendor 根的斜杠路径。
	Path string
	// Bytes 是磁盘占用（目录递归求和）。
	Bytes int64
}

// VendorReclaimPlan 是一次 vendor 可达性扫描的结果。
type VendorReclaimPlan struct {
	Root   string
	Exists bool
	// Items 按 Path 升序。
	Items      []VendorReclaimItem
	TotalBytes int64
}

// Empty 报告计划里有没有候选对象。
func (p VendorReclaimPlan) Empty() bool { return len(p.Items) == 0 }

// PlanVendorReclaim 找出 vendor 根下不被 reachable 命中的子树。
//
// reachable 是**当前 lock 的 vendorPath（含 subPath）**。判定按"前缀"而不是
// 精确相等：`github.com/org` 是 `github.com/org/repo` 的容器，必须保留并继续
// 向下看；只有整棵子树都没有任何可达路径经过时，才把该子树算作候选。
//
// 这条前缀规则是安全关键：少了它，一个 monorepo（可达路径是
// `github.com/org/repo/packages/core`）会连同兄弟目录一起被删掉。
func PlanVendorReclaim(root string, reachable []string) (VendorReclaimPlan, error) {
	plan := VendorReclaimPlan{Root: root}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return plan, nil
		}
		return plan, errs.Wrap(errs.CodeConfigInvalid, "stat the vendor root", "", err)
	}
	if !info.IsDir() {
		return plan, errs.New(errs.CodeConfigInvalid, root+" is not a directory", "")
	}
	plan.Exists = true

	keep := map[string]bool{}
	for _, rel := range reachable {
		rel = strings.Trim(strings.TrimSpace(filepath.ToSlash(rel)), "/")
		if rel == "" || rel == "." {
			continue
		}
		keep[rel] = true
	}

	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		entries, rerr := os.ReadDir(dir)
		if rerr != nil {
			return errs.Wrap(errs.CodeConfigInvalid, "read "+dir, "", rerr)
		}
		for _, e := range entries {
			childRel := path.Join(rel, e.Name())
			if rel == "" {
				childRel = e.Name()
			}
			if keep[childRel] {
				// 精确命中：整棵子树都是可达的，不再向下判断。
				continue
			}
			if prefixOfReachable(childRel, keep) {
				// 可达路径更深：这是容器，保留并继续向下。
				if e.IsDir() {
					if werr := walk(filepath.Join(dir, e.Name()), childRel); werr != nil {
						return werr
					}
				}
				continue
			}
			// 整棵子树没有任何可达路径经过 → 候选。
			full := filepath.Join(dir, e.Name())
			var n int64
			if e.IsDir() {
				n, rerr = dirBytes(full)
				if rerr != nil {
					return rerr
				}
			} else {
				li, ierr := e.Info()
				if ierr != nil {
					return errs.Wrap(errs.CodeConfigInvalid, "stat "+full, "", ierr)
				}
				n = li.Size()
			}
			plan.Items = append(plan.Items, VendorReclaimItem{Path: childRel, Bytes: n})
			plan.TotalBytes += n
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return plan, err
	}
	sort.Slice(plan.Items, func(i, j int) bool { return plan.Items[i].Path < plan.Items[j].Path })
	return plan, nil
}

// prefixOfReachable 报告 rel 是否是某个可达路径的严格前缀（按路径分量比较）。
func prefixOfReachable(rel string, keep map[string]bool) bool {
	for k := range keep {
		if strings.HasPrefix(k, rel+"/") {
			return true
		}
	}
	return false
}
