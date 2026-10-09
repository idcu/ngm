package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

	shapes := configErrorShapes

	counts := map[string]int{}
	oks := []string{}
	okByCmd := map[string]int{}
	runs := 0
	// v0.65：**每一列（形状）都要有自己的证据**。
	//
	// 从前这张网只数**全局**下限（`error-text ≥ 40`），于是"某一列其实什么都没测到"
	// 是看不见的 ✗：新增一个形状、而它的夹具其实合法（或与另一列等价），
	// 矩阵照样绿、覆盖照样是"8 形状 × N 命令"那么好看。
	// 现在每一格都记下"通道 + 第一行"，按列比较：
	//
	//	① 这一列必须至少有一格真的**走到配置层**（不是全退 0 / 全用法）；
	//	② 这一列的签名**不能与另一列完全相同**（那样它只是噪声）。
	shapeSig := map[string][]string{}

	for _, spec := range commands {
		for _, sh := range shapes {
			runs++
			t.Run(spec.Name+"/"+sh.name, func(t *testing.T) {
				home := isolateUserEnv(t)
				args := append([]string{spec.Name}, matrixArgs[spec.Name]...)
				paths := []string{home}
				if !globalCommands[spec.Name] {
					dir := sh.dir(t)
					paths = append(paths, dir)
					args = append(args, "--dir="+dir)
				}

				var out, errb bytes.Buffer
				code := dispatch(args, &out, &errb)
				stderr, text := errb.String(), out.String()+errb.String()
				body := reportBody(text)

				var class string
				switch {
				case strings.Contains(text, "USAGE:"):
					class = "usage"
					// 注意读的是**未截断**的 `text`：`reportBody` 恰恰会在 "USAGE:" 处截断，
					// 于是用它去找用法文本永远找不到——初版正是这么写的，
					// 这一格只能掉进"其它"桶里被兜住（判据的另一条腿替它报了警）。
					// 配置形状下打用法 = 矩阵没走到配置层（见文件头注释）。
					t.Errorf("this command printed its usage instead of reaching the configuration layer — "+
						"either the argument table cannot reach it, or it stopped accepting --dir:\n%s",
						firstLine(text))
				case code == 0:
					class = "ok"
					counts["ok"]++
					oks = append(oks, spec.Name+"/"+sh.name)
					okByCmd[spec.Name]++
				case strings.TrimSpace(text) == "":
					class = "silent"
					counts["silent"]++
					t.Errorf("exited %d without saying anything — a silent failure is the hardest kind to debug", code)
				case reErrPrefix.MatchString(stderr):
					class = "error"
					counts["error-text"]++
					if !strings.Contains(stderr, "hint:") {
						t.Errorf("this error gives no next step:\n%s", stderr)
					}
				case strings.Contains(body, failMark):
					class = "report"
					counts["report"]++
					if len(reActionLine.FindAllString(body, -1)) == 0 {
						t.Errorf("this failing report lists items (`%s`) but says nothing about what to do next:\n%s",
							failMark, text)
					}
				default:
					class = "unclassified"
					counts["unclassified"]++
					t.Errorf("this output fits none of the known channels (error text / report / usage / silent) — "+
						"a new channel is a decision someone has to make consciously:\n%s", firstLine(text))
				}
				// 签名里**不能有这一轮自己造出来的路径**（v0.43 那条教训，v0.51 收成一个 helper）。
				// 签名 = 这张网**自己承诺的可分辨面**（通道 · 退出码 · hint · 行动行），
				// 不是输出全文：全文里有平台相关的写法（文件系统错误文本、路径形态），
				// v0.65 的第一版正是用全文比较，于是在 ubuntu / macos 上把两列判成了等价 ✗
				// （本机 Windows 不红）——而那三个 job 一红，事情就说不清了。
				sigCell := spec.Name + "=" + outcomeSignature(t, normalizeRunPaths(t, text, paths...), class, code)
				shapeSig[sh.name] = append(shapeSig[sh.name], sigCell)
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

	// v0.65：**每一列（形状）都要有自己的证据**。
	//
	// 上面那些是**全局**下限（`error-text ≥ 40`）——它们回答"矩阵整体还在动吗"，
	// 回答不了"**这一列**还在动吗"：新增一个形状、而它的夹具其实合法（或与另一列等价），
	// 矩阵照样绿，覆盖照样是"8 形状 × N 命令"那么好看 ✗。
	// 于是按列比较（签名 = 每格的"通道 + 第一行"，路径已归一化）：
	//
	//	① 这一列至少要有一格真的**走到配置层**（不是全退 0 / 全用法 / 全静默）；
	//	② 这一列的签名不能与另一列**完全相同**——那样它只是噪声。
	{
		seen := map[string]string{} // 签名 → 第一个用它的形状
		for _, sh := range shapes {
			sig := shapeSig[sh.name]
			if len(sig) == 0 {
				t.Errorf("形状 %q 没有任何一格可比较——矩阵根本没跑它", sh.name)
				continue
			}
			reached := 0
			for _, cell := range sig {
				if reReachedConfigLayer.MatchString(cell) {
					reached++
				}
			}
			if reached == 0 {
				t.Errorf("形状 %q 没有任何命令在它上面走到配置层（全退 0 / 全用法 / 全静默）——"+
					"这一列没在测东西：夹具可能其实合法，或者参数表够不到它", sh.name)
			}
			key := strings.Join(sig, "\n")
			if other, dup := seen[key]; dup {
				// 出口只有两个：改夹具让这一列落到别的分支上，或者在 shapeEquivalents
				// 里写下"为什么两列必须都在"。**没有"忍着"这个选项**——它会静默地把噪声
				// 算成覆盖。
				//
				// 登记按**两个方向**认（哪一列先被遍历到不该决定这条登记有没有生效）。
				if why, ok := shapeEquivalents[sh.name]; ok && why != "" {
					continue
				}
				if why, ok := shapeEquivalents[other]; ok && why != "" {
					continue
				}
				t.Errorf("形状 %q 与 %q 的**每一格**输出都完全相同——对这张网的判据"+
					"（通道 + 下一步）而言它们是同一列：要么改夹具让它落在别的分支上，"+
					"要么在 shapeEquivalents 里写下为什么两列必须都在", sh.name, other)
			}
			seen[key] = sh.name
		}
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

// configErrorShapes 是**配置错误形状**这一轴（v0.65 从测试函数里提出来）。
//
// 提出来有两个理由：
//
//  1. 它是一根**轴**——"命令 × 形状"那张矩阵的一半。藏在函数体里时，
//     它既不能被别的判据引用，也看不出自己需要哪条守卫；
//  2. v0.65 给它加了守卫（见测试里的 shapeSig 段）：每一列必须有**自己的证据**——
//     至少要有一格走到配置层，且签名不能与另一列完全相同。
//     于是"新增一个形状而它其实什么都没测到"会红，而不是**静默地把覆盖数字撑大**。
var configErrorShapes = []struct {
	name string
	dir  func(t *testing.T) string
}{
	{"dir-missing", func(t *testing.T) string { return filepath.Join(t.TempDir(), "nope") }},
	{"no-manifest", func(t *testing.T) string { return t.TempDir() }},
	{"manifest-broken-json", func(t *testing.T) string { return manifestDir(t, "{") }},
	{"manifest-without-a-name", func(t *testing.T) string {
		return manifestDir(t, `{"runtime":"node"}`)
	}},
	// 注意这两条都写全了**必填字段**（name + version）：v0.65 的按列守卫发现它们
	// 从前都停在更早的一处校验上（`version is required`）——两条夹具的输出**逐格相同**，
	// 也就是说它们谁都没有测到名字里写的那个缺陷 ✗。夹具缺字段时，失败会发生在**别处**，
	// 而矩阵照样绿：这正是"每一列都要有自己的证据"存在的理由。
	{"manifest-with-an-unknown-runtime", func(t *testing.T) string {
		return manifestDir(t, `{"name":"github.com:x/app","version":"1.0.0","runtime":"cobol"}`)
	}},
	{"manifest-with-a-dependency-without-a-name", func(t *testing.T) string {
		return manifestDir(t, `{"name":"github.com:x/app","version":"1.0.0","runtime":"node",`+
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

// outcomeSignature 把一个决策节点压成这张网**自己承诺的可分辨面**：
//
//	通道（错误文本 / 报告 / 用法 / 静默） · 退出码 · `hint:` 行 · 行动行
//
// **不含消息全文**：全文里有平台相关的写法（文件系统错误文本、路径形态、大小写），
// 拿它当签名会在一个平台上判出"等价"、在另一个平台上不判 ✗——而跨平台的红
// 最难查（日志要管理员权限，本机又复现不出来）。
//
// 这正是这张网对用户的承诺本身："通道 + 下一步"。两列在这四项上完全一致时，
// 用户看不出区别，判据也看不出区别——那时该做的是修夹具，或者在
// `shapeEquivalents` 里写下为什么两列必须都在。
func outcomeSignature(t *testing.T, text, class string, code int) string {
	t.Helper()
	hint, action := "", ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "hint:") {
			hint = strings.TrimSpace(line)
			break
		}
	}
	if m := reActionLine.FindString(text); m != "" {
		action = m
	}
	return strconv.Itoa(code) + "/" + class + "|" + hint + "|" + action
}

// shapeEquivalents 是**具名等价**：某一列与另一列逐格相同时，为什么它还得在。
//
// 目前**为空**——v0.65 立这条守卫时它当场抓出两列等价（`manifest-with-an-unknown-runtime`
// 与 `manifest-with-a-dependency-without-a-name`），而那两列的真相是**夹具少了必填字段**：
// 它们都停在更早的 `version is required` 上，谁都没有测到名字里写的缺陷 ✗。
// 处置是**修夹具**，不是登记等价——这个出口留给"两列确实各有前置条件"那种情形。
//
// 每条都必须写 why（空串等于没登记）。
//
// 第一条（v0.65，把签名从"输出全文"改成"通道 + 退出码 + hint + 行动行"之后才现形）：
// `no-manifest` 与 `dir-missing` 落在**同一个 not-found 分支**上——对一个用户来说，
// "目录不存在"与"目录在、清单不在"给出的是同一句话与同一个下一步 ✓。
// 保留两者是为了钉住前置条件（前者还顺带说明"目录里什么都没有"也是一种合法起点），
// 因此这是一条**具名等价**，而不是噪声。
var shapeEquivalents = map[string]string{
	"no-manifest": "与 `dir-missing` 同落在 not-found 分支（同一句错误 + 同一个下一步）；" +
		"保留它是为了钉住另一个前置条件：目录存在、但里面没有 ngm.json",
	"manifest-with-a-mistyped-dependencies-field": "与 `manifest-broken-json` 同落在**解码失败**分支" +
		"（同一句错误 + 同一个下一步）；两个形状各钉一种解码失败：语法坏 vs 类型不匹配",
}

// reReachedConfigLayer 从签名格里认"这一格真的走到了配置层"。
//
// 签名格的形状是 `<命令>=<退出码>/<通道>|…`（见 outcomeSignature）——
// 用正则而不是字符串包含：v0.65 的第一版写成 `strings.Contains(cell, "=error|")`，
// 而格式一换成 `<码>/<通道>` 它就再也匹配不上，于是**每一列都被报成"没在测东西"** ✗
// （形状变了、判据没跟上——本项目的常见坑之一）。
var reReachedConfigLayer = regexp.MustCompile(`=\d+/(error|report)\|`)

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
