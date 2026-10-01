package security

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEnforcementPointsAreRegistered 把"每个权限命名空间都有施加点、且都有端到端断言"
// 变成机械检查（与 `internal/config/field_wiring_test.go` 同型）。
//
// ## 为什么需要它
//
// `permissions` 段在 v0.1 就存在，却**从未被施加**——写下 `"deny": ["env:GITHUB_TOKEN"]`
// 不阻止任何事。而"哪些地方有门禁"这件事，在 v0.5 之前一直靠人读文档；
// 本轮 C 组一上来就发现那条文档是**错的**：它写着 `run:git` 的施加点是
// "`git.Run`（所有 git 子进程的出口）"，而包里其实有三个 spawn 出口，
// 其中两个（`RunAllowFailure` / `CatFileBatch`）根本没有门禁。
//
// 所以这张表把三件事变成可失败的断言：
//
//  1. 每个命名空间都必须**登记**：施加点在哪、由哪条测试证明"改了配置，行为就变"
//  2. 登记的测试函数必须**真的存在**（扫仓库源码，不靠名字形状猜）
//  3. 文档的施加点表必须**提到**该命名空间（代码与文档不许各自漂移）
//
// 判据沿用 v0.5 复核的裁定：**"生效" = 改变行为**。只被解析、只被回显的东西不算生效——
// 若某个命名空间确实没有施加点，必须在表里写明裁决与原因，而不是留白。
func TestEnforcementPointsAreRegistered(t *testing.T) {
	type entry struct {
		ns        Namespace
		enforced  string   // 施加点（人读的描述；必须是具体位置，不是"已实现"）
		byTests   []string // 证明"行为会变"的测试函数名
		docMarker string   // 文档里必须出现的写法
	}

	table := []entry{
		{
			ns:       Read,
			enforced: "仅沙箱内：PlanSandbox → Deno --allow-read=<该依赖的子树>；ngm 自身的读取**不加门禁**",
			byTests:  []string{"TestPlanSandbox", "TestV03SandboxRealDeno"},
			// 裁决（v0.5 C 组）：read 默认允许，而"读自己的项目与 vendor"本就是 ngm 的职能；
			// 需要拦下的场景只有沙箱里的他人代码。这条裁决与安全模型的表一致。
			docMarker: "`read:`",
		},
		{
			ns:       Write,
			enforced: "仅沙箱内：Grants.DenoArgs 恒为 --deny-write（从不授予）；ngm 自身的写入**不加门禁**",
			byTests:  []string{"TestPlanSandbox", "TestV03SandboxRealDeno"},
			// 同上：vendor/lock/content store 的写入是 ngm 的职能，没有需要拦下的场景。
			docMarker: "`write:`",
		},
		{
			ns: Net,
			enforced: "①`vendor.Mirror.Ensure`（clone/fetch 的唯一出口）与 `resolve` 的 ls-remote（`git.CheckNetAccess`）；" +
				"②ngm 自己的 HTTP 出口——OSV 查询在发请求前判定（v0.5 C 组补，此前直连 api.osv.dev 无门禁）",
			byTests: []string{
				"TestCheckNetAccess",
				"TestV03PermissionsAcceptance",
				"TestV05AuditNeedsNetPermission",
				"TestQueryOSV_AsksBeforeItReachesOut",
			},
			// 沙箱内的 net 另走 CheckExplicit（必须显式授权），由 PlanSandbox 测。
			docMarker: "`net:",
		},
		{
			ns:       Run,
			enforced: "git 包唯一的子进程构造点 newGitCommand（三个出口共用）+ adapter.Runner 的 preflight",
			byTests: []string{
				"TestRun_DeniedRunGitDoesNotStart",
				"TestSpawn_GateOnEveryExit",
				"TestSpawn_OnlyOneFileBuildsProcesses",
				"TestV03PermissionsAcceptance",
			},
			docMarker: "`run:",
		},
		{
			ns:        Env,
			enforced:  "git 子进程的环境构造（buildEnv 剔除被拒绝的变量；ngm 不读取其值）+ 沙箱的 --allow-env/--deny-env",
			byTests:   []string{"TestBuildEnv_StripsDeniedVariables", "TestV03CredentialDiscipline"},
			docMarker: "`env:",
		},
	}

	repo := repoRoot(t)
	declared := declaredTestFuncs(t, repo)
	doc := readTextFile(t, filepath.Join(repo, "docs", "architecture", "security-model.md"))

	registered := map[Namespace]bool{}
	for _, e := range table {
		registered[e.ns] = true

		if strings.TrimSpace(e.enforced) == "" {
			t.Errorf("%s: 登记了命名空间却没写施加点", e.ns)
		}
		if len(e.byTests) == 0 {
			t.Errorf("%s: 没有一条端到端断言——按 v0.5 的判据它不算生效", e.ns)
		}
		for _, name := range e.byTests {
			if !declared[name] {
				t.Errorf("%s: 登记的测试 %s 在仓库里不存在（名单会腐烂，所以要机械核对）", e.ns, name)
			}
		}
		if !strings.Contains(doc, e.docMarker) {
			t.Errorf("%s: 安全模型的施加点表里找不到 %s——代码与文档必须同时更新", e.ns, e.docMarker)
		}
	}

	// 反向：新增命名空间时**必须**在这里登记。少了这一步，新命名空间会静默地
	// 既没有施加点、也没有断言——正是本检查要防的形态。
	for _, ns := range Namespaces() {
		if !registered[ns] {
			t.Errorf("命名空间 %s 未在施加点表中登记：要么给它一个施加点与断言，"+
				"要么写明裁决与原因", ns)
		}
	}
}

// TestHTTPEgressIsRegistered 登记所有会出现 HTTP 出口的源码文件。
//
// 为什么需要它（v0.5 C 组）：`net:` 的施加点在文档里只列了 git 的两处
// （mirror 的 clone/fetch 与 ls-remote），而 ngm 其实还有自己的 HTTP 客户端——
// OSV 查询当时**没有任何门禁**，一直连到 `api.osv.dev`。那是"文档没写、代码里也没人发现"。
//
// 这条扫描把"新增一个 HTTP 出口"变成**必须登记**的事：不登记就红，
// 而不是等下一个读代码的人去发现。
func TestHTTPEgressIsRegistered(t *testing.T) {
	// 文件 → 由什么门禁（必须具体到函数，不写"已实现"）。
	registered := map[string]string{
		"internal/supplychain/osv.go": "发起请求前调用 cfg.CheckNet(host)，host 取自**实际端点**（默认 api.osv.dev）",
	}

	repo := repoRoot(t)
	importRe := regexp.MustCompile(`(?m)^\s*"net/http"\s*$`)

	found := map[string]bool{}
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(repo, top), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			if !importRe.Match(data) {
				return nil
			}
			rel, rerr := filepath.Rel(repo, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			found[rel] = true
			if _, ok := registered[rel]; !ok {
				t.Errorf("%s imports net/http but is not registered as an egress point: "+
					"every HTTP exit needs a net: gate and a covering test", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", top, err)
		}
	}

	if len(found) == 0 {
		t.Error("no net/http import found at all — this check would pass vacuously")
	}
	for file := range registered {
		if !found[file] {
			t.Errorf("registered egress %s no longer imports net/http; "+
				"remove the registration so the list keeps meaning something", file)
		}
	}
}

// declaredTestFuncs 扫出仓库里**真的存在**的测试函数名。
//
// 刻意扫源码而不是用反射：反射只能看到本包的测试，而这些断言分布在
// internal/git、internal/security 与 cmd/ngm 三个包里。
func declaredTestFuncs(t *testing.T, repo string) map[string]bool {
	t.Helper()
	re := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\s*\(`)
	out := map[string]bool{}

	for _, dir := range []string{filepath.Join(repo, "cmd"), filepath.Join(repo, "internal")} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for _, m := range re.FindAllStringSubmatch(string(data), -1) {
				out[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
	}
	if len(out) < 50 {
		t.Fatalf("only %d test functions found — the scanner is looking in the wrong place", len(out))
	}
	return out
}

// repoRoot 从当前包目录往上找 go.mod。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the package directory")
		}
		dir = parent
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
