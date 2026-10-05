package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.37：**枚举，而不是列举。**
//
// v0.36 的扫描覆盖了 57 个用例，但那些用例来自两张**为别的目的建的表**
// （退出码对账、错误面）。真正的"全称"该由**枚举**给出：
//
//	每一个命令 × 每一种配置错误 ⇒ 走上哪条输出通道，就守哪条通道的契约
//
//	错误文本 ⇒ 必须带 `hint:`
//	报告     ⇒ 必须带行动行
//	用法文本 ⇒ **不许出现**（配置形状下打用法 = 矩阵没走到配置层）
//	静默     ⇒ 红（退出非零却什么都不说）
//
// 好处有三：矩阵是**生成的**（不依赖我记得哪些命令会怎样）；
// 分类是**穷尽**的（没有"其它"桶可躲）；而且这些形状**都不需要夹具**，跑起来很便宜。
//
// 为什么"用法文本不许出现"值得当成红：它恰好抓住两类事——
// ① 矩阵对某个命令**给不出能到达配置层的参数**（我的表该补）；
// ② 某个命令**悄悄变成了全局命令**（不接受 `--dir`），而名单没跟上。
// 两种情况都该有人看一眼，而不是被静默容忍。
func TestV37EveryCommandUnderEveryConfigErrorUsesItsChannel(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// 表的**完整性**先立住：每个命令都必须有名字（参数可以是空），
	// 否则"某个命令不在矩阵里"这件事本身没人会发现。
	if len(matrixArgs) != len(commands) {
		t.Fatalf("the argument table has %d entries but there are %d commands — "+
			"a command outside the matrix is exactly what this net exists to prevent",
			len(matrixArgs), len(commands))
	}
	for _, spec := range commands {
		if _, ok := matrixArgs[spec.Name]; !ok {
			t.Errorf("command %q has no entry in the argument table — add one (it may be empty)", spec.Name)
		}
	}

	shapes := []struct {
		name string
		dir  func(t *testing.T) string
	}{
		{"dir-missing", func(t *testing.T) string { return filepath.Join(t.TempDir(), "nope") }},
		{"no-manifest", func(t *testing.T) string { return t.TempDir() }},
		{"manifest-broken", func(t *testing.T) string {
			d := t.TempDir()
			if err := os.WriteFile(filepath.Join(d, "ngm.json"), []byte("{"), 0o644); err != nil {
				t.Fatal(err)
			}
			return d
		}},
	}

	counts := map[string]int{}
	runs := 0

	for _, spec := range commands {
		for _, sh := range shapes {
			runs++
			t.Run(spec.Name+"/"+sh.name, func(t *testing.T) {
				isolateUserEnv(t)
				args := append([]string{spec.Name}, matrixArgs[spec.Name]...)
				if !globalCommands[spec.Name] {
					args = append(args, "--dir="+sh.dir(t))
				}

				var out, errb bytes.Buffer
				code := dispatch(args, &out, &errb)
				stderr, text := errb.String(), out.String()+errb.String()
				body := reportBody(text)

				switch {
				case strings.Contains(text, "USAGE:"):
					// 注意读的是**未截断**的 `text`：`reportBody` 恰恰会在 "USAGE:" 处截断，
					// 于是用它去找用法文本永远找不到——初版正是这么写的，
					// 这一格只能掉进"其它"桶里被兜住（判据的另一条腿替它报了警）。
					// 配置形状下打用法 = 矩阵没走到配置层（见文件头注释）。
					t.Errorf("this command printed its usage instead of reaching the configuration layer — "+
						"either the argument table cannot reach it, or it stopped accepting --dir:\n%s",
						firstLine(text))
				case code == 0:
					counts["ok"]++
				case strings.TrimSpace(text) == "":
					counts["silent"]++
					t.Errorf("exited %d without saying anything — a silent failure is the hardest kind to debug", code)
				case reErrPrefix.MatchString(stderr):
					counts["error-text"]++
					if !strings.Contains(stderr, "hint:") {
						t.Errorf("this error gives no next step:\n%s", stderr)
					}
				case strings.Contains(body, failMark):
					counts["report"]++
					if len(reActionLine.FindAllString(body, -1)) == 0 {
						t.Errorf("this failing report lists items (`%s`) but says nothing about what to do next:\n%s",
							failMark, text)
					}
				default:
					counts["unclassified"]++
					t.Errorf("this output fits none of the known channels (error text / report / usage / silent) — "+
						"a new channel is a decision someone has to make consciously:\n%s", firstLine(text))
				}
			})
		}
	}

	// 可达性守卫：矩阵必须真的走到配置层。
	// 若哪天所有命令都退 0，下面这些计数会归零——那时这张网看起来还是绿的，
	// 但它已经什么都不查了。
	if counts["error-text"] < 40 {
		t.Fatalf("only %d of %d runs produced error text (%v) — the matrix stopped reaching the configuration layer",
			counts["error-text"], runs, counts)
	}
	if counts["unclassified"] != 0 {
		t.Fatalf("%d run(s) fell outside every known channel (%v)", counts["unclassified"], counts)
	}
	t.Logf("config matrix: %d runs over %d commands × %d shapes → %v",
		runs, len(commands), len(shapes), counts)
}

// matrixArgs 给每个命令一份"语法上够用"的参数，好让它走到**配置层**。
//
// 每个命令都必须有名字（可以是空的切片）——`TestV37` 第一段就断言这件事。
// 它只是"怎么到达配置层"的桥：没有它，需要位置参数的命令会先打用法文本。
var matrixArgs = map[string][]string{
	"init":         {"github.com:x/app"},
	"add":          {"github:x/dep@v1", "--ref-type=tag"},
	"install":      {},
	"update":       {"--all"},
	"remove":       {"github:x/dep"},
	"verify":       {},
	"audit":        {},
	"why":          {"github:x/dep"},
	"tree":         {},
	"outdated":     {},
	"typecheck":    {"--engine=fake"},
	"typedecl":     {"--outdir=types", "--engine=fake"},
	"build":        {"entry.ts", "--engine=fake"},
	"transform":    {"src.ts", "--loader=ts", "--engine=fake"},
	"css":          {"a.css", "--engine=fake"},
	"mappings":     {"validate"},
	"integrations": {"add", "vite"},
	"cache":        {"clean"},
	"store":        {"usage"},
	"config":       {"validate"},
	"engines":      {"list"},
}

// globalCommands 是不接受 `--dir` 的命令：它们操作的是 `~/.ngm`（缓存、内容存储），
// 而不是某个项目。**这份名单必须完整**——多一个没列到的全局命令，
// 矩阵就会给它塞 `--dir`，于是它打用法文本，而判据把"落在用法文本桶"当作红。
var globalCommands = map[string]bool{"cache": true, "store": true}
