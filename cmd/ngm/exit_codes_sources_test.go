package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// v0.57 立、v0.59 扩：**退出码的来源必须有据可查**。
//
// v0.57 管的是码 1：那时量出"关于码 1 有哪些来源"三处说法互不相同
// （`errs.go` 的注释说 3 个、V24 的手写关键词表说 4 个、源码里 20+ 处站点），
// 而那张关键词表只有**单向**——代码多一处来源不会红，靠人记得。
//
// v0.59 动作更大：按 **ADR-026** 把"内部失败"分了出去（**码 6**），
// 于是这条判据的宇宙也从一个码变成**两个码**：
//
//	码 1（策略失败）  ← 提到 `CodeRefDrift`，或字面量 `return 1` / `exit = 1`
//	码 6（内部失败）  ← 提到 `CodeInternal`
//
// 判据的形状（沿用 v0.56 那套，第三次现形）：
//
//	① **从源码派生**站点，按 `文件 :: 所在函数` 作键；
//	② 每个派生站点都必须在下面的登记里（**漏站即红**——这是从前缺的那一半）；
//	③ 登记里每个条目都必须在派生集合里（悬挂即红）；
//	④ 每个 **source** 类站点都要归到一个 category，而 category 说它属于哪个码
//	   （`categoryCodes`）；**每个码的每一类**都必须在 observability.md
//	   **那一行**里被写到（码 1 的那四类不许跑到码 6 的说明里去）。
//
// 键是 `文件 :: 函数`，值是一**条列表**：一个条目对应**一处站点**。
// 于是"同一函数里多加一处"也躲不过——**列表长度必须等于该函数里的站点数**
// （这条是 v0.57 的牙齿 A 提醒我加的：只按函数名核对的话，在已登记的函数里再写一个
// `return 1` 是看不见的，而"多一处说不出去"正是要防的那类漂移）。
var reExitCodeSite = regexp.MustCompile(`(return 1\b|exit\s*=\s*1\b|CodeRefDrift|CodeInternal)`)

// categoryCodes 是**类别 → 退出码**的唯一定义处（ADR-026 的决定就写在这里）。
//
// 分类不是装饰：它决定"这一类别该出现在哪个码的说明里"。
var categoryCodes = map[string]int{
	catDrift:    1,
	catEngine:   1,
	catHook:     1,
	catVuln:     1,
	catInternal: 6,
}

// exitOneRole 是一个站点在这件事里的角色。
type exitOneRole string

const (
	// roleSource：它**能让进程以 1 退出**——这是"来源"。
	roleSource exitOneRole = "source"
	// roleDefinition：它**定义**这个码（数值或名字），不是来源。
	roleDefinition exitOneRole = "definition"
	// roleHelper：它返回 `1`，但那不是退出码（例如各种 rank）。
	roleHelper exitOneRole = "helper"
)

type exitOneEntry struct {
	role     exitOneRole
	category string // roleSource 时必填：文档必须写到的那一类
	// tokenCode 只在 **非 source** 条目上填：这一条解释的那个 token 提到的是哪个码
	// （例如 `CodeRefDrift Code = 1` 那一行提到的是码 1）。
	//
	// 为什么要它：判据不只要能数"这个函数里有几处"，还要能查"**每一处退的是哪个码**"
	// ——否则把某处 `errs.CodeInternal.ExitCode()` 改回字面量 `return 1`
	// 是数不出来的（匹配数一样）。这一版实测到的洞。
	tokenCode int
	why       string
}

// 文档里的类别名（observability.md 相应的那一行必须逐类写到）。
const (
	catDrift    = "策略漂移"
	catEngine   = "引擎运行失败"
	catHook     = "审计钩子否决或超时"
	catVuln     = "漏洞超阈值"
	catInternal = "内部失败"
)

var exitOneSiteRegistry = map[string][]exitOneEntry{
	// ---- definitions：这个码本身（不是"来源"）----
	"internal/errs/errs.go :: (top-level)": {
		{
			role: roleDefinition, tokenCode: 1, why: "`CodeRefDrift Code = 1`——数值的唯一定义处",
		},
		{
			role: roleDefinition, tokenCode: 6, why: "`CodeInternal Code = 6`——码 6 的数值定义处（v0.59 · ADR-026）",
		},
	},
	"internal/errs/errs.go :: String": {
		{
			role: roleDefinition, tokenCode: 1, why: "`case CodeRefDrift:`——显示名映射",
		},
		{
			role: roleDefinition, tokenCode: 6, why: "`case CodeInternal:`——同上（v0.59 新增码 6）",
		},
	},
	"internal/errs/errs.go :: ExitCode": {{
		role: roleSource, category: catInternal,
		why: "**非 `NgmError` 的兜底**：错误没走 ngm 的错误模型 ⇒ 码 6（ADR-026）——" +
			"从前它退 1，于是 CI 会把「有错误逃过了错误模型」读成「上游漂移」",
	}},

	// ---- helpers：返回 1 但不是退出码 ----
	"internal/verify/report.go :: Rank": {{
		role: roleHelper, tokenCode: 1, why: "漂移分类的**排序权重**，返回值不进退出码",
	}},
	"cmd/ngm/verify_signatures.go :: signatureRank": {{
		role: roleHelper, tokenCode: 1, why: "签名状态的**排序权重**，同上",
	}},

	// ---- sources：策略类（文档本来就该写的那几类）----
	"internal/verify/report.go :: ExitCode": {
		{
			role: roleSource, category: catDrift,
			why: "verify 的漂移判定汇到这里——`expected` 之外都退 1；`why` / `outdated` 那些间接路径也是同一个出口（两处：两种判定各一条 `return errs.CodeRefDrift.ExitCode()`）",
		},
		{
			role: roleSource, category: catDrift,
			why: "同上：`critical` 与 `unexpected` 各占一处（同一函数、两处站点）",
		},
	},
	"internal/adapter/engine.go :: AsNgmError": {{
		role: roleSource, category: catEngine,
		why: "引擎跑了但失败 ⇒ 包成 CodeRefDrift（引擎自己的退出码留在消息里，不透传）",
	}},
	"internal/adapter/runner.go :: runChain": {{
		role: roleSource, category: catEngine,
		why: "链式调用里引擎失败 ⇒ `code = errs.CodeRefDrift`",
	}},
	"cmd/ngm/audit_hook.go :: auditHookVerdict": {
		{
			role: roleSource, category: catHook,
			why: "钩子**否决** ⇒ 码 1",
		},
		{
			role: roleSource, category: catHook,
			why: "钩子**超时** ⇒ 码 1（与否决同一函数、两处站点）",
		},
	},
	"cmd/ngm/tree.go :: runTree": {
		{
			role: roleSource, category: catVuln,
			why: "`--osv` 查到漏洞 ⇒ `exit = 1`（同时给 remediation 那行）",
		},
		{
			role: roleSource, category: catInternal,
			why: "`write report: %v`——同一函数里的第二处：报告写不出去",
		},
	},

	// ---- sources：**内部失败**（v0.57 量出来的那一类；文档此前完全没提）----
	//
	// 这些站点同一句话：命令已经跑完、结论也算出来了，但**说不出去**——
	// 写 stdout 失败 / 读 stdin 失败 / JSON 编码失败。它们与"策略失败"是两件事，
	// 却共用同一个数字；而文档的语义列只写"策略失败"，
	// 读文档的人因此会以为码 1 一定是策略问题。
	"cmd/ngm/root.go :: dispatch": {{
		role: roleSource, category: catInternal,
		why: "**人读通道的写失败**（v0.60）：`dispatch` 是唯一入口，包装里记账 stdout——" +
			"命令本来成功（码 0）而写失败 ⇒ 码 6。从前人读路径忽略写失败（`fmt.Fprintf` " +
			"的返回值被丢掉），于是「报告写了一半却报成功」✗；JSON 路径一直检查 ✓",
	}},
	"cmd/ngm/root.go :: runWithRecovery": {{
		role: roleSource, category: catInternal,
		why: "**捕获 panic** ⇒ `exit = 1`（这一处连 `CodeRefDrift` 都不提，只写了字面量 1——" +
			"从前扫描只看这个名字时，它整个是不可见的）",
	}},
	"cmd/ngm/verify.go :: runVerify": {{
		role: roleSource, category: catInternal, why: "`write report: %v`——JSON 报告写不出去",
	}},
	"cmd/ngm/audit.go :: runAudit": {{
		role: roleSource, category: catInternal, why: "`write report: %v`",
	}},
	"cmd/ngm/why.go :: runWhy": {{
		role: roleSource, category: catInternal, why: "`write report: %v`",
	}},
	"cmd/ngm/outdated.go :: runOutdated": {{
		role: roleSource, category: catInternal, why: "`write report: %v`",
	}},
	"cmd/ngm/build.go :: runBuild": {{
		role: roleSource, category: catInternal, why: "`write bundle: %v`",
	}},
	"cmd/ngm/typecheck.go :: runCSS": {{
		role: roleSource, category: catInternal, why: "`write css: %v`",
	}},
	"cmd/ngm/transform.go :: runTransform": {
		{
			role: roleSource, category: catInternal,
			why: "`read stdin: %v`——管道输入读不出来",
		},
		{
			role: roleSource, category: catInternal,
			why: "`write output: %v`——变换结果写不出去",
		},
	},
	"cmd/ngm/engines.go :: runEnginesList": {{
		role: roleSource, category: catInternal, why: "`write listing: %v`",
	}},
	"cmd/ngm/engines.go :: writeJSON": {
		{
			role: roleSource, category: catInternal,
			why: "`marshal json: %v`（engines 的 info / validate 共用这一个出口）",
		},
		{
			role: roleSource, category: catInternal,
			why: "`write json: %v`——同一函数的第二处",
		},
	},
	"cmd/ngm/integrations.go :: writeIntegrationsJSON": {
		{
			role: roleSource, category: catInternal, why: "`encode report: %v`",
		},
		{
			role: roleSource, category: catInternal, why: "`write report: %v`——同一函数的第二处",
		},
	},
}

func TestV57ExitCodeSourcesAreRegistered(t *testing.T) {
	sites := exitCodeSitesFromSource(t)
	total := 0
	for _, codes := range sites {
		total += len(codes)
	}
	if total == 0 {
		t.Fatal("一个站点都没扫到——这条判据的范围缩到零了")
	}

	// ① + ② 派生 ⊆ 登记：新增一个让进程退 1 的地方 ⇒ 红
	var unregistered []string
	for key := range sites {
		if _, ok := exitOneSiteRegistry[key]; !ok {
			unregistered = append(unregistered, key)
		}
	}
	sort.Strings(unregistered)
	for _, key := range unregistered {
		t.Errorf("`%s` 能让进程以 **1** 退出，但它不在这条判据的登记里——\n"+
			"要么给它一个 category（文档里的码 1 那一行必须写到那一类），"+
			"要么写明它是 definition/helper", key)
	}

	// ③ 登记 ⊆ 派生，**并且逐函数对账数目与码**：
	// 一个条目对应一处站点，所以"同一函数里多加一处"也必须被发现；
	// 而"某处换了码"（例如把 `errs.CodeInternal.ExitCode()` 改回字面量 `return 1`）
	// 只对数目是不够的——两处的**码**也要一一对上。
	for key, entries := range exitOneSiteRegistry {
		got, ok := sites[key]
		if !ok {
			t.Errorf("登记里的 `%s` 在源码里找不到对应站点——登记指向空气（改名或删掉了？）", key)
			continue
		}
		if len(got) != len(entries) {
			t.Errorf("`%s` 里现在有 **%d** 处站点，而登记写着 %d 条——\n"+
				"同一函数里多出（或少了）一处也是漂移：每一处都该有自己的 category 与 why",
				key, len(got), len(entries))
			continue
		}
		want := make([]int, 0, len(entries))
		for _, e := range entries {
			if e.role != roleSource {
				want = append(want, e.tokenCode)
				continue
			}
			code, ok := categoryCodes[e.category]
			if !ok {
				t.Errorf("`%s` 的 source 条目没有可用的 category（%q）——它属于哪个码无从判断",
					key, e.category)
				continue
			}
			want = append(want, code)
		}
		gotSorted, wantSorted := append([]int{}, got...), append([]int{}, want...)
		sort.Ints(gotSorted)
		sort.Ints(wantSorted)
		if !intsEqual(gotSorted, wantSorted) {
			t.Errorf("`%s` 的站点**退的码对不上**：源码里是 %v，登记说 %v——\n"+
				"换了码（内部失败 ⇄ 策略失败）是语义改动，必须在登记里说明白",
				key, gotSorted, wantSorted)
		}
	}

	// ④ source ⊆ 文档：**每一类**都要出现在**它那个码**的那一行里
	obs := observabilityExitCodes(t)
	sources, empty := 0, 0
	byCode := map[int][]string{} // 码 → 它的类别（排序后）
	seenCat := map[string]bool{}
	for _, entries := range exitOneSiteRegistry {
		for _, e := range entries {
			if e.role != roleSource {
				continue
			}
			sources++
			if strings.TrimSpace(e.category) == "" {
				empty++
				continue
			}
			code, ok := categoryCodes[e.category]
			if !ok {
				t.Errorf("类别 %q 没有对应的退出码——`categoryCodes` 里要写清它属于哪个码",
					e.category)
				continue
			}
			if !seenCat[e.category] {
				seenCat[e.category] = true
				byCode[code] = append(byCode[code], e.category)
			}
		}
	}
	if empty > 0 {
		t.Errorf("有 %d 个 source 类条目没写 category——它归哪一类，正是这条判据要查的东西", empty)
	}
	if len(byCode) == 0 {
		t.Fatal("没有任何 source 类条目——这条判据没有可核对的对象")
	}
	var codes []int
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	for _, code := range codes {
		row, ok := obs[code]
		if !ok {
			t.Errorf("observability.md 里没有码 %d 那一行——这条判据没有可核对的对象", code)
			continue
		}
		cats := byCode[code]
		sort.Strings(cats)
		for _, cat := range cats {
			if !strings.Contains(row.sources, cat) {
				t.Errorf("码 %d 有一类来源是 %q（源码里真实存在），而 observability.md 的码 %d "+
					"那一行没有写到它——\n读文档的人会以为这类失败与我无关：\n%s",
					code, cat, code, row.sources)
			}
		}
	}
	if sources < 5 {
		t.Errorf("只登记了 %d 个 source——实测远不止这些，这条判据的范围缩了", sources)
	}

	t.Logf("exit codes: %d site(s) in %d function(s) · %d source function(s) · codes %v",
		total, len(sites), sources, codes)
}

// exitCodeSitesFromSource 扫出**能让进程以 1 退出**的站点，键为 `文件 :: 所在函数`，
// 值是**该函数里的站点数**（登记表按同数目条目写）。
//
// 扫两个地方：`cmd/ngm`（CLI 自己）与 `internal`（库层里构造错误或直接设码的地方）。
// 跳过测试文件与 testdata；**跳过纯注释行**——注释里提到 `CodeRefDrift` 不是站点
// （`root.go` 那条 panic 从前就是只在注释里提了一句，而代码本身写的是字面量 1）。
func exitCodeSitesFromSource(t *testing.T) map[string][]int {
	t.Helper()
	out := map[string][]int{}

	scanRoot := func(root string) {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == "testdata" || d.Name() == ".git" {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			fset := token.NewFileSet()
			af, perr := parser.ParseFile(fset, path, src, parser.ParseComments)
			if perr != nil {
				return nil
			}
			lines := strings.Split(string(src), "\n")
			for _, m := range reExitCodeSite.FindAllStringSubmatchIndex(string(src), -1) {
				pos := fset.Position(token.Pos(m[0] + 1))
				if pos.Line < 1 || pos.Line > len(lines) {
					continue
				}
				if strings.HasPrefix(strings.TrimSpace(lines[pos.Line-1]), "//") {
					continue
				}
				fn := "(top-level)"
				for _, decl := range af.Decls {
					fd, ok := decl.(*ast.FuncDecl)
					if !ok {
						continue
					}
					if fd.Pos() <= token.Pos(m[0]+1) && token.Pos(m[0]+1) <= fd.End() {
						fn = fd.Name.Name
						break
					}
				}
				// 这一处提的是哪个码：`CodeInternal` ⇒ 6，其余（`CodeRefDrift`
				// 与字面量 `return 1` / `exit = 1`）⇒ 1。
				matched := string(src[m[0]:m[1]])
				code := 1
				if strings.Contains(matched, "CodeInternal") {
					code = 6
				}
				key := repoRelative(t, path) + " :: " + fn
				out[key] = append(out[key], code)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	scanRoot(".")
	scanRoot("../../internal")
	return out
}

// intsEqual 比较两个**已排序**的 int 切片（用来对账"这一组站点退的是哪些码"）。
func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// repoRelative 把扫描时用的路径归一成"仓库根起算"的样子，登记表按它写。
func repoRelative(t *testing.T, path string) string {
	t.Helper()
	slash := filepath.ToSlash(path)
	slash = strings.TrimPrefix(slash, "../../")
	slash = strings.TrimPrefix(slash, "./")
	if !strings.HasPrefix(slash, "cmd/") && !strings.HasPrefix(slash, "internal/") {
		slash = "cmd/ngm/" + slash
	}
	return slash
}
