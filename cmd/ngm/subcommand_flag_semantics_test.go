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

// flagsetBlock 是一个 flagset 声明块：从 `newFlagSet("name"` 到下一个声明之前。
type flagsetBlock struct {
	// name 是声明时的名字，例如 `typecheck` / `css` / `store prune`。
	name string
	// flags 是这一块里声明的**布尔** flag（按出现顺序，去重）。
	flags []string
}

var (
	reFlagSetDecl = regexp.MustCompile(`newFlagSet\("([^"]+)"`)
	reBoolDecl    = regexp.MustCompile(`fs\.Bool\("([a-z][a-z-]*)"`)
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
		for _, m := range reBoolDecl.FindAllStringSubmatch(body, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				b.flags = append(b.flags, m[1])
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

	known := map[string]bool{}
	for _, c := range commands {
		known[c.Name] = true
	}

	checked := 0
	for _, file := range sourceFiles(t) {
		for _, b := range flagsetBlocksOf(t, file) {
			// 只认**子命令层**：名字里有空格，且第一段是真命令。
			parts := strings.Fields(b.name)
			if len(parts) < 2 || !known[parts[0]] || len(b.flags) == 0 {
				continue
			}
			sub := parts
			for _, flag := range b.flags {
				t.Run(b.name+"/--"+flag, func(t *testing.T) {
					run := func(t *testing.T, extra ...string) (int, string) {
						t.Helper()
						home := isolateUserEnv(t)
						args := append(append([]string{}, sub...), extra...)
						c, out := runCaptureCode(t, args...)
						// 把这一次的隔离 home 归一化掉。`store prune` 会印 content store 的
						// **绝对路径**（`no unpack residue in <path> (nothing to do)`），
						// 而两次 run 各自 isolate 到**不同的** temp 目录——不归一化的话，
						// 判据会把"路径不同"当成"flag 不等价"（v0.45 实测到的第一处红）。
						out = strings.ReplaceAll(out, home, "<home>")
						out = strings.ReplaceAll(out, filepath.ToSlash(home), "<home>")
						return c, out
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
	for _, c := range commands {
		for _, f := range boolFlagsOfCommand(t, c.Name) {
			covered[c.Name+" --"+f] = true
		}
	}
	for _, file := range sourceFiles(t) {
		for _, b := range flagsetBlocksOf(t, file) {
			if len(b.flags) == 0 {
				continue
			}
			parts := strings.Fields(b.name)
			if len(parts) >= 2 && known[parts[0]] {
				for _, f := range b.flags {
					covered[b.name+" --"+f] = true
				}
				continue
			}
			for _, f := range b.flags {
				if !covered[parts[0]+" --"+f] {
					unowned++
					t.Errorf("the flagset %q (%s) contributes `--%s`, which neither net reaches — "+
						"it would be silently unchecked", b.name, file, f)
				}
			}
		}
	}
	t.Logf("subcommand-layer flags: %d pair(s) × 4 assertions; unowned flagsets: %d", checked, unowned)
}
