package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// surfaceCase 是**按失败路径**组织的一条用例。
//
// 与 v0.30 那张网的区别在**组织方式**，不在断言：
//
//	v0.30：按**声明覆盖**——每对 (命令, 退出码) 一条。于是配置类错误的 21 条用例
//	       **全都是同一个场景**（未知 flag），错误面其实只被碰了一个点。
//	v0.31：按**失败路径**——坏 JSON、缺清单、缺 vendor 树、未知集成、坏 mappings …
//	       每条都是一个用户真的会撞到的入口。
//
// 断言仍然是三条：错误文本必须带建议 · 非零不许静默 · 判据必须还有东西可查。
type surfaceCase struct {
	name  string
	setup func(t *testing.T) string
	args  []string
	// expectZero 非空表示这条路径**预期退 0**，其值是原因（会打进测试输出）。
	//
	// 它有两种用法：**对照组**（健康项目应当通过）与**实测边界**
	// （行为实测如此、且已知为什么如此，但不是"应当如此"）。
	// 后者写进网里的价值是：行为哪天变了，这一条会红，逼着人重新读一遍那条说明。
	expectZero string
}

// surfaceAdviceMust（v0.48）要求某条路径的**建议**至少命中其中一个子串——
// 即"建议要承认**这一种失败**的形状"（v0.34 的原则，落到错误文本这一侧）。
//
// 为什么需要它：`ngm add --path=/etc` 的 cause 说的是"路径必须相对"，
// 而用户看到的建议整句都在讲 `<host>:<org>/<repo>[@<ref>]`——为 path 挨骂，
// 却被告知去检查仓库标识。**没有这张表，那种错位是绿的。**
//
// 为什么是旁表而不是用例里的一个字段：这张表有 20 条用例，全是**位置式**字面量
// （`{name, setup, args, expectZero}`），加字段要动全部 20 条——而其中 19 条
// 并不需要这个断言。旁表的代价是键可能与用例名脱节，所以下面有一条守卫：
// **旁表里的每个键都必须在 surfaceCases 里存在**（名单自己要有牙齿）。
var surfaceAdviceMust = map[string][]string{
	"manifest-field-has-the-wrong-type":   {"config validate"},
	"manifest-dependencies-is-not-a-list": {"config validate"},
	"manifest-schema-version-is-too-new":  {"config validate"},
	"manifest-schema-version-is-absent":   {"config validate"},
	// 这两条是这一版抓到的错位：建议必须提到**相对路径**这件事。
	"sub-path-escapes-the-repository": {"relative"},
	"sub-path-is-absolute":            {"relative"},
}

var surfaceCases = []surfaceCase{
	// ---- 配置类：四种坏法，四条不同的路径 ----
	{"manifest-is-not-json", func(t *testing.T) string {
		p := newProject(t)
		writeSurfaceFile(t, p, "ngm.json", "{")
		return p
	}, []string{"install"}, ""},
	{"manifest-without-a-name", func(t *testing.T) string {
		p := newProject(t)
		writeSurfaceFile(t, p, "ngm.json", `{"runtime":"node"}`)
		return p
	}, []string{"install"}, ""},
	{"manifest-with-an-unknown-runtime", func(t *testing.T) string {
		p := newProject(t)
		writeSurfaceFile(t, p, "ngm.json", `{"name":"github.com:x/app","runtime":"cobol"}`)
		return p
	}, []string{"install"}, ""},
	{"no-manifest-at-all", func(t *testing.T) string { return t.TempDir() },
		[]string{"install"}, ""},
	{"config-validate-on-broken-json", func(t *testing.T) string {
		p := newProject(t)
		writeSurfaceFile(t, p, "ngm.json", "{")
		return p
	}, []string{"config", "validate"}, ""},

	// ---- 锁与落地完整性 ----
	{"lock-is-gone", func(t *testing.T) string {
		p := newProject(t)
		seedDep(t, p)
		if err := os.Remove(filepath.Join(p, "ngm.lock")); err != nil {
			t.Fatal(err)
		}
		return p
	}, []string{"verify"}, ""},
	{"vendored-tree-is-gone", func(t *testing.T) string {
		p := newProject(t)
		seedDep(t, p)
		if err := os.RemoveAll(filepath.Join(p, "ngm.vendor")); err != nil {
			t.Fatal(err)
		}
		return p
	}, []string{"verify"}, ""},
	{"vendored-tree-has-a-stray-file", func(t *testing.T) string {
		p := newProject(t)
		seedDep(t, p)
		writeSurfaceFile(t, p, filepath.Join("ngm.vendor", "github.com", "x", "dep", "stray.txt"), "x")
		return p
	}, []string{"verify"}, ""},
	{"vendored-tree-has-a-stray-file-at-the-root", func(t *testing.T) string {
		p := newProject(t)
		seedDep(t, p)
		writeSurfaceFile(t, p, filepath.Join("ngm.vendor", "stray.txt"), "x")
		return p
	}, []string{"verify"},
		"实测边界（不是对照组，也不是缺陷）：verify 按**每个依赖自己的树**核对，" +
			"落在 `ngm.vendor/` 根、不属于任何依赖的文件不在它的判据里。" +
			"同一个文件落在**依赖树内**则退 2（见上一条）。" +
			"这一条的价值是：行为哪天变了它会红，逼着人重读这段说明"},

	// ---- 依赖图与地址 ----
	{"add-a-malformed-spec", func(t *testing.T) string { return newProject(t) },
		[]string{"add", "???"}, ""},
	{"why-an-undeclared-dependency", func(t *testing.T) string { return newProject(t) },
		[]string{"why", "github:x/never"}, ""},
	{"remove-an-undeclared-dependency", func(t *testing.T) string { return newProject(t) },
		[]string{"remove", "github:x/never"}, ""},

	// ---- 集成与映射 ----
	{"integrations-add-an-unknown-one", func(t *testing.T) string {
		p := newProject(t)
		seedDep(t, p)
		return p
	}, []string{"integrations", "add", "no-such-integration"}, ""},
	{"mappings-file-is-not-json", func(t *testing.T) string {
		p := newProject(t)
		seedDep(t, p)
		writeSurfaceFile(t, p, "ngm.mappings.json", "{ not json")
		return p
	}, []string{"mappings", "validate"}, ""},

	// ---- 取数 ----
	{"tree-osv-offline-without-a-cached-result", func(t *testing.T) string {
		p := newProject(t)
		seedDep(t, p)
		return p
	}, []string{"tree", "--osv", "--offline"}, ""},

	// ---- v0.48 新增：清单的**形状**（v0.38 记下这三种，一直没做；三种都实测过） ----
	//
	// 它们与上面"坏 JSON / 缺字段"是**不同的失败面**：JSON 能解析、字段也在，
	// 只是**值不合形状**。三条各自走到清单校验的不同分支。
	{"manifest-field-has-the-wrong-type", func(t *testing.T) string {
		p := newProject(t)
		writeSurfaceFile(t, p, "ngm.json",
			"{\n  \"schemaVersion\": 1,\n  \"name\": 123,\n  \"runtime\": \"node\"\n}\n")
		return p
	}, []string{"verify"}, ""},

	{"manifest-dependencies-is-not-a-list", func(t *testing.T) string {
		p := newProject(t)
		writeSurfaceFile(t, p, "ngm.json",
			"{\n  \"schemaVersion\": 1,\n  \"name\": \"github.com:x/a\",\n  \"runtime\": \"node\",\n"+
				"  \"dependencies\": \"nope\"\n}\n")
		return p
	}, []string{"install"}, ""},

	{"manifest-schema-version-is-too-new", func(t *testing.T) string {
		p := newProject(t)
		writeSurfaceFile(t, p, "ngm.json",
			"{\n  \"schemaVersion\": 2,\n  \"name\": \"github.com:x/a\",\n  \"runtime\": \"node\"\n}\n")
		return p
	}, []string{"verify"}, ""},

	{"manifest-schema-version-is-absent", func(t *testing.T) string {
		p := newProject(t)
		writeSurfaceFile(t, p, "ngm.json",
			"{\n  \"schemaVersion\": 0,\n  \"name\": \"github.com:x/a\",\n  \"runtime\": \"node\"\n}\n")
		return p
	}, []string{"tree"}, ""},

	{"sub-path-escapes-the-repository", func(t *testing.T) string {
		p := newProject(t)
		scUpstream(t, "github:x/dep", "export const dep = 1\n", "")
		return p
	}, []string{"add", "github:x/dep@v1", "--ref-type=tag", "--path=../escape"}, ""},

	{"sub-path-is-absolute", func(t *testing.T) string {
		p := newProject(t)
		scUpstream(t, "github:x/dep", "export const dep = 1\n", "")
		return p
	}, []string{"add", "github:x/dep@v1", "--ref-type=tag", "--path=/etc"}, ""},

	// ---- 对照组：这条路径**预期成功** ----
	{"verify-a-healthy-project", func(t *testing.T) string {
		p := newProject(t)
		seedDep(t, p)
		return p
	}, []string{"verify"}, "对照组：健康的项目**应当**通过——它不是失败面的一部分，但必须被跑一遍"},
}

func writeSurfaceFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// reErrorName 抽出错误文本的**身份**（显示名），用来数这张网覆盖了几种错误。
var reErrorName = regexp.MustCompile(`^([A-Za-z]+):`)

// minSurfaceFailureCases 是表里**失败用例**的条数，钉住防缩水。
//
// 删掉一条用例 → 这个数对不上 → 红。与 v0.15 的负例名单、v0.22 的缺口名单同一个套路：
// **名单自己要有牙齿**。
// v0.48 起是 20：14 条（v0.31） + 6 条形状（清单字段类型错 · 依赖不是数组 ·
// schemaVersion 太新 / 缺失 · 子路径逃出仓库 · 子路径是绝对路径）。
const minSurfaceFailureCases = 20

// TestV31EveryReachableFailurePathExplainsItself 逐条走**真实失败路径**，
// 断言"用户看到的错误说得清下一步"。
//
// 三条断言（与 v0.30 同一族）：
//
//	① 错误文本必须带 `hint:`——这是"给下一步"的契约；
//	② 非零退出**不许静默**；
//	③ 判据必须还有东西可查：产生的错误身份**至少 4 种**。
//
// 第 ③ 条是这张网自己的可达性守卫。它比 v0.30 的那条更严一点：
// v0.30 只要求"有错误文本"，这里要求**错误身份不止一种**——
// 因为这张网存在的理由正是"v0.30 的错误面只有一种（未知 flag）"。
func TestV31EveryReachableFailurePathExplainsItself(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	identities := map[string]int{}
	silent, reports, failures := 0, 0, 0

	for _, sc := range surfaceCases {
		t.Run(sc.name, func(t *testing.T) {
			isolateUserEnv(t)
			dir := sc.setup(t)
			args := append(append([]string{}, sc.args...), "--dir="+dir)

			var out, errb bytes.Buffer
			code := dispatch(args, &out, &errb)
			stderr := errb.String()

			if sc.expectZero != "" {
				if code != 0 {
					t.Fatalf("this path is expected to exit 0 (%s), got %d:\n%s",
						sc.expectZero, code, stderr)
				}
				t.Logf("exit 0 by design: %s", sc.expectZero)
				return
			}
			failures++
			if code == 0 {
				t.Errorf("this path is listed as a failure but exited 0 — "+
					"either the path changed or the fixture no longer builds the state it names:\n%s", out.String())
				return
			}
			if strings.TrimSpace(stderr+out.String()) == "" {
				silent++
				t.Errorf("exited %d without saying anything — a silent failure is the hardest kind to debug", code)
				return
			}
			m := reErrPrefix.FindStringSubmatch(stderr)
			if m == nil {
				reports++ // 报告式失败：不套这条契约，但要被数到
				return
			}
			identities[m[1]]++
			if !strings.Contains(stderr, "hint:") {
				t.Errorf("exited %d with an error that gives no next step:\n%s", code, stderr)
			}
			// v0.48：**建议要承认这一种失败的形状**（该路径被登记时才要求）。
			// 只要求"有建议"是不够的——一句关于**别处**的建议也是建议。
			if must := surfaceAdviceMust[sc.name]; len(must) > 0 {
				ok := false
				for _, s := range must {
					if strings.Contains(stderr, s) {
						ok = true
						break
					}
				}
				if !ok {
					t.Errorf("the advice does not acknowledge **this** failure's shape "+
						"(expected one of %v in the hint):\n%s", must, stderr)
				}
			}
		})
	}

	// 旁表的键必须在用例表里存在——否则那条登记**什么也没看着**（名单自己要有牙齿）。
	for name := range surfaceAdviceMust {
		found := false
		for _, sc := range surfaceCases {
			if sc.name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("surfaceAdviceMust mentions %q, which is not a case in surfaceCases — "+
				"a stale entry watches nothing", name)
		}
	}

	// ③ 可达性守卫。
	//
	// 第一条是**防缩水**：表里的失败用例数被钉住，删掉一条就会红。
	//
	// 第二条原本写的是"错误身份至少 4 种"——**实测把它否掉了**：
	// 错误文本这一路只有两种身份（`ConfigInvalid` 11 条 + `GitFetch` 1 条），
	// 另有 2 条走**报告**（`verify` 的落地完整性）。这不是网的缺陷，是产品的设计：
	// **"你做错了什么"走错误文本，"这份树为什么不可信"走报告**——
	// 报告能给出逐条细节，错误文本只能说一句话。
	//
	// 所以守卫改成如实的两条：身份**不少于 2 种**、且**必须有报告式失败**。
	// 它挡的是"整个错误面塌缩成一条路"——那才是这张网真正会失效的方式。
	if failures < minSurfaceFailureCases {
		t.Fatalf("only %d failure paths ran, the table has %d — cases were removed",
			failures, minSurfaceFailureCases)
	}
	if len(identities) < 2 || reports == 0 {
		t.Fatalf("the failure surface collapsed: %d identit(y/ies) (%v), %d report-style — "+
			"a surface net that keeps hitting one path is not a surface net",
			len(identities), identities, reports)
	}
	t.Logf("failure surface: %d paths (%d error-text, %d report-style, %d silent), identities: %v",
		failures, failures-reports-silent, reports, silent, identities)
}
