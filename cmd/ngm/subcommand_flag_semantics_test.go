package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.45：**子命令层的 flag**——把 v0.43 里"公开跳过"的那几对真的测掉。
//
// 起点是 v0.43 报出的 3 处跳过：
//
//	store --dry-run · store --orphans   ← 真的是**子命令层**（`store prune` 的）
//	typecheck --minify                  ← **不是**：`--minify` 是 `css` 的，
//	                                      而 `typecheck.go` 这个文件里住着**两个命令**
//
// 第二条是这一版最有价值的读数：它不是"粒度不够"，是**我的派生规则太粗**。
// v0.43 按"整个文件里的 `fs.Bool`"派生，于是同一个文件里第二个命令的 flag
// 被算到了第一个命令头上，运行时当然报 "flag provided but not defined"，
// 而那句报错被当成了"它属于某个子命令"的证据跳过了。
// **跳过名单里躺着的不是产品的东西，是我的误差。**
//
// 于是这一版做两件事：
//
//	① 派生改成**按 flagset 块**（`newFlagSet("name"` 到下一个声明之前）——
//	   `typecheck.go` 于是分成 `typecheck` 与 `css` 两块，各归各的；
//	② 名字里**带空格**的块（`store prune`）由这张网按 `<cmd> <sub> --flag` 来测，
//	   四条断言与 v0.43 共用（`checkBoolFlagSemantics`）——两层用**同一套判据**，
//	   于是它们不可能各自漂移。
//
// 归零的读数：v0.43 的公开跳过 **3 → 0**，而两层合起来覆盖**全部**派生出的布尔 flag
// （由 §守卫 里那条"并集"断言钉住）。

// flagDecl 是一条 flag 声明：名字 + 声明的种类。
//
// 为什么要带种类（v0.46）：同一套派生要喂两张判据不同的网——
// **布尔 flag**（v0.43 / v0.45：`=true`/`=false`/`=x`/不吞 token）与
// **取值 flag**（v0.42：空值必须"与不给等价"或"明确失败"）。
// 种类来自声明本身（`fs.Bool` / `fs.String` / `fs.Duration` / `fs.Int`），不是我记的。
type flagDecl struct {
	name string
	kind string // "Bool" / "String" / "Duration" / "Int"
}

// flagsetBlock 是一个 flagset 声明块：从 `newFlagSet("name"` 到下一个声明之前。
type flagsetBlock struct {
	// name 是声明时的名字，例如 `typecheck` / `css` / `store prune`。
	name string
	// decls 是这一块里声明的 flag（按出现顺序，同名去重）。
	decls []flagDecl
}

// boolFlags 返回这一块里的布尔 flag 名（v0.43 / v0.45 用）。
func (b flagsetBlock) boolFlags() []string {
	var out []string
	for _, d := range b.decls {
		if d.kind == "Bool" {
			out = append(out, d.name)
		}
	}
	return out
}

// valueFlags 返回这一块里的**取值** flag 名（v0.42 用）：
// String / Duration / Int —— 也就是"要吃掉一个值"的那些。
func (b flagsetBlock) valueFlags() []string {
	var out []string
	for _, d := range b.decls {
		if d.kind != "Bool" {
			out = append(out, d.name)
		}
	}
	return out
}

// allFlags 返回这一块里的全部 flag 名（不分种类）——覆盖守卫用。
func (b flagsetBlock) allFlags() []string {
	var out []string
	for _, d := range b.decls {
		out = append(out, d.name)
	}
	return out
}

// subcommandBoolPairs 是**这张网实际会跑**的表：名字带空格、第一段是真命令、有布尔 flag。
//
// 它被抽出来是为了让 v0.46 的覆盖守卫调它——守卫要按**三张网各自的选择函数**
// 拼出覆盖集，而不是自己再写一套规则（v0.45 的第一版守卫就是这样放过了 `css`：
// 它核对的是名字，不是覆盖路径）。
func subcommandBoolPairs(t *testing.T) []flagsetBlock {
	t.Helper()
	known := map[string]bool{}
	for _, c := range commands {
		known[c.Name] = true
	}
	var out []flagsetBlock
	for _, file := range sourceFiles(t) {
		for _, b := range flagsetBlocksOf(t, file) {
			parts := strings.Fields(b.name)
			if len(parts) < 2 || !known[parts[0]] || len(b.boolFlags()) == 0 {
				continue
			}
			out = append(out, b)
		}
	}
	return out
}

var (
	reFlagSetDecl = regexp.MustCompile(`newFlagSet\("([^"]+)"`)
	reFlagDecl    = regexp.MustCompile(`fs\.(Bool|String|Duration|Int)\("([a-z][a-z-]*)"`)
)

// flagsetBlocksOf 把 `cmd/ngm/<file>` 拆成 flagset 块。
//
// 推导方式刻意保持"文本 + 块边界"这种粗粒度：它要挡的是**派生规则与源码脱节**
// （v0.43 那个坑），而不是要做一个 Go 解析器。块边界就是下一个 `newFlagSet(`。
func flagsetBlocksOf(t *testing.T, file string) []flagsetBlock {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "cmd", "ngm", file))
	if err != nil {
		return nil
	}
	src := string(data)
	locs := reFlagSetDecl.FindAllStringSubmatchIndex(src, -1)

	var out []flagsetBlock
	for i, loc := range locs {
		end := len(src)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		body := src[loc[1]:end]
		b := flagsetBlock{name: src[loc[2]:loc[3]]}
		seen := map[string]bool{}
		for _, m := range reFlagDecl.FindAllStringSubmatch(body, -1) {
			if !seen[m[2]] {
				seen[m[2]] = true
				b.decls = append(b.decls, flagDecl{name: m[2], kind: m[1]})
			}
		}
		out = append(out, b)
	}
	return out
}

// sourceFiles 列出 cmd/ngm 下的非测试 Go 文件（派生的输入面）。
func sourceFiles(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(repoRoot(t), "cmd", "ngm"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, n)
	}
	return out
}

// checkBoolFlagSemantics 是 v0.43 与 v0.45 **共用**的四条断言。
//
// 共用是刻意的：两层（命令层 / 子命令层）若各写一份，两份判据迟早会漂移
// ——而"两个地方对同一件事有两套标准"本身就是这类项目最容易长出来的病。
func checkBoolFlagSemantics(t *testing.T, label, flag string, run func(t *testing.T, extra ...string) (int, string)) {
	t.Helper()

	plain, oPlain := run(t, "--"+flag)          // --flag
	yes, oYes := run(t, "--"+flag+"=true")      // --flag=true
	off, oOff := run(t, "--"+flag+"=false")     // --flag=false
	base, oBase := run(t)                       // 不给
	bad, oBad := run(t, "--"+flag+"=x")         // --flag=x
	junk, oJunk := run(t, "--"+flag, "ZZ-JUNK") // 布尔 flag 后的 token

	if plain != yes || oPlain != oYes {
		t.Errorf("`--%s=true` 与 `--%s` 不等价（exit %d vs %d）:\n  %s\n  %s",
			flag, flag, yes, plain, firstLine(oYes), firstLine(oPlain))
	}
	if off != base || oOff != oBase {
		t.Errorf("`--%s=false` 与**不给**它不等价（exit %d vs %d）:\n  %s\n  %s",
			flag, off, base, firstLine(oOff), firstLine(oBase))
	}
	if bad == 0 {
		t.Errorf("`--%s=x` 被接受了（exit 0）——Go 说那不是合法布尔值，"+
			"静默当成 true 是最坏的处置:\n  %s", flag, firstLine(oBad))
	}
	if junk == 0 {
		t.Errorf("`--%s ZZ-JUNK` 退 0 —— 那个 token 被当成了 flag 的值**被吞掉**了，"+
			"而布尔 flag 从不消费下一个 token（normalizeArgs 的注释专门写过这件事）:\n  %s",
			flag, firstLine(oJunk))
	}
	t.Logf("%-16s --%-12s ①✓ ②%s ③exit=%d ④exit=%d", label, flag,
		map[bool]string{true: "✓", false: "✗"}[off == base && oOff == oBase], bad, junk)
}

func TestV45SubcommandFlagsHoldTheSameSemantics(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	checked := 0
	for _, b := range subcommandBoolPairs(t) {
		{
			// 只认**子命令层**：名字里有空格，且第一段是真命令（选择规则在
			// `subcommandBoolPairs` 里，守卫调的是同一个函数）。
			sub := strings.Fields(b.name)
			for _, flag := range b.boolFlags() {
				t.Run(b.name+"/--"+flag, func(t *testing.T) {
					run := func(t *testing.T, extra ...string) (int, string) {
						t.Helper()
						home := isolateUserEnv(t)
						args := append(append([]string{}, sub...), extra...)
						c, out := runCaptureCode(t, args...)
						// 把这一次的隔离 home 归一化掉（v0.45 实测到的第一处红：
						// `store prune` 会印 content store 的**绝对路径**）。
						// v0.51 起统一走共享助手——它同时处理 JSON 的转义形态。
						return c, normalizeRunPaths(t, out, home)
					}
					checkBoolFlagSemantics(t, b.name, flag, run)
				})
				checked++
			}
		}
	}

	// 守卫① **规模**：子命令层至少要有这一版实测到的 2 对。
	// 写成下限而不是等号：源码里再长出子命令 flag 时，这张网应当**自动多测**，
	// 而不是先红一次（那是 v0.43 结构性守卫的教训：别把阈值拍成当前值）。
	if checked < 2 {
		t.Fatalf("only %d subcommand-layer flag pair(s) were exercised — "+
			"either the derivation broke or the subcommand flagsets moved", checked)
	}

	// 守卫② **覆盖并集**：两张网合起来必须覆盖**全部**派生出的布尔 flag。
	//
	// 这条是这一版真正的结构性守卫，而且它刻意走**与两张网同一条路**去找，
	// 不是核对名字——第一版就是核对名字（"块名等于某个命令名 ⇒ v0.43 覆盖"），
	// 于是它**没抓到**那个更大的洞：`css` 住在 `typecheck.go` 里，没有 `css.go`，
	// 于是 v0.43 一条也派生不出来——而按名字看，`css` 这个块"看起来"是被覆盖的。
	//
	// 现在：先把"两张网会跑的 (命令, flag)"集合算出来，再要求每个块里的每个 flag
	// 都落在这个集合里。落不进去就是**没人看的东西**。
	unowned := 0
	covered := map[string]bool{}
	// 命令层：布尔归 v0.43、取值归 v0.42 —— 两处都调**它们自己的**选择函数。
	for _, c := range commands {
		for _, f := range boolFlagsOfCommand(t, c.Name) {
			covered[c.Name+" --"+f] = true
		}
	}
	// 取值一侧（v0.46）：直接调 v0.42 那张网的选择函数——它现在**跨两层**
	//（命令层 + 子命令层，于是 `store prune --older-than` 也在里面）。
	for _, p := range valueFlagPairs(t) {
		covered[p.block+" --"+p.flag] = true
	}
	// 子命令层：布尔归这张网——同样调它自己的选择函数。
	for _, b := range subcommandBoolPairs(t) {
		for _, f := range b.boolFlags() {
			covered[b.name+" --"+f] = true
		}
	}

	for _, file := range sourceFiles(t) {
		for _, b := range flagsetBlocksOf(t, file) {
			parts := strings.Fields(b.name)
			if len(parts) == 0 {
				continue
			}
			// 一个 flag 只要被**任一条路**覆盖就算数：命令层用 `<命令> --flag` 这个键，
			// 子命令层用 `<命令> <子命令> --flag`。
			//
			// 这里刻意**不**写"名字带空格 ⇒ 一定被覆盖"那种捷径——v0.45 的第一版
			// 就是这么错的（它按名字判断，于是放过了 `css`）。判断依据只有
			// 上面那三个**选择函数**给出的集合。
			for _, f := range b.allFlags() {
				if !covered[parts[0]+" --"+f] && !covered[b.name+" --"+f] {
					unowned++
					t.Errorf("the flagset %q (%s) contributes `--%s`, which no net reaches — "+
						"it would be silently unchecked", b.name, file, f)
				}
			}
		}
	}
	t.Logf("subcommand-layer flags: %d pair(s) × 4 assertions; unowned flagsets: %d", checked, unowned)
}
