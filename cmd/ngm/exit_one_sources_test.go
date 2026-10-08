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

// v0.57：**码 1 的来源必须有据可查**（把 V24 那条单向关键词表改成派生 + 双向）。
//
// 起点是一个量出来的事实：关于"码 1 有哪些来源"，三处说法**互不相同**——
//
//	· `internal/errs/errs.go` 的注释说 **3 个**（漂移 · 漏洞 · 引擎）
//	· V24 的手写关键词表说 **4 个**（漂移 · 引擎 · 漏洞 · 钩子）
//	· 源码里**能让进程以 1 退出的站点**扫出来是 **20+ 处**
//
// 而那张关键词表只有**单向**：文档少一个词会红，**代码多一处来源不会红**。
// 它自己的注释写着"哪天真出现第五种来源，加进这里的同时也必须写进文档"——
// 也就是**靠人记得**，而这一条正是本版要拿掉的东西。
//
// 判据的形状（沿用 v0.56 那套，第三次现形）：
//
//	① **从源码派生**站点：谁能让进程以 1 退出（字面量 `return 1` / `exit = 1`，
//	   或提到 `CodeRefDrift`），按 `文件 :: 所在函数` 作键；
//	② 每个派生站点都必须在下面的登记里（**漏站即红**——这是从前缺的那一半）；
//	③ 登记里每个条目都必须在派生集合里（悬挂即红）；
//	④ 每个 **source** 类站点都要归到一个 category，而每个 category
//	   都必须在 observability.md 的码 1 那一行里被写到。
//
// 键是 `文件 :: 函数`，值是一**条列表**：一个条目对应**一处站点**。
// 于是"同一函数里多加一处"也躲不过——**列表长度必须等于该函数里的站点数**
// （这条是牙齿 A 提醒我加的：只按函数名核对的话，在已登记的函数里再写一个
// `return 1` 是看不见的，而"多一处说不出去"正是本版要防的那类漂移）。
var reExitOneSite = regexp.MustCompile(`(return 1\b|exit\s*=\s*1\b|CodeRefDrift)`)

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
	why      string
}

// 文档里的类别名（observability.md 的码 1 那一行必须逐类写到）。
const (
	catDrift     = "策略漂移"
	catEngine    = "引擎运行失败"
	catHook      = "审计钩子否决或超时"
	catVuln      = "漏洞超阈值"
	catInternal  = "内部失败"
	catExitOneNM = "内部失败" // 与 catInternal 同一个类别（避免散落的字面量）
)

var exitOneSiteRegistry = map[string][]exitOneEntry{
	// ---- definitions：这个码本身（不是"来源"）----
	"internal/errs/errs.go :: (top-level)": {{
		role: roleDefinition, why: "`CodeRefDrift Code = 1`——数值的唯一定义处",
	}},
	"internal/errs/errs.go :: String": {{
		role: roleDefinition, why: "`case CodeRefDrift:`——显示名映射",
	}},
	"internal/errs/errs.go :: ExitCode": {{
		role: roleDefinition, why: "`return 1`——码到退出码的映射",
	}},

	// ---- helpers：返回 1 但不是退出码 ----
	"internal/verify/report.go :: Rank": {{
		role: roleHelper, why: "漂移分类的**排序权重**，返回值不进退出码",
	}},
	"cmd/ngm/verify_signatures.go :: signatureRank": {{
		role: roleHelper, why: "签名状态的**排序权重**，同上",
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

func TestV57ExitOneSourcesAreRegistered(t *testing.T) {
	sites := exitOneSitesFromSource(t)
	total := 0
	for _, n := range sites {
		total += n
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

	// ③ 登记 ⊆ 派生，**并且逐函数对账条目数**：
	// 一个条目对应一处站点，所以"同一函数里多加一处"也必须被发现。
	for key, entries := range exitOneSiteRegistry {
		n, ok := sites[key]
		if !ok {
			t.Errorf("登记里的 `%s` 在源码里找不到对应站点——登记指向空气（改名或删掉了？）", key)
			continue
		}
		if n != len(entries) {
			t.Errorf("`%s` 里现在有 **%d** 处站点，而登记写着 %d 条——\n"+
				"同一函数里多出（或少了）一处也是漂移：每一处都该有自己的 category 与 why",
				key, n, len(entries))
		}
	}

	// ④ source ⊆ 文档：每个 category 都必须在 observability.md 的码 1 行里出现
	obs := observabilityExitCodes(t)
	row, ok := obs[1]
	if !ok {
		t.Fatal("observability.md 里没有码 1 那一行——这条判据没有可核对的对象")
	}
	sources, empty := 0, 0
	categories := map[string]bool{}
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
			categories[e.category] = true
		}
	}
	if empty > 0 {
		t.Errorf("有 %d 个 source 类条目没写 category——它归哪一类，正是这条判据要查的东西", empty)
	}
	if len(categories) == 0 {
		t.Fatal("没有任何 source 类条目——这条判据没有可核对的对象")
	}
	var cats []string
	for c := range categories {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	for _, cat := range cats {
		if !strings.Contains(row.sources, cat) {
			t.Errorf("码 1 有一类来源是 %q（源码里真实存在），而 observability.md 的码 1 "+
				"那一行没有写到它——\n读文档的人会以为这类失败与我无关：\n%s", cat, row.sources)
		}
	}
	if sources < 5 {
		t.Errorf("只登记了 %d 个 source——实测远不止这些，这条判据的范围缩了", sources)
	}

	t.Logf("exit code 1: %d site(s) in %d function(s) · %d source function(s) · %d categor(y|ies) [%s]",
		total, len(sites), sources, len(cats), strings.Join(cats, " / "))
}

// exitOneSitesFromSource 扫出**能让进程以 1 退出**的站点，键为 `文件 :: 所在函数`，
// 值是**该函数里的站点数**（登记表按同数目条目写）。
//
// 扫两个地方：`cmd/ngm`（CLI 自己）与 `internal`（库层里构造错误或直接设码的地方）。
// 跳过测试文件与 testdata；**跳过纯注释行**——注释里提到 `CodeRefDrift` 不是站点
// （`root.go` 那条 panic 从前就是只在注释里提了一句，而代码本身写的是字面量 1）。
func exitOneSitesFromSource(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}

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
			for _, m := range reExitOneSite.FindAllStringSubmatchIndex(string(src), -1) {
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
				out[repoRelative(t, path)+" :: "+fn]++
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
