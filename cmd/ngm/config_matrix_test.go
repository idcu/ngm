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
		{"manifest-broken-json", func(t *testing.T) string { return manifestDir(t, "{") }},
		{"manifest-without-a-name", func(t *testing.T) string {
			return manifestDir(t, `{"runtime":"node"}`)
		}},
		{"manifest-with-an-unknown-runtime", func(t *testing.T) string {
			return manifestDir(t, `{"name":"github.com:x/app","runtime":"cobol"}`)
		}},
		{"manifest-with-a-dependency-without-a-name", func(t *testing.T) string {
			return manifestDir(t, `{"name":"github.com:x/app","runtime":"node",`+
				`"dependencies":[{"ref":"v1","refType":"tag"}]}`)
		}},
		{"manifest-with-a-mistyped-dependencies-field", func(t *testing.T) string {
			return manifestDir(t, `{"name":"github.com:x/app","runtime":"node","dependencies":"nope"}`)
		}},
		{"lock-is-broken-json", func(t *testing.T) string {
			// 这一条需要一个**合法项目**再加一个坏锁（夹具仍然很便宜：不起 git）。
			p := newProject(t)
			writeSurfaceFile(t, p, "ngm.lock", "{")
			return p
		}},
	}

	counts := map[string]int{}
	oks := []string{}
	okByCmd := map[string]int{}
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
					oks = append(oks, spec.Name+"/"+sh.name)
					okByCmd[spec.Name]++
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

	// 退 0 的格子**不受任何通道契约约束**——若它们悄悄攒起来，
	// 这张网的覆盖率就会在数字上很好看、在实际上很空。所以两件事：
	//
	//	① 把它们**报出来**（人要看一眼这些"没问题"是什么）；
	//	② 其中"**全形状都退 0**"的命令**必须在 globalCommands 里**——
	//	   一个对项目上下文完全无所谓的命令，要么是有据可查的全局命令，
	//	   要么说明矩阵够不到它（与"用法文本不许出现"同一个道理）。
	for _, label := range oks {
		t.Logf("exit 0: %s", label)
	}

	// **"退 0" 必须是登记过的**（v0.38）。
	//
	// 退 0 的格子不受任何通道契约约束：这里没建议可给、也没报告可查。
	// 所以它们是这张网唯一能"悄悄攒起来"的地方——数字上很好看、实际上很空。
	// 处置与 v0.15 的负例名单、v0.22 的缺口名单同款：**每一格都要写下为什么它是 0**，
	// 而且**双向**都要对账（有格子没登记 ⇒ 红；登记了却没有格子 ⇒ 红）。
	for cmd, n := range okByCmd {
		if _, ok := exitZeroByDesign[cmd]; !ok {
			t.Errorf("`ngm %s` exited 0 in %d run(s) and nothing here says why — "+
				"a zero is not a pass until someone writes down what it means", cmd, n)
		}
	}
	for cmd := range exitZeroByDesign {
		if okByCmd[cmd] == 0 {
			t.Errorf("exitZeroByDesign lists %q, but no run of it exited 0 any more — "+
				"the registration went stale, drop it (or find out what changed)", cmd)
		}
	}
	for cmd, n := range okByCmd {
		if n == len(shapes) && !globalCommands[cmd] {
			if _, registered := exitZeroByDesign[cmd]; registered {
				continue
			}
			t.Errorf("`ngm %s` exited 0 under **every** shape — it ignores the project context entirely. "+
				"Either it is a global command (add it to globalCommands with a reason), "+
				"or the matrix cannot reach it", cmd)
		}
	}
	t.Logf("config matrix: %d runs over %d commands × %d shapes → %v",
		runs, len(commands), len(shapes), counts)
}

// manifestDir 造一个"只有 ngm.json、内容是给定文本"的目录。
//
// 为什么不做成夹具（起 git、种 mirror）：这些形状**只需要一个文件**——
// 而正是这种廉价，让"把形状枚举完整"变得划算（见本版复盘）。
func manifestDir(t *testing.T, body string) string {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "ngm.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
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

// exitZeroByDesign 登记"**退 0 是正确行为**"的格子，并写下**为什么**。
//
// 为什么需要这张表（v0.38）：退 0 的格子不受任何通道契约约束——没有建议可给、
// 也没有报告可查。于是它们是这张网唯一能**悄悄攒起来**的地方：
// 覆盖率数字很好看，实际上什么都没查。登记 + 双向对账（有格子没登记 ⇒ 红；
// 登记了却没格子 ⇒ 红）把这个口子关上。
//
// 每条理由都是**实测**出来的（v0.38 逐条跑过），不是"大概吧"。
var exitZeroByDesign = map[string]string{
	"cache": "全局命令：操作 `~/.ngm` 的缓存，与项目上下文无关（8 个形状都退 0）。" +
		"见 globalCommands",
	"store": "全局命令：操作 `~/.ngm` 的内容存储，与项目上下文无关（8 个形状都退 0）。" +
		"见 globalCommands",
	"init": "`init` 的职责就是**创建**清单——「目录不存在」与「空目录」正是它的成功路径",
	"add": "`add` 只写清单、**不读锁**：实测锁损坏时它照常追加，并打印 " +
		"`next: run ngm install to resolve and lock`（把解析交给 install）",
	"update": "`update` 的职责**就是（重）写锁**：实测 `--all` 在锁损坏时全量重解析并写出新锁" +
		"（坏锁它不需要读）",
	"config":  "`config validate` 校验的是**清单**，不读锁",
	"engines": "`engines list` 读的是引擎目录，不读锁",
}
