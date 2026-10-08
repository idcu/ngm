package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// reExitCodeLine 匹配 EXIT CODES 段里的一条声明：`<码> <说明>`。
var reExitCodeLine = regexp.MustCompile(`^([0-9])\s+(\S.*)$`)

// indentOf 返回行的缩进列数。
func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " \t")) }

// declaredExitCodes 取出用法文本里 EXIT CODES 段声明的码。
//
// 段的边界：遇到**顶格**且不是 `<数字> 说明` 的行就结束。这样段里既能带
// IMPORTANT / CACHE / POLICY 这类补充块，也能容纳跨行的说明。
//
// 第一版把"续行"误判成段结束（用了固定阈值），21 个命令里只有 6 个被解析出来——
// `verify` 只解出 `0 1 2`，于是网对着一份**残缺**的声明对账。
// 这类"判据自己先错"的失败比"没抓到"更危险：它假装已经对过账。
func declaredExitCodes(usage string) []int {
	lines := strings.Split(usage, "\n")
	in := false
	var codes []int
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "EXIT CODES") {
			in = true
			continue
		}
		if !in || l == "" {
			continue
		}
		if m := reExitCodeLine.FindStringSubmatch(l); m != nil {
			codes = append(codes, int(m[1][0]-'0'))
			continue
		}
		if indentOf(lines[i]) > 0 {
			continue // 上一条的续行
		}
		break
	}
	return codes
}

// exitCase 是"一条声明"的实测判据，或一条已知缺口。
type exitCase struct {
	cmd  string
	code int
	args []string
	// setup 决定用例跑在什么样的项目目录上。
	setup string
	// env 是该用例要设置的环境变量（假引擎用 FAKE_EXIT 控制成功/失败）。
	env map[string]string
	// noDir：cache / store 操作的是全局层，不接受 --dir。
	noDir bool
	// why：缺口条目与"在别处测量"的条目都要写清原因。
	why string
}

// setup 的取值。
const (
	setupMissing  = "missing"  // 目录不存在
	setupEmpty    = "empty"    // 空目录（没有 ngm.json）
	setupProject  = "project"  // 有 ngm.json、无依赖
	setupDep      = "dep"      // 有依赖 + lock + vendor
	setupCold     = "cold"     // 有依赖声明、但没有本地 mirror、也没有 lock
	setupNoMirror = "nomirror" // 有 lock + vendor，但本地 mirror 被删掉
	setupDrift    = "drift"    // 上游把 tag 挪到新提交（非预期漂移）
	setupDrifted  = "drifted"  // 漂移 + verifyOnLock（install/update 的策略路径）
	setupTampered = "tampered" // lock 里的 archiveDigest 被改成全零
	setupOsvClean = "osvclean" // OSV 走本地替身（无发现）
	setupOsvVuln  = "osvvuln"  // OSV 走本地替身（一个 HIGH 发现）
	setupGhost    = "ghost"    // 引擎目录里声明了一个"没装"的引擎
	setupEngine   = "engine"   // 真实可跑的假引擎（FAKE_EXIT 控制成功/失败）
)

// fakeEngine 是假引擎二进制的路径，由测试启动时构建一次。
var fakeEngine string

// exitCodeMeasured 是**已经对上账**的那部分声明。
//
// 触发命令一律**离线或本地替身**：只看本地 mirror / vendor / lock，用假引擎，
// 用本地 OSV 替身。凡需要真实网络或真实引擎安装的码，不进这张表。
var exitCodeMeasured = []exitCase{
	// ---- 0：成功态 ----
	{cmd: "init", code: 0, args: []string{"github.com:x/app", "--runtime=node"}, setup: setupEmpty},
	{cmd: "add", code: 0, args: []string{"github:x/dep@v1", "--ref-type=tag"}, setup: setupProject},
	{cmd: "remove", code: 0, args: []string{"github:x/dep"}, setup: setupDep},
	{cmd: "install", code: 0, args: nil, setup: setupDep},
	{cmd: "update", code: 0, args: []string{"--offline", "--all"}, setup: setupDep},
	{cmd: "verify", code: 0, args: nil, setup: setupDep},
	{cmd: "audit", code: 0, args: []string{"--no-cache"}, setup: setupOsvClean},
	{cmd: "why", code: 0, args: []string{"github:x/dep"}, setup: setupDep},
	{cmd: "tree", code: 0, args: nil, setup: setupDep},
	{cmd: "outdated", code: 0, args: []string{"--offline"}, setup: setupDep},
	{cmd: "cache", code: 0, args: []string{"clean"}, noDir: true},
	{cmd: "store", code: 0, args: []string{"usage"}, noDir: true},
	{cmd: "config", code: 0, args: []string{"validate"}, setup: setupDep},
	{cmd: "engines", code: 0, args: []string{"list"}, setup: setupProject},
	{cmd: "typecheck", code: 0, args: []string{"--engine=fake"}, setup: setupEngine, env: map[string]string{"FAKE_EXIT": "0"}},
	{cmd: "typedecl", code: 0, args: []string{"--outdir=types", "--engine=fake"}, setup: setupEngine, env: map[string]string{"FAKE_EXIT": "0"}},
	{cmd: "build", code: 0, args: []string{"entry.ts", "--engine=fake"}, setup: setupEngine, env: map[string]string{"FAKE_EXIT": "0"}},
	{cmd: "transform", code: 0, args: []string{"src.ts", "--loader=ts", "--engine=fake"}, setup: setupEngine, env: map[string]string{"FAKE_EXIT": "0"}},
	{cmd: "css", code: 0, args: []string{"a.css", "--engine=fake"}, setup: setupEngine, env: map[string]string{"FAKE_EXIT": "0"}},
	// mappings / integrations 要 `install` 先把 ngm.mappings.json 写出来。
	{cmd: "mappings", code: 0, args: []string{"validate"}, setup: setupDep},
	{cmd: "integrations", code: 0, args: []string{"add", "vite"}, setup: setupDep},

	// ---- 1：策略失败（非预期漂移 / 引擎运行失败 / 超阈值漏洞）----
	{cmd: "verify", code: 1, args: nil, setup: setupDrift},
	{cmd: "install", code: 1, args: nil, setup: setupDrifted},
	{cmd: "audit", code: 1, args: []string{"--no-cache"}, setup: setupOsvVuln},
	{cmd: "tree", code: 1, args: []string{"--osv"}, setup: setupOsvVuln},
	{cmd: "typecheck", code: 1, args: []string{"--engine=fake"}, setup: setupEngine, env: map[string]string{"FAKE_EXIT": "1"}},
	{cmd: "typedecl", code: 1, args: []string{"--outdir=types", "--engine=fake"}, setup: setupEngine, env: map[string]string{"FAKE_EXIT": "1"}},
	{cmd: "build", code: 1, args: []string{"entry.ts", "--engine=fake"}, setup: setupEngine, env: map[string]string{"FAKE_EXIT": "1"}},
	{cmd: "transform", code: 1, args: []string{"src.ts", "--loader=ts", "--engine=fake"}, setup: setupEngine, env: map[string]string{"FAKE_EXIT": "1"}},
	{cmd: "css", code: 1, args: []string{"a.css", "--engine=fake"}, setup: setupEngine, env: map[string]string{"FAKE_EXIT": "1"}},

	// ---- 2：完整性失败 ----
	{cmd: "verify", code: 2, args: nil, setup: setupTampered},
	{cmd: "install", code: 2, args: nil, setup: setupTampered},

	// ---- 3：配置 / 策略 / lock 错误（含用法错误）----
	// 每个命令都给它一个不认识的 flag，并指向一个不存在的目录。
	{cmd: "init", code: 3, args: []string{"github.com:x/app", "--runtime=node", "--zz-nope"}, setup: setupMissing},
	{cmd: "add", code: 3, args: []string{"github:x/dep@v1", "--ref-type=tag", "--zz-nope"}, setup: setupMissing},
	{cmd: "remove", code: 3, args: []string{"github:x/dep", "--zz-nope"}, setup: setupMissing},
	{cmd: "install", code: 3, args: []string{"--zz-nope"}, setup: setupMissing},
	{cmd: "update", code: 3, args: []string{"--zz-nope"}, setup: setupMissing},
	{cmd: "verify", code: 3, args: []string{"--zz-nope"}, setup: setupMissing},
	{cmd: "audit", code: 3, args: []string{"--zz-nope"}, setup: setupMissing},
	{cmd: "why", code: 3, args: []string{"github:x/dep", "--zz-nope"}, setup: setupMissing},
	{cmd: "tree", code: 3, args: []string{"--zz-nope"}, setup: setupMissing},
	{cmd: "outdated", code: 3, args: []string{"--zz-nope"}, setup: setupMissing},
	{cmd: "typecheck", code: 3, args: []string{"--zz-nope"}, setup: setupMissing},
	{cmd: "typedecl", code: 3, args: []string{"--outdir=types", "--zz-nope"}, setup: setupMissing},
	{cmd: "build", code: 3, args: []string{"--zz-nope"}, setup: setupMissing},
	{cmd: "transform", code: 3, args: []string{"--loader=ts", "--zz-nope"}, setup: setupMissing},
	{cmd: "css", code: 3, args: []string{"a.css", "--zz-nope"}, setup: setupMissing},
	{cmd: "mappings", code: 3, args: []string{"validate", "--zz-nope"}, setup: setupMissing},
	{cmd: "integrations", code: 3, args: []string{"add", "vite", "--zz-nope"}, setup: setupMissing},
	{cmd: "cache", code: 3, args: []string{"clean", "--zz-nope"}, setup: setupMissing},
	{cmd: "store", code: 3, args: []string{"usage", "--zz-nope"}, setup: setupMissing},
	{cmd: "config", code: 3, args: []string{"validate", "--zz-nope"}, setup: setupMissing},
	{cmd: "engines", code: 3, args: []string{"list", "--zz-nope"}, setup: setupMissing},

	// ---- 4：Git 或网络失败（含 --offline 且资源不在本地）----
	{cmd: "install", code: 4, args: []string{"--offline"}, setup: setupCold},
	{cmd: "update", code: 4, args: []string{"--offline", "--all"}, setup: setupCold},
	{cmd: "verify", code: 4, args: []string{"--offline"}, setup: setupNoMirror},
	{cmd: "tree", code: 4, args: []string{"--offline"}, setup: setupNoMirror},
	{cmd: "audit", code: 4, args: []string{"--offline"}, setup: setupDep},

	// ---- 5：引擎不可用 ----
	// 注意区分两个码：名字**不在目录里** ⇒ 3（配置错误）；
	// 名字在目录里但**命令没装** ⇒ 5（引擎不可用）。
	{cmd: "typecheck", code: 5, args: []string{"--engine=ghost"}, setup: setupGhost},
	{cmd: "typedecl", code: 5, args: []string{"--outdir=types", "--engine=ghost"}, setup: setupGhost},
	{cmd: "build", code: 5, args: []string{"--engine=ghost"}, setup: setupGhost},
	{cmd: "transform", code: 5, args: []string{"--loader=ts", "--engine=ghost"}, setup: setupGhost},
	{cmd: "css", code: 5, args: []string{"a.css", "--engine=ghost"}, setup: setupGhost},
	{cmd: "engines", code: 5, args: []string{"validate"}, setup: setupGhost},
}

// exitCodeElsewhere 是**由专属测试测量**的声明。
//
// 它们进不了主表：触发它们要先**清空 PATH**（让 ngm 找不到 deno），
// 而清空 PATH 会让 git 一起消失——所以每个都得单独成测，
// 在自己的 isolateUserEnv 里先建好夹具、再清 PATH。
var exitCodeElsewhere = []exitCase{
	// v0.59：码 6（内部失败 · ADR-026）由专属测试测量——那一组用例给 `dispatch`
	// 传一个**永远写失败的 writer**，让命令算完结论、写报告时失败。
	// 指针由 `TestV56RegisteredPointersResolve` 核对（它要求子测试真的存在）。
	// v0.62：另两个命令的码 6 搬进实测（夹具本来就现成——`v3AuditProject` 把 OSV
	// 指向本地替身、`integrations add <tool>` 只需一个已安装的项目）。
	{cmd: "audit", code: 6, why: "TestV62TheRemainingGapCommandsAreMeasuredForCodeSix/audit"},
	{cmd: "integrations", code: 6, why: "TestV62TheRemainingGapCommandsAreMeasuredForCodeSix/integrations"},
	// v0.63：最后四个（也是 v0.62 判成"到不了"的那四个）搬进实测。
	// v0.62 把它们的失败读成"钩子是死的"，真相是那次的夹具**少了一行**
	// `isolateUserEnv`——命令读的是真实 HOME，于是没有 `run:fake-engine` 授权。
	// 补上之后四个都退 6（见 v0.63 复盘的第一节）。
	{cmd: "build", code: 6, why: "TestV63EngineCommandsAreMeasuredForCodeSix/build"},
	{cmd: "typecheck", code: 6, why: "TestV63EngineCommandsAreMeasuredForCodeSix/typecheck"},
	{cmd: "css", code: 6, why: "TestV63EngineCommandsAreMeasuredForCodeSix/css"},
	{cmd: "transform", code: 6, why: "TestV63EngineCommandsAreMeasuredForCodeSix/transform"},
	{cmd: "verify", code: 6, why: "TestV22StdoutWriteFailureIsMeasured/verify"},
	{cmd: "why", code: 6, why: "TestV22StdoutWriteFailureIsMeasured/why"},
	{cmd: "outdated", code: 6, why: "TestV22StdoutWriteFailureIsMeasured/outdated"},
	{cmd: "tree", code: 6, why: "TestV22StdoutWriteFailureIsMeasured/tree"},
	{cmd: "engines", code: 6, why: "TestV22StdoutWriteFailureIsMeasured/engines"},
	{cmd: "verify", code: 5, why: "TestV22DenoIsMissing/verify"},
	{cmd: "install", code: 5, why: "TestV22DenoIsMissing/install"},
	{cmd: "audit", code: 5, why: "TestV22DenoIsMissing/audit"},
}

// TestV56RegisteredPointersResolve 要求上表里的**指针指到真东西**（v0.56）。
//
// 那张表的语义是"这个码由**别处的专属测试**测量"，而它只写了三串字符串：
// `TestV22DenoIsMissing/verify` 之类。那些子测试若被改名或删掉，
// **没有任何判据会红**：V22 继续认为"码 5 已经被测量了"，而实际上没有人在测它。
//
// 这是 v0.52 那条"登记看着空气"的**第二次现形**——那次查的是测试函数名
// （v0.53 立的判据），这次是**子测试名**：同一条纪律，指针指到哪里就要核到哪里。
//
// 判据从**源码**取：指针形如 `<测试函数>/<子测试>`，
// 就在本目录的 `_test.go` 里找那个测试函数，并要求它里面有 `t.Run("<子测试>"`。
func TestV56RegisteredPointersResolve(t *testing.T) {
	if len(exitCodeElsewhere) == 0 {
		t.Fatal("这张表是空的——没有可核对的指针")
	}

	files := map[string]string{}
	for _, f := range testFilesInThisDir(t) {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		files[filepath.Base(f)] = string(body)
	}

	for _, c := range exitCodeElsewhere {
		fn, sub, ok := strings.Cut(c.why, "/")
		if !ok {
			t.Errorf("%s 的指针 %q 不是 `<测试函数>/<子测试>` 的形状——"+
				"这条判据没法核对它，而核对不了就是没有查", c.cmd, c.why)
			continue
		}

		owner, body := "", ""
		for name, text := range files {
			if strings.Contains(text, "func "+fn+"(") {
				owner, body = name, text
				break
			}
		}
		if owner == "" {
			t.Errorf("%s 的指针指向 %q，而本目录里**没有**这个测试函数——登记指向空气", c.cmd, fn)
			continue
		}
		if !strings.Contains(body, `t.Run("`+sub+`"`) {
			t.Errorf("%s 的指针说码 %d 由 %s 测量，而 %s 里**没有**那个子测试（找不到 `t.Run(%q`）——"+
				"它若被删改，V22 会继续以为这个码测得着", c.cmd, c.code, c.why, owner, sub)
		}
	}
	t.Logf("registered pointers: %d pointer(s) resolve to a real subtest", len(exitCodeElsewhere))
}

// exitCodeGaps 是**已知还没被测量**的声明。
//
// 与 v0.15 的负例名单同一个套路：**名单自己会过期**。
// 每条都断言"该命令确实还声明着这个码"——哪天那段声明被删改，这一条 gap
// 就会变红，逼着人把它一起删掉。
var exitCodeGaps = map[string][]int{
	// update 的三个码目前测不到，且**每一条都带着实测记录**：
	//
	//   1 —— 声明说"verifyOnLock 开启且**更新后的复检**发现漂移或策略失败"。
	//
	//        实测链（v0.28 逐步骤量过）：
	//          · 上游把 tag 挪走、mirror 停在旧提交时，`verify` **确实**看到漂移 → **1** ✓；
	//          · 但 `update --all` 退 **0**，而且它写进 lock 的是**新提交**——
	//            也就是说 update 的解析**已经跟上了新 ref**，复检于是与它一致。
	//
	//        要走到 1，得让"更新已经写完、复检却仍看到漂移"：即上游在两步之间又动过一次。
	//        那是**竞态**，不是离线夹具能固定下来的状态。声明的代码路径真实存在
	//        （`autoVerifyAfterLock` 会原样返回 verify 的退出码），只是**无法被确定性地触发**。
	//
	//   2 —— 两个子句分开看："postinstall 钩子失败"需要 Deno **在**（本环境没有）；
	//        "vendored bytes did not verify" 实测两次都走不到——篡改 lock 的 archiveDigest
	//        退 0，篡改 vendor 里的文件也退 0。原因是 update **重建** lock 与 vendor，
	//        而不是校验既有的：它把被篡改的东西**覆盖掉**，于是复检看到的是新写的、
	//        一致的副本。这一句更像是从 `install` 抄来的（install 才会就地校验）。
	//        **它能不能被走到仍未被证明**——这里只记录"两种构造都得到 0"。
	//
	//   5 —— 需要 Deno 缺失，而 update 运行时要 git；清空 PATH 会先把 git 拿掉，
	//        于是先退 4（离线取数失败），到不了钩子那一步。
	"update": {1, 2, 5},

	// v0.59 给码 6 立了"声明了就要能测"，v0.62 把 7 个命令搬进实测，
	// v0.63 把最后四个也搬完——**码 6 已无缺口**（上表里 9 条 elsewhere 覆盖全部
	// 声明过 6 的命令）。
	//
	// 这里留一段给"缺口表"本身的使用者：它是**未测量的声明**的住处，
	// 而"未测量的原因"必须**量过**——v0.59 曾写过 6 条原因，v0.62 一试发现
	// 两条彻底错、四条只对一半；v0.63 又发现 v0.62 那四条里有一条是**误判**
	// （把"夹具少一行"读成了"钩子是死的"，见上表 v0.63 那段注释与 v0.63 复盘）。
	//
	// **一个钩子看起来没人用，先量它到底有没有人用**——`AllowedEngines()` 的消费者
	// 在 `testenv_test.go` 的 `init()` 里（`testutils.WriteUserConfig` 的注入实现）。
}

// TestV21UsageExitCodeSectionsAreWellFormed 固定：**每个命令都写下了自己的退出码**，
// 且那段文本**格式正确、机器可解析**。
//
// 为什么它值得一张网：`ngm <cmd> --help` 是脚本作者唯一会读的东西。
// 段里写一个**不存在的码**（比如 9）比不写更糟——它看起来像承诺。
//
// v0.59：上界不再硬编码 5，而是**读全局契约表**（`observability.md` 的退出码表）
// ——契约的唯一事实源在文档里，判据从那里取，才不会在契约扩展时落后一版。
func TestV21UsageExitCodeSectionsAreWellFormed(t *testing.T) {
	if len(commands) == 0 {
		t.Fatal("the command table is empty — the net would pass vacuously")
	}
	contract := observabilityExitCodes(t)
	if len(contract) == 0 {
		t.Fatal("observability.md 里读不出退出码表——这条判据没有可核对的契约")
	}

	for _, spec := range commands {
		t.Run(spec.Name, func(t *testing.T) {
			codes := declaredExitCodes(spec.Usage)
			if len(codes) == 0 {
				t.Fatalf("`ngm %s` has no EXIT CODES section, or it lists no code", spec.Name)
			}
			seen := map[int]bool{}
			for _, c := range codes {
				if _, ok := contract[c]; !ok {
					t.Errorf("`ngm %s` declares exit code %d; the global contract "+
						"(docs/architecture/observability.md) does not list it", spec.Name, c)
				}
				if seen[c] {
					t.Errorf("`ngm %s` declares exit code %d twice", spec.Name, c)
				}
				seen[c] = true
			}
		})
	}
}

// TestV22DeclaredCodesAreMeasuredOrKnownGaps 是本网的牙齿。
//
// 它要求：**每个命令声明的每个码，要么有一条能离线触发它的实测，
// 要么由专属测试测量，要么在缺口名单里。** 于是以后任何人新声明一个码，
// 都必须同时回答——怎么证明它真的会退这个码。否则测试红。
func TestV22DeclaredCodesAreMeasuredOrKnownGaps(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	measured := 0
	everywhere := 0
	gapped := 0

	for _, spec := range commands {
		for _, code := range declaredExitCodes(spec.Usage) {
			trig, ok := lookupMeasured(spec.Name, code)
			if !ok {
				switch {
				case lookupElsewhere(spec.Name, code):
					everywhere++
				case lookupGap(spec.Name, code):
					gapped++
				default:
					t.Errorf("`ngm %s` declares exit code %d: not measured, not measured elsewhere, "+
						"and not a known gap — add the trigger or record the gap", spec.Name, code)
				}
				continue
			}

			// 每个用例都**重新隔离一次环境**并造自己的项目目录：
			// 共用一个会让前一个命令的副作用（写出的 lock、OSV 缓存、
			// 甚至 t.Setenv 残留的端点）改变后一个的读数。探针连踩三次这类坑。
			home := isolateUserEnv(t)
			args := append([]string{spec.Name}, trig.args...)
			if !trig.noDir {
				args = append(args, "--dir="+buildFixture(t, home, trig.setup))
			}
			for k, v := range trig.env {
				t.Setenv(k, v)
			}

			var out, errb bytes.Buffer
			got := dispatch(args, &out, &errb)
			if got != code {
				msg := strings.TrimSpace(firstLine(errb.String()))
				if msg == "" {
					msg = strings.TrimSpace(firstLine(out.String()))
				}
				t.Errorf("`ngm %s` declares exit code %d, but measured %d: %s",
					spec.Name, code, got, msg)
				continue
			}
			measured++
		}
	}

	// 缺口名单自己也得被看住：条目若不再对应任何**声明**，就说明那段声明已被
	// 删改（或测量已补上），条目必须一起删——否则一张"已知的洞"会永远留在纸上。
	for cmd, codes := range exitCodeGaps {
		for _, g := range codes {
			if !declares(cmd, g) {
				t.Errorf("exitCodeGaps[%s] contains %d, but `ngm %s` does not declare it — "+
					"the declaration changed, so the gap entry must go", cmd, g, cmd)
			}
		}
	}

	if measured == 0 {
		t.Fatal("no declared exit code was measured — a net that checks nothing is not a net")
	}
	t.Logf("declared exit codes: %d measured here, %d measured elsewhere, %d known gaps",
		measured, everywhere, gapped)
}

// TestV22DenoIsMissing 测量那几个**只能在"缺 Deno"状态下**触发的码。
//
// 为什么必须单独成测：清空 PATH 是全局且不可逆的（t.Setenv 到本测试结束才还原），
// 而夹具要用 git 建。所以每个子测试自己先建夹具、再清 PATH。
//
// 它也是"不降级"这条安全边界的可执行证据：**宁可退 5，也不在沙箱外执行依赖作者的代码。**
func TestV22DenoIsMissing(t *testing.T) {
	t.Run("verify", func(t *testing.T) {
		proj := v3SandboxProject(t, map[string]string{"verify.js": "Deno.exit(0);\n"})
		t.Setenv("PATH", t.TempDir())
		var out, errb bytes.Buffer
		if got := dispatch([]string{"verify", "--sandbox", "--dir=" + proj}, &out, &errb); got != 5 {
			t.Errorf("`ngm verify` declares exit code 5 for a missing sandbox, measured %d: %s",
				got, firstLine(errb.String()))
		}
	})

	t.Run("install", func(t *testing.T) {
		proj := v3PostInstallProject(t, map[string]string{"postinstall.js": "Deno.exit(0);\n"},
			`{"postInstallPolicy": "allow"}`)
		t.Setenv("PATH", t.TempDir())
		var out, errb bytes.Buffer
		if got := dispatch([]string{"install", "--dir=" + proj}, &out, &errb); got != 5 {
			t.Errorf("`ngm install` declares exit code 5 for a missing sandbox, measured %d: %s",
				got, firstLine(errb.String()))
		}
	})

	t.Run("audit", func(t *testing.T) {
		home := isolateUserEnv(t)
		scUpstream(t, "github:z/hook", "export const a = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:z/hook@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		testutils.WriteFile(t, proj, "hook.js", "Deno.exit(0);\n")

		srv := osvStub(t, `{}`)
		t.Setenv("NGM_OSV_URL", srv.URL)
		grantNetFor(t, home, srv)

		t.Setenv("PATH", t.TempDir())
		var out, errb bytes.Buffer
		args := []string{"audit", "--no-cache", "--hook=" + filepath.Join(proj, "hook.js"), "--dir=" + proj}
		if got := dispatch(args, &out, &errb); got != 5 {
			t.Errorf("`ngm audit` declares exit code 5 for a missing sandbox, measured %d: %s",
				got, firstLine(errb.String()))
		}
	})
}

// buildFixture 按 setup 造一个**独立**的项目目录。
func buildFixture(t *testing.T, home, setup string) string {
	t.Helper()
	switch setup {
	case setupMissing:
		return filepath.Join(t.TempDir(), "no-such-dir")
	case setupEmpty:
		return t.TempDir()
	case setupProject:
		return newProject(t)
	case setupDep:
		p := newProject(t)
		seedDep(t, p)
		return p
	case setupCold:
		p := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:x/cold@v1", "--ref-type=tag", "--dir="+p); code != 0 {
			t.Fatalf("add cold: %s", out)
		}
		return p
	case setupNoMirror:
		p := newProject(t)
		seedDep(t, p)
		if err := os.RemoveAll(mirrorDirForTest(t, "github:x/dep")); err != nil {
			t.Fatal(err)
		}
		return p
	case setupDrift, setupDrifted:
		r := scUpstream(t, "github:x/drift", "export const a = 1\n", "")
		p := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:x/drift@v1", "--ref-type=tag", "--dir="+p); code != 0 {
			t.Fatalf("add drift: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+p); code != 0 {
			t.Fatalf("install drift: %s", out)
		}
		// 上游把 tag 挪到新提交：非预期漂移。
		r.WriteFile("index.ts", "export const a = 2\n")
		r.Commit("fix: move the tag")
		r.Exec("tag", "-f", "v1")
		if setup == setupDrifted {
			writeSupplyChain(t, p, `{"verifyOnLock": true}`)
		}
		return p
	case setupTampered:
		p := newProject(t)
		seedDep(t, p)
		m5TamperLockDigest(t, p)
		return p
	case setupOsvClean:
		p := newProject(t)
		seedDep(t, p)
		srv := osvStub(t, `{}`)
		t.Setenv("NGM_OSV_URL", srv.URL)
		grantNetFor(t, home, srv)
		return p
	case setupOsvVuln:
		p := newProject(t)
		seedDep(t, p)
		srv := osvStub(t, `{"vulns":[{"id":"GHSA-1","summary":"v","severity":"HIGH"}]}`)
		t.Setenv("NGM_OSV_URL", srv.URL)
		grantNetFor(t, home, srv)
		return p
	case setupEngine:
		p := newProject(t)
		testutils.WriteFile(t, p, "a.css", "a{color:red}\n")
		testutils.WriteFile(t, p, "src.ts", "export const a: number = 1\n")
		testutils.WriteFile(t, p, "entry.ts", "export const a = 1\n")
		writeEngineCatalog(t, p,
			engineEntry{Name: "fake", Kind: "typeCheck", Adapter: "subprocess", Command: fakeEngine},
			engineEntry{Name: "fake", Kind: "typeDecl", Adapter: "subprocess", Command: fakeEngine},
			engineEntry{Name: "fake", Kind: "bundle", Adapter: "subprocess", Command: fakeEngine},
			engineEntry{Name: "fake", Kind: "transform", Adapter: "subprocess", Command: fakeEngine},
			engineEntry{Name: "fake", Kind: "css", Adapter: "subprocess", Command: fakeEngine},
		)
		return p
	case setupGhost:
		p := newProject(t)
		testutils.WriteFile(t, p, "a.css", "a{color:red}\n")
		writeEngineCatalog(t, p,
			engineEntry{Name: "ghost", Kind: "typeCheck", Adapter: "subprocess", Command: "ngm-no-such-tool"},
			engineEntry{Name: "ghost", Kind: "typeDecl", Adapter: "subprocess", Command: "ngm-no-such-tool"},
			engineEntry{Name: "ghost", Kind: "bundle", Adapter: "subprocess", Command: "ngm-no-such-tool"},
			engineEntry{Name: "ghost", Kind: "transform", Adapter: "subprocess", Command: "ngm-no-such-tool"},
			engineEntry{Name: "ghost", Kind: "css", Adapter: "subprocess", Command: "ngm-no-such-tool"},
		)
		return p
	}
	t.Fatalf("unknown fixture setup %q", setup)
	return ""
}

// seedDep 给项目声明一个依赖并 install（于是有 lock + vendor）。
func seedDep(t *testing.T, proj string) {
	t.Helper()
	scUpstream(t, "github:x/dep", "export const dep = 1\n", "")
	if code, out := runCaptureCode(t, "add", "github:x/dep@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}
}

// osvStub 起一个固定响应的 OSV 端点替身。
func osvStub(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func lookupMeasured(cmd string, code int) (*exitCase, bool) {
	for i := range exitCodeMeasured {
		if exitCodeMeasured[i].cmd == cmd && exitCodeMeasured[i].code == code {
			return &exitCodeMeasured[i], true
		}
	}
	return nil, false
}

func lookupElsewhere(cmd string, code int) bool {
	for _, c := range exitCodeElsewhere {
		if c.cmd == cmd && c.code == code {
			return true
		}
	}
	return false
}

func lookupGap(cmd string, code int) bool {
	for _, g := range exitCodeGaps[cmd] {
		if g == code {
			return true
		}
	}
	return false
}

// declares 报告 cmd 的用法文本是否声明了 code。
func declares(cmd string, code int) bool {
	for _, spec := range commands {
		if spec.Name != cmd {
			continue
		}
		for _, c := range declaredExitCodes(spec.Usage) {
			if c == code {
				return true
			}
		}
	}
	return false
}

var _ = http.StatusOK
