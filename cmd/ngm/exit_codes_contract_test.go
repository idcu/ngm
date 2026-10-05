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
	{cmd: "verify", code: 5, why: "TestV22DenoIsMissing/verify"},
	{cmd: "install", code: 5, why: "TestV22DenoIsMissing/install"},
	{cmd: "audit", code: 5, why: "TestV22DenoIsMissing/audit"},
}

// exitCodeGaps 是**已知还没被测量**的声明。
//
// 与 v0.15 的负例名单同一个套路：**名单自己会过期**。
// 每条都断言"该命令确实还声明着这个码"——哪天那段声明被删改，这一条 gap
// 就会变红，逼着人把它一起删掉。
var exitCodeGaps = map[string][]int{
	// update 的三个码目前测不到，且**每一条都带着实测记录**：
	//   1 —— 声明说"verifyOnLock 开启且更新后的复检发现漂移"。实测：漂移 + verifyOnLock
	//        下 `update --all` 退 **0**——因为 update 本来就把漂移**修好**（重新解析并锁定），
	//        更新之后不再有漂移。要触发这条得让"更新后复检仍失败"（例如策略拒绝图）。
	//   2 —— "postinstall 钩子失败"需要 Deno **在**；"vendored bytes did not verify"
	//        实测走不到：篡改 lock 或篡改 vendor 字节后 update 都退 0（它**重建**
	//        lock 与 vendor，而不是校验既有的）。
	//   5 —— 需要 Deno 缺失，而 update 运行时要 git，清空 PATH 会先把它打到 4。
	"update": {1, 2, 5},
}

// TestV21UsageExitCodeSectionsAreWellFormed 固定：**每个命令都写下了自己的退出码**，
// 且那段文本**格式正确、机器可解析**。
//
// 为什么它值得一张网：`ngm <cmd> --help` 是脚本作者唯一会读的东西。
// 段里写一个**不存在的码**（比如 6）比不写更糟——它看起来像承诺。
func TestV21UsageExitCodeSectionsAreWellFormed(t *testing.T) {
	if len(commands) == 0 {
		t.Fatal("the command table is empty — the net would pass vacuously")
	}

	for _, spec := range commands {
		t.Run(spec.Name, func(t *testing.T) {
			codes := declaredExitCodes(spec.Usage)
			if len(codes) == 0 {
				t.Fatalf("`ngm %s` has no EXIT CODES section, or it lists no code", spec.Name)
			}
			seen := map[int]bool{}
			for _, c := range codes {
				if c < 0 || c > 5 {
					t.Errorf("`ngm %s` declares exit code %d; the global contract defines 0..5",
						spec.Name, c)
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
