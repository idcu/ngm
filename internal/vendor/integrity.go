package vendor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/idcu/ngm/internal/errs"
)

// VerifyResult 是一次 vendor ↔ content 一致性校验的结果。
type VerifyResult struct {
	// Files 是校验通过的文件数。
	Files int
	// Symlinks 是校验通过的 symlink 数。
	Symlinks int
	// Mismatches 是差异描述（空表示完全一致）。
	Mismatches []string
}

// OK 报告是否一致。
func (r VerifyResult) OK() bool { return len(r.Mismatches) == 0 }

// VerifyVendorTree 逐文件校验 vendor 落地树与 content store 的内容树是否一致。
//
// **全量内容哈希，非抽样**（development/v0.1-plan.md M4 验收要求）。
// 这是 `ngm verify --deep` 使用的深度校验；默认的廉价校验见 VerifyVendorTreeShallow。
//
// 校验项：
//
//  1. 两侧的相对路径集合完全相同（多一个、少一个都算不一致）
//  2. 普通文件的 sha256 相同（内容与字节一致）
//  3. symlink 的目标字符串相同
//  4. 目录结构一致（由路径集合隐含）
//
// 不做 mode 比对：hardlink 共享 inode 时 mode 天然一致；copy 模式由 copyFile
// 保留权限位。若 mode 成为问题，会在 --deep 校验中单独报告。
//
// 本函数供 `ngm verify` 复用（M5），因此它只依赖两个目录路径、不依赖 lock。
func VerifyVendorTree(vendorTree, contentTree string) (VerifyResult, error) {
	return verifyTrees(vendorTree, contentTree, true)
}

// VerifyVendorTreeShallow 是 VerifyVendorTree 的**廉价版本**：只比对结构与元数据，
// 不读取文件内容。
//
// 与深校验的分工（architecture/observability.md §ngm verify 的"检查层次与开关"）：
//
//	默认（浅）  路径集合 + symlink 目标 + 普通文件大小 → 检出缺失/多余/结构错位
//	--deep     逐文件 sha256                        → 追加检出等长字节篡改
//
// 为什么默认不做内容哈希：verify 是 CI 门禁，浅校验的代价与树规模无关（只 stat），
// 而全量哈希需要把整个 vendor 树读一遍。等长篡改由 `--deep` 覆盖。
func VerifyVendorTreeShallow(vendorTree, contentTree string) (VerifyResult, error) {
	return verifyTrees(vendorTree, contentTree, false)
}

// verifyTrees 是浅/深校验的共同实现。
//
// deep=false 时用「大小相同」代替「内容哈希」；其余判定完全一致，
// 因此两个入口不会出现"一个检出了某类问题、另一个却检不出"的漂移。
func verifyTrees(vendorTree, contentTree string, deep bool) (VerifyResult, error) {
	var res VerifyResult

	left, err := scanTree(vendorTree)
	if err != nil {
		return res, err
	}
	right, err := scanTree(contentTree)
	if err != nil {
		return res, err
	}

	// 1) 路径集合比对
	for path := range left {
		if _, ok := right[path]; !ok {
			res.Mismatches = append(res.Mismatches,
				fmt.Sprintf("%s: present in vendor but missing from the content store", path))
		}
	}
	for path := range right {
		if _, ok := left[path]; !ok {
			res.Mismatches = append(res.Mismatches,
				fmt.Sprintf("%s: present in the content store but missing from vendor", path))
		}
	}

	// 2) 逐个共同路径比对
	common := make([]string, 0, len(left))
	for path := range left {
		if _, ok := right[path]; ok {
			common = append(common, path)
		}
	}
	sort.Strings(common)

	// 3) 逐个共同路径比对。
	//
	// 深校验（deep=true）**逐文件读两侧内容**，因此这一段是 `--deep` 的全部成本，
	// 而它此前是串行的（v0.2 复盘 §2.3 挂账项）。依赖级并发已经拿走了主要收益，
	// 但"一个依赖内部文件很多"时剩下的就是这一段。
	//
	// 并行不改变结论：`Mismatches` 末尾统一排序（下方 sort.Strings），
	// 计数按索引求和，错误取**索引最小**的那条——与串行版本"第一条命中即返回"一致，
	// 否则同一个仓库在不同机器上会报出不同的文件。
	//
	// 浅校验（deep=false）不做内容读取，这一段只是遍历内存里的 map，**不并行**：
	// 为它起 goroutine 的调度成本比它本身还大。
	type outcome struct {
		mismatch string
		symlink  bool
		file     bool
		err      error
	}

	results := make([]outcome, len(common))
	compareOne := func(i int) {
		path := common[i]
		a := left[path]
		b := right[path]

		if a.symlink || b.symlink {
			if a.symlink != b.symlink {
				results[i].mismatch = fmt.Sprintf("%s: one side is a symlink and the other is not", path)
				return
			}
			if a.linkTarget != b.linkTarget {
				results[i].mismatch = fmt.Sprintf("%s: symlink target differs (%q vs %q)",
					path, a.linkTarget, b.linkTarget)
				return
			}
			results[i].symlink = true
			return
		}

		// 长度不同 → 无需读内容即可判定（浅校验的主要检出能力）
		if a.size != b.size {
			results[i].mismatch = fmt.Sprintf("%s: size differs (%d vs %d bytes)", path, a.size, b.size)
			return
		}

		if !deep {
			results[i].file = true
			return
		}

		sumA, herr := hashFile(a.full)
		if herr != nil {
			results[i].err = errs.Wrap(errs.CodeConfigInvalid, "hash vendor file "+path, "", herr)
			return
		}
		sumB, herr := hashFile(b.full)
		if herr != nil {
			results[i].err = errs.Wrap(errs.CodeConfigInvalid, "hash content file "+path, "", herr)
			return
		}
		if sumA != sumB {
			results[i].mismatch = fmt.Sprintf("%s: content differs (sha256 %s vs %s)",
				path, sumA[:12], sumB[:12])
			return
		}
		results[i].file = true
	}

	if deep && len(common) > 1 {
		workers := runtime.GOMAXPROCS(0)
		if workers > len(common) {
			workers = len(common)
		}
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(start int) {
				defer wg.Done()
				for i := start; i < len(common); i += workers {
					compareOne(i)
				}
			}(w)
		}
		wg.Wait()
	} else {
		for i := range common {
			compareOne(i)
		}
	}

	// 合并：必须按索引顺序，错误才与串行版本一致（最小索引优先）
	for i := range results {
		if results[i].err != nil {
			return res, results[i].err
		}
		if results[i].mismatch != "" {
			res.Mismatches = append(res.Mismatches, results[i].mismatch)
			continue
		}
		if results[i].symlink {
			res.Symlinks++
		}
		if results[i].file {
			res.Files++
		}
	}

	sort.Strings(res.Mismatches)
	return res, nil
}

// treeEntry 是扫描到的单个条目。
type treeEntry struct {
	full       string
	symlink    bool
	linkTarget string
	// size 是普通文件的字节数。symlink 时是链接目标字符串的长度（不参与比对）。
	//
	// 用途：浅校验据此检出"长度不同"的篡改，无需读文件内容。
	size int64
}

// scanTree 递归扫描目录，返回 相对路径 → 条目 的映射。
//
// 相对路径统一用 `/` 分隔（与清单口径一致），因此跨平台可比。
func scanTree(root string) (map[string]treeEntry, error) {
	out := map[string]treeEntry{}

	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil // 不存在的目录视为空树，由调用方比对后报告差异
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "stat "+root, "", err)
	}
	if !info.IsDir() {
		return nil, errs.New(errs.CodeConfigInvalid, root+" is not a directory", "")
	}

	err = filepath.Walk(root, func(path string, fi os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if fi.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		key := filepath.ToSlash(rel)

		entry := treeEntry{full: path, size: fi.Size()}
		if fi.Mode()&os.ModeSymlink != 0 {
			target, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			entry.symlink = true
			entry.linkTarget = target
		}
		out[key] = entry
		return nil
	})
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "scan "+root, "", err)
	}
	return out, nil
}

// hashFile 返回文件内容的 sha256 小写十六进制。
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SameFile 报告两个路径是否指向同一个 inode（hardlink 的快速判定）。
//
// 用途：诊断输出可以据此告诉用户"这一项确实是 hardlink 而非副本"；
// 内容一致性仍以 VerifyVendorTree 的哈希比对为准。
func SameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

// VendorPathFor 返回某依赖在 vendor 中的**相对路径**（`/` 分隔）。
//
// 规则：canonical 路径 + 可选 monorepo 子路径。
// monorepo 只落地子目录（与 mappings 的 `to` 语义一致：`to` 指向实际被消费的目录）。
func VendorPathFor(canonicalPath, subPath string) string {
	base := strings.Trim(filepath.ToSlash(canonicalPath), "/")
	sub := strings.Trim(filepath.ToSlash(subPath), "/")
	if sub == "" {
		return base
	}
	return base + "/" + sub
}
