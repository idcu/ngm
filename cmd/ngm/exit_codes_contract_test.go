package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
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
// 第一版把"续行"误判成段结束（用了固定阈值 6），21 个命令里只有 6 个被
// 解析出来——`verify` 只解出 `0 1 2`，于是网对着一份**残缺**的声明对账。
// 这类"判据本身先错了"的失败，比网没抓到东西更危险：它假装已经对过账。
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
	// noDir：cache / store 操作的是全局层，不接受 --dir。
	noDir bool
	// why：仅缺口条目需要——为什么这一段还没有人盯着。
	why string
}

// setup 的取值。
const (
	setupMissing  = "missing"  // 目录不存在
	setupEmpty    = "empty"    // 空目录（没有 ngm.json）
	setupProject  = "project"  // 有 ngm.json、无依赖
	setupDep      = "dep"      // 有依赖 + lock + vendor
	setupCold     = "cold"     // 有依赖声明、但没有本地 mirror、也没有 lock
	setupOsvClean = "osvclean" // 有依赖 + lock + vendor，且 OSV 走本地替身（无发现）
	setupGhost    = "ghost"    // 引擎目录里声明了一个"没装"的引擎
)

// exitCodeMeasured 是**已经对上账**的那部分声明。
//
// 触发命令一律**离线**：要么只看本地 mirror / vendor / lock，要么故意给一个
// "声明了但没装"的引擎名。凡需要真实网络或真实引擎的码，不进这张表——
// 进了就是一张会自己变红的网。
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
	// 依赖已声明但从未 install ⇒ 没有 lock ⇒ --offline 报"资源不在本地" ⇒ 4。
	{cmd: "install", code: 4, args: []string{"--offline"}, setup: setupCold},
	{cmd: "update", code: 4, args: []string{"--offline", "--all"}, setup: setupCold},
	{cmd: "audit", code: 4, args: []string{"--offline"}, setup: setupDep},

	// ---- 5：引擎不可用（引擎目录里声明了，但命令不存在）----
	// 注意区分两个码：名字**不在目录里** ⇒ 3（配置错误）；
	// 名字在目录里但**命令没装** ⇒ 5（引擎不可用）。
	{cmd: "typecheck", code: 5, args: []string{"--engine=ghost"}, setup: setupGhost},
	{cmd: "typedecl", code: 5, args: []string{"--outdir=types", "--engine=ghost"}, setup: setupGhost},
	{cmd: "build", code: 5, args: []string{"--engine=ghost"}, setup: setupGhost},
	{cmd: "transform", code: 5, args: []string{"--loader=ts", "--engine=ghost"}, setup: setupGhost},
	{cmd: "css", code: 5, args: []string{"a.css", "--engine=ghost"}, setup: setupGhost},
	{cmd: "engines", code: 5, args: []string{"validate"}, setup: setupGhost},
}

// exitCodeGaps 是**已知还没被测量**的声明（本网故意不覆盖的那部分）。
//
// 与 v0.15 的负例名单同一个套路：**名单自己会过期**。
// 每条都断言"该命令确实还声明着这个码"——哪天那段声明被删改，这一条 gap
// 就会变红，逼着人把它一起删掉。
//
// 这些码之所以没被测量，是因为它们的触发条件**需要真实引擎或真实网络**
// （引擎返回诊断、上游 tag 被移动、vendor 字节被篡改、postinstall 钩子失败、
// 超阈值漏洞）。把它们做成网会违反"网必须确定性、离线"这条纪律。
var exitCodeGaps = map[string][]int{
	"install":      {1, 2, 5},
	"update":       {1, 2, 5},
	"verify":       {1, 2, 4, 5},
	"audit":        {1, 5},
	"tree":         {1, 4},
	"typecheck":    {0, 1},
	"typedecl":     {0, 1},
	"build":        {0, 1},
	"transform":    {0, 1},
	"css":          {0, 1},
	"mappings":     {0},
	"integrations": {0},
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

// TestV21DeclaredCodesAreMeasuredOrKnownGaps 是本网的牙齿。
//
// 它要求：**每个命令声明的每个码，要么有一条能离线触发它的实测，
// 要么在缺口名单里。** 于是以后任何人新声明一个码，都必须同时回答——
// 怎么证明它真的会退这个码。否则测试红。
func TestV21DeclaredCodesAreMeasuredOrKnownGaps(t *testing.T) {
	measured := 0
	gapped := 0

	for _, spec := range commands {
		for _, code := range declaredExitCodes(spec.Usage) {
			trig, ok := lookupMeasured(spec.Name, code)
			if !ok {
				if lookupGap(spec.Name, code) {
					gapped++
				} else {
					t.Errorf("`ngm %s` declares exit code %d: neither measured nor a known gap — "+
						"add the trigger or record the gap", spec.Name, code)
				}
				continue
			}

			// 每个用例都**重新隔离一次环境**并造自己的项目目录：
			// 共用一个会让前一个命令的副作用（写出的 lock、OSV 缓存、
			// 甚至 t.Setenv 残留的端点）改变后一个的读数。探针连踩两次这类坑。
			home := isolateUserEnv(t)
			args := append([]string{spec.Name}, trig.args...)
			if !trig.noDir {
				args = append(args, "--dir="+buildFixture(t, trig.setup))
			}
			if trig.setup == setupOsvClean {
				srv := osvStub(t, `{}`)
				t.Setenv("NGM_OSV_URL", srv.URL)
				grantNetFor(t, home, srv)
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
	t.Logf("declared exit codes: %d measured, %d recorded as known gaps", measured, gapped)
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

// buildFixture 按 setup 造一个**独立**的项目目录。
func buildFixture(t *testing.T, setup string) string {
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
	case setupOsvClean:
		p := newProject(t)
		seedDep(t, p)
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

func lookupGap(cmd string, code int) bool {
	for _, g := range exitCodeGaps[cmd] {
		if g == code {
			return true
		}
	}
	return false
}
