package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// TestV02ObservabilityAcceptance 是 v0.2 D 组的可执行验收。
//
// 每条断言都对应 observability.md 里的一句承诺，而不是"命令能跑"：
// 树要体现层级、漂移要被标出、没查漏洞时**不许**暗示查过。
func TestV02ObservabilityAcceptance(t *testing.T) {
	t.Run("tree shows who pulled in whom", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:obs/leaf", "export const leaf = 1\n", "")
		midManifest := `{"name":"obs-mid","version":"1.0.0","runtime":"node",` +
			`"dependencies":[{"name":"github:obs/leaf","ref":"v1","refType":"tag"}]}`
		scUpstream(t, "github:obs/mid", "export const mid = 1\n", midManifest)

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/mid@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "tree", "--dir="+proj)
		if code != 0 {
			t.Fatalf("tree exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "github:obs/mid") {
			t.Fatalf("tree should list the declared dependency:\n%s", out)
		}
		if !strings.Contains(out, "github:obs/leaf") {
			t.Errorf("tree should show the transitive dependency:\n%s", out)
		}
		// 层级必须由缩进体现：叶节点比中间节点深一层
		if !strings.Contains(out, "│   github:obs/leaf") {
			t.Errorf("the leaf must be nested one level deeper than mid:\n%s", out)
		}
	})

	t.Run("tree --json carries the same topology", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:obs/leaf2", "export const leaf2 = 1\n", "")
		midManifest := `{"name":"obs-mid2","version":"1.0.0","runtime":"node",` +
			`"dependencies":[{"name":"github:obs/leaf2","ref":"v1","refType":"tag"}]}`
		scUpstream(t, "github:obs/mid2", "export const mid2 = 1\n", midManifest)

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/mid2@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "tree", "--json", "--dir="+proj)
		if code != 0 {
			t.Fatalf("tree --json exit=%d:\n%s", code, out)
		}
		var parsed struct {
			Dependencies int `json:"dependencies"`
			Entries      []struct {
				Name     string `json:"name"`
				Children []struct {
					Name string `json:"name"`
				} `json:"children"`
			} `json:"entries"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("--json must be machine readable: %v\n%s", err, out)
		}
		if parsed.Dependencies != 2 {
			t.Errorf("dependencies = %d, want 2", parsed.Dependencies)
		}
		if len(parsed.Entries) != 1 || len(parsed.Entries[0].Children) != 1 {
			t.Fatalf("expected one root with one child, got %+v", parsed.Entries)
		}
		if parsed.Entries[0].Children[0].Name != "github:obs/leaf2" {
			t.Errorf("child = %q, want github:obs/leaf2", parsed.Entries[0].Children[0].Name)
		}
	})

	// 漂移是 tree 的核心价值：ref 被重打后，lock 里那个 commit 已经不再被指向。
	t.Run("tree marks drift when the ref moved away from the lock", func(t *testing.T) {
		isolateUserEnv(t)

		r := scUpstream(t, "github:obs/moving", "export const a = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/moving@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		// 上游把 v1 挪到新 commit（这正是 ngm 要防的一类事）
		r.WriteFile("index.ts", "export const a = 2\n")
		r.Commit("feat: move on")
		r.Exec("tag", "-d", "v1")
		r.Tag("v1", false)
		// mirror 已存在时 EnsureLocal 不会更新，必须先删掉再重种
		layout, err := vendor.DefaultLayout()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(vendor.NewMirror(layout.MirrorRoot(), git.Options{}).
			PathFor(resolve.MustNormalize("github:obs/moving"))); err != nil {
			t.Fatal(err)
		}
		seedMirror(t, "github:obs/moving", r.Dir)

		code, out := runCaptureCode(t, "tree", "--dir="+proj)
		if code != 0 {
			t.Fatalf("tree exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "漂移") {
			t.Errorf("a moved tag must be marked as drift:\n%s", out)
		}
		if !strings.Contains(out, "locked ") {
			t.Errorf("the drift marker should also show the locked commit:\n%s", out)
		}
	})

	// 这条固定的是一条纪律：没查过漏洞就**不许**暗示"没漏洞"。
	t.Run("tree without --osv claims nothing about vulnerabilities", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:obs/quiet", "export const q = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/quiet@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "tree", "--dir="+proj)
		if code != 0 {
			t.Fatalf("tree exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "not consulted") {
			t.Errorf("without --osv the report must say vulnerability data was not consulted:\n%s", out)
		}
		if strings.Contains(out, "✗") {
			t.Errorf("without --osv there must be no vulnerability marker:\n%s", out)
		}
	})

	t.Run("why traces a transitive dependency back to the root", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:obs/wleaf", "export const wl = 1\n", "")
		midManifest := `{"name":"obs-wmid","version":"1.0.0","runtime":"node",` +
			`"dependencies":[{"name":"github:obs/wleaf","ref":"v1","refType":"tag"}]}`
		scUpstream(t, "github:obs/wmid", "export const wm = 1\n", midManifest)

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/wmid@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "why", "github:obs/wleaf", "--dir="+proj)
		if code != 0 {
			t.Fatalf("why exit=%d:\n%s", code, out)
		}
		// 路径必须体现"谁把它带进来的"，而不只是"它在图里"
		if !strings.Contains(out, "github:obs/wmid@v1 → github:obs/wleaf@v1") {
			t.Errorf("why should show the full path from ngm.json:\n%s", out)
		}
		if !strings.Contains(out, "直接依赖：否") {
			t.Errorf("wleaf is transitive, not declared by ngm.json:\n%s", out)
		}
		if !strings.Contains(out, "archiveDigest:") {
			t.Errorf("why should show what is pinned for it:\n%s", out)
		}
	})

	t.Run("why says so when the dependency is declared by the root", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:obs/wroot", "export const wr = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/wroot@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "why", "github:obs/wroot", "--dir="+proj)
		if code != 0 {
			t.Fatalf("why exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "直接依赖：ngm.json 声明") {
			t.Errorf("a root-declared dependency must be reported as such:\n%s", out)
		}
	})

	// 问一个不在图里的依赖必须明确报错（exit 3），而不是输出空结果让人以为"没人依赖它"
	t.Run("why fails clearly for a dependency that is not in the graph", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:obs/only", "export const o = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/only@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "why", "github:obs/absent", "--dir="+proj)
		if code != 3 {
			t.Fatalf("an unknown dependency must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "not in the dependency graph") {
			t.Errorf("the error should name the reason:\n%s", out)
		}
	})

	t.Run("why --json is machine readable", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:obs/jleaf", "export const jl = 1\n", "")
		midManifest := `{"name":"obs-jmid","version":"1.0.0","runtime":"node",` +
			`"dependencies":[{"name":"github:obs/jleaf","ref":"v1","refType":"tag"}]}`
		scUpstream(t, "github:obs/jmid", "export const jm = 1\n", midManifest)

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/jmid@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "why", "github:obs/jleaf", "--json", "--dir="+proj)
		if code != 0 {
			t.Fatalf("why --json exit=%d:\n%s", code, out)
		}
		var parsed struct {
			Name  string     `json:"name"`
			Paths [][]string `json:"paths"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("--json must be machine readable: %v\n%s", err, out)
		}
		if parsed.Name != "github:obs/jleaf" {
			t.Errorf("name = %q", parsed.Name)
		}
		if len(parsed.Paths) != 1 || len(parsed.Paths[0]) != 3 {
			t.Errorf("expected one 3-element path (ngm.json → mid → leaf), got %v", parsed.Paths)
		}
	})

	t.Run("outdated finds a newer tag", func(t *testing.T) {
		isolateUserEnv(t)

		r := testutils.NewGitRepo(t)
		r.WriteFile("index.ts", "export const v1 = 1\n")
		r.Commit("feat: v1")
		r.Tag("v1.0.0", false)
		r.WriteFile("index.ts", "export const v2 = 1\n")
		r.Commit("feat: v2")
		r.Tag("v1.1.0", false)
		seedMirror(t, "github:obs/ver", r.Dir)

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/ver@v1.0.0", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "outdated", "--dir="+proj)
		if code != 0 {
			t.Fatalf("outdated exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "v1.1.0") {
			t.Errorf("the newer tag must be reported:\n%s", out)
		}
		if !strings.Contains(out, "yes") {
			t.Errorf("a newer tag must be marked as an update:\n%s", out)
		}
	})

	t.Run("outdated says a commit ref has nothing to compare", func(t *testing.T) {
		isolateUserEnv(t)

		r := scUpstream(t, "github:obs/pinned", "export const p = 1\n", "")
		head := r.Head()
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/pinned@"+head, "--ref-type=commit", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "outdated", "--dir="+proj)
		if code != 0 {
			t.Fatalf("outdated exit=%d:\n%s", code, out)
		}
		if strings.Contains(out, "yes") {
			t.Errorf("a commit ref cannot have an update:\n%s", out)
		}
	})

	// 这条是 outdated 最该被固定的纪律：离线查不到时必须是 unknown，不能写成 no。
	t.Run("outdated reports unknown instead of up-to-date when it cannot look", func(t *testing.T) {
		isolateUserEnv(t)

		r := testutils.NewGitRepo(t)
		r.WriteFile("index.ts", "export const b = 1\n")
		r.Commit("feat: b")
		branch := strings.TrimSpace(r.Exec("rev-parse", "--abbrev-ref", "HEAD"))
		seedMirror(t, "github:obs/branchy", r.Dir)

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/branchy@"+branch, "--ref-type=branch", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "outdated", "--offline", "--dir="+proj)
		if code != 0 {
			t.Fatalf("outdated exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "unknown") {
			t.Errorf("an unchecked branch must be reported as unknown:\n%s", out)
		}
		if !strings.Contains(out, "not 'up to date'") {
			t.Errorf("the report should say unknown is not the same as up to date:\n%s", out)
		}
	})

	// --offline 的另一半：**不许触网**。上面那条只证明"查不到时报 unknown"，
	// 而当时的实现仍然会把在线版 EnsureMirror 交给检查器——mirror 在就 fetch、
	// 不在就 clone，也就是说 `--offline` 被绕过了，而检查器那一侧看不到它
	// （CheckOutdated 只在 branch 分支才读 Offline）。失败时用户看到的是
	// `mirror unavailable: <net 权限错误>`，指向 mirror 可用性——一个错误的方向。
	t.Run("outdated --offline refuses to reach for a cold mirror", func(t *testing.T) {
		isolateUserEnv(t)
		testutils.MustHaveGit(t)

		r := testutils.NewGitRepo(t)
		r.WriteFile("index.ts", "export const cold = 1\n")
		r.Commit("feat: cold")
		r.Tag("v1.0.0", false)
		seedMirror(t, "github:obs/cold", r.Dir)

		proj := newProject(t)
		// 用 **tag** 而不是 branch：branch 在 --offline 下会短路（直接报 unknown，
		// 这正是上一条用例固定的行为），因此碰不到 EnsureMirror；
		// tag 必须读 mirror 才能列出标签——那才是此前会偷偷 fetch 的路径。
		if code, out := runCaptureCode(t, "add", "github:obs/cold@v1.0.0", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		// 把 mirror 删掉：本机不再有这个仓库的任何本地副本。
		layout, lerr := vendor.DefaultLayout()
		if lerr != nil {
			t.Fatal(lerr)
		}
		mirror := vendor.NewMirror(layout.MirrorRoot(), git.Options{})
		if rerr := os.RemoveAll(mirror.PathFor(resolve.MustNormalize("github:obs/cold"))); rerr != nil {
			t.Fatal(rerr)
		}

		// 退出码仍是 0：outdated 是**报告**，不设门禁（见它的 EXIT CODES）。
		// 要固定的是两件事：不触网、且说得出为什么是 unknown。
		code, out := runCaptureCode(t, "outdated", "--offline", "--dir="+proj)
		if code != 0 {
			t.Fatalf("outdated reports; it does not gate. exit=%d\n%s", code, out)
		}
		if !strings.Contains(out, "unknown") {
			t.Errorf("a cold mirror under --offline is unknown, never 'no update':\n%s", out)
		}
		// 这一条是有牙齿的那一条：只有 offlineEnsureMirror 会产出 "no local mirror"。
		// 换回在线版 EnsureMirror 时，note 会变成一次 clone 的失败原因（而且真的会去连网）。
		if !strings.Contains(out, "no local mirror") {
			t.Errorf("the report must name the real cause (a missing local mirror) instead of fetching:\n%s", out)
		}
	})

	t.Run("outdated --json is machine readable", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:obs/jver", "export const jv = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:obs/jver@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "outdated", "--json", "--dir="+proj)
		if code != 0 {
			t.Fatalf("outdated --json exit=%d:\n%s", code, out)
		}
		var parsed struct {
			Dependencies int `json:"dependencies"`
			Entries      []struct {
				Name   string `json:"name"`
				Stale  bool   `json:"stale"`
				Update bool   `json:"update"`
			} `json:"entries"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("--json must be machine readable: %v\n%s", err, out)
		}
		if parsed.Dependencies != 1 || len(parsed.Entries) != 1 {
			t.Fatalf("unexpected payload: %+v", parsed)
		}
		if parsed.Entries[0].Name != "github:obs/jver" {
			t.Errorf("name = %q", parsed.Entries[0].Name)
		}
	})
}
