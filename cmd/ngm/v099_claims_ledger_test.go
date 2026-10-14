package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.99：**写在注释里的"两边必须一致"必须进账本**。
//
// 起因是 v0.97/v0.98/v0.99 一路撞到同一件事：仓库里散着一些"逐字镜像""必须一致"
// 的承诺，而**承诺自己不会检查自己**——v0.99 当场抓到两处：`wasmEngine.entryArgs`
// 把 `entry.Args` 前置了两遍（注释正说着"两者必须一致"），
// `mappings.lockFileName` 的作者写着"由 CLI 的集成测试保证"（而常量没导出 ⇒ 做不到）。
//
// 这条判据把那些承诺**变成账本上的条目**：每一处必须写明
//   - 被哪条判据对账（判据名必须真实存在——v0.53 的老教训：登记不能看着空气），或
//   - 为什么验不了（缺环境 / 是说明文字 / 已经合并为单实现）。
//
// 记账口径（写在判据里，因为它决定了这条网守得住什么）：
//   - 词表是**六个人工选定的词**（见 claimWords）：`逐字镜像` · `逐字对齐` ·
//     `必须一致` · `必须保持` · `两份实现` · `两条实现`。改词表是显式动作。
//   - 扫描范围：Go 的**非测试**源码 + `scripts/*.sh|*.mjs`。
//     `.ps1` 不在范围内：它们按约定只转调同名 `.sh`（那份约定由 v0.90 的参数对齐网
//     与实跑网守着），因此它们的文件头重复同一句话并不构成新的承诺。
//   - **它是筛子不是证明**：挡住的是"从没被登记过"这一档；"登记的判据真的管用"
//     由那些判据自己的牙齿负责（每一条都在各自的复盘里）。
func TestV99MirroringClaimsAreAccounted(t *testing.T) {
	root := repoRoot(t)
	hits := claimHits(t, root)

	// 每一处命中必须被**恰好一条**登记项认领；每条登记项也必须认领到命中。
	claimed := make([]bool, len(hits))
	for _, e := range claimsLedger {
		found := false
		for i, h := range hits {
			if h.file == e.file && strings.Contains(h.text, e.phrase) {
				claimed[i] = true
				found = true
			}
		}
		if !found {
			t.Errorf("登记项 %s 「%s」没有认领到任何命中——**登记看着空气**（改过措辞？删过那行？）",
				e.file, e.phrase)
		}
		if e.net == "" && e.why == "" {
			t.Errorf("登记项 %s 「%s」既没写判据也没写理由", e.file, e.phrase)
		}
		if e.net != "" && e.why != "" {
			t.Errorf("登记项 %s 「%s」同时写了判据与理由——二者只取其一（理由意味着没网）", e.file, e.phrase)
		}
		if e.net != "" && !testFunctionExists(t, root, e.net) {
			t.Errorf("登记项 %s 「%s」指的判据 %s **在这个仓库里不存在**（改名了？还是想写但没写？）",
				e.file, e.phrase, e.net)
		}
	}

	for i, h := range hits {
		if claimed[i] {
			continue
		}
		t.Errorf("未登记的承诺：%s:%d\n    %s\n  它说「两边必须一致」，但没有任何判据对账它——"+
			"补一条网，或者写进 claimsLedger 说明为什么验不了", h.file, h.line, h.text)
	}
}

// claimWords 是账本覆盖的六个词。**改它就是在改这条网的语义**，所以它是一份显式数据。
var claimWords = []string{"逐字镜像", "逐字对齐", "必须一致", "必须保持", "两份实现", "两条实现"}

type claimHit struct {
	file string
	line int
	text string
}

type claimEntry struct {
	file   string // 相对仓库根
	phrase string // 该行必须包含的锚点
	net    string // 覆盖它的判据名（必须存在），或 "" 表示验不了
	why    string // net == "" 时必填
}

// claimsLedger：v0.99 清点出的每一处"两边必须一致"。
var claimsLedger = []claimEntry{
	{
		file: "internal/adapter/subprocess.go", phrase: "两者必须一致",
		net: "TestV99EntryArgsArePrependedExactlyOnce",
	},
	{
		file: "internal/resolve/normalize.go", phrase: "且必须保持同步",
		net: "TestV99HostMapsAreInverse",
	},
	{
		file: "internal/sbom/spdx.go", phrase: "两种格式的依赖图形必须一致",
		net: "TestV99SBOMFormatsDescribeTheSameGraph",
	},
	{
		file: "internal/supplychain/allowlist.go", phrase: "段数必须一致",
		net: "TestMatchRepoPattern_StarDoesNotCrossSlash",
	},
	{
		// v0.82 已把两份实现合并为一份（两个调用点都转调 resolve.ValidateSubPath）。
		file: "internal/resolve/addspec.go", phrase: "两份实现不一致的问题由此消失",
		why: "v0.82 已合并为单实现（addspec 与 config 都转调 resolve.ValidateSubPath）——没有第二方，无可对账",
	},
	{
		file: "internal/resolve/subpath.go", phrase: "这条规则此前有**两份实现**",
		why: "同上：这两行注释记录的正是那次合并（规则现在只有一处定义）",
	},
	{
		file: "scripts/check-docs-links.sh", phrase: "为了让两条实现同口径",
		net: "TestV097LinkImplementationsAgree",
	},
	{
		file: "scripts/check-library-standard.sh", phrase: "exports 的解析**逐字对齐**",
		net: "TestV87EntryInferenceAgreesBetweenProductAndChecker",
	},
	{
		file: "scripts/check-library-standard.sh", phrase: "rel_path 逐字镜像",
		net: "TestV98EntryPathsAreNormalised",
	},
	{
		file: "scripts/check-library-standard.sh", phrase: "如果只是话说得好听",
		net: "TestV87EntryInferenceAgreesBetweenProductAndChecker",
	},
	{
		// `.ps1` 只转调同名 `.sh`（这条约定是脚本自己的文件头在重复声明）。
		file: "scripts/check-gitee-release-status.sh", phrase: "两份实现会漂移",
		net: "TestV90WrapperOptionsParity",
	},
	{
		file: "scripts/check-library-standard.sh", phrase: "两份实现会漂移",
		net: "TestV90WrapperOptionsParity",
	},
	{
		// 探针比对工具：输入是三份探针结果（CI 里由 repro-probe.yml 造），本地没有夹具。
		file: "scripts/compare-probe-results.mjs", phrase: "引擎版本必须一致",
		why: "它是 CI 里 repro-probe.yml 的一步（输入是三份探针结果，本地没有那种夹具）；它比对的是**报告**，不是产品行为",
	},
	{
		file: "scripts/compare-probe-results.mjs", phrase: "产物哈希必须一致",
		why: "同上（同一个脚本的第 4 层判定）",
	},
}

// claimHits 扫出词表覆盖的每一处：Go 非测试源码 + `scripts/*.sh|*.mjs`。
func claimHits(t *testing.T, root string) []claimHit {
	t.Helper()
	var files []string
	walk := func(dir string, keep func(string) bool) {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return nil
			}
			if keep(filepath.ToSlash(rel)) {
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
	}
	walk("internal", func(rel string) bool {
		return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
	})
	walk("cmd", func(rel string) bool {
		return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
	})
	walk("scripts", func(rel string) bool {
		return strings.HasSuffix(rel, ".sh") || strings.HasSuffix(rel, ".mjs")
	})

	if len(files) < 50 {
		t.Fatalf("只扫到 %d 个文件——扫描范围坏掉时这条网会静默地什么都不守", len(files))
	}

	var hits []claimHit
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			for _, w := range claimWords {
				if strings.Contains(line, w) {
					hits = append(hits, claimHit{file: rel, line: i + 1, text: strings.TrimSpace(line)})
					break
				}
			}
		}
	}
	if len(hits) == 0 {
		t.Fatal("一处承诺都没扫到——词表或扫描范围坏了（这条网会假装全绿）")
	}
	return hits
}

// testFunctionExists 在整个仓库的测试文件里找 `func <name>(`。
func testFunctionExists(t *testing.T, root, name string) bool {
	t.Helper()
	needle := "func " + name + "("
	found := false
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		if strings.Contains(string(raw), needle) {
			found = true
		}
		return nil
	})
	return found
}
