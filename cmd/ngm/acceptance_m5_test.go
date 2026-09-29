package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// m5Project 建一个项目并声明给定依赖。
//
// 每个 dep 形如 `github:m5/lib main branch`：slug + ref + refType。
// 与 m3Project 的区别是 refType 可变——M5 的漂移分类必须覆盖两种语义：
// tag 不许移动（移动即非预期），branch 允许快进（预期）但不容忍改写（非预期）。
func m5Project(t *testing.T, deps ...string) string {
	t.Helper()
	proj := t.TempDir()
	if code, out := runCaptureCode(t, "init", "github.com:m5-test/app", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init: %s", out)
	}
	for _, d := range deps {
		fields := strings.Fields(d)
		if len(fields) != 3 {
			t.Fatalf("bad fixture dep %q: want `<slug> <ref> <refType>`", d)
		}
		spec := fields[0] + "@" + fields[1]
		if code, out := runCaptureCode(t, "add", spec, "--ref-type="+fields[2], "--dir="+proj); code != 0 {
			t.Fatalf("add %s: %s", spec, out)
		}
	}
	return proj
}

// m5Upstream 建一个上游 fixture（一个 commit + 一个 v1 tag）并预置 mirror。
//
// 后续 testutils 对 up 的改动（新增 commit、重打 tag、amend）会通过真实
// `git fetch` 进入 mirror——ngm 不依赖任何网络，全部走本地路径。
func m5Upstream(t *testing.T, slug, body string) *testutils.GitRepo {
	t.Helper()
	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", body)
	r.Commit("feat: " + slug)
	r.Tag("v1", false)
	seedMirror(t, slug, r.Dir)
	return r
}

// m5Install 跑一次 install，返回项目目录与指定依赖的 lock 条目。
func m5Install(t *testing.T, proj, slug string) lock.Dependency {
	t.Helper()
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}
	lf, err := lock.Read(lock.Find(proj))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if lf == nil {
		t.Fatalf("install must write ngm.lock")
	}
	d, ok := lf.Find(slug, "")
	if !ok {
		t.Fatalf("%s missing from lock:\n%s", slug, readLockFile(t, proj))
	}
	return *d
}

// m5ContentFile 返回某 digest 的内容树中某个文件的绝对路径。
func m5ContentFile(t *testing.T, digest, rel string) string {
	t.Helper()
	layout := defaultLayoutForTest(t)
	return filepath.Join(vendor.NewContentStore(layout.ContentRoot()).TreePath(digest), filepath.FromSlash(rel))
}

// m5VendorFile 返回 vendor 树中某个文件的绝对路径。
func m5VendorFile(proj, vendorPath, subPath, rel string) string {
	return filepath.Join(proj, vendor.VendorDirName,
		filepath.FromSlash(vendor.VendorPathFor(vendorPath, subPath)), filepath.FromSlash(rel))
}

var lockDigestField = regexp.MustCompile(`"archiveDigest": "sha256:[0-9a-f]{64}"`)

// m5TamperLockDigest 把 ngm.lock 里的 archiveDigest 改成全零（形状合法、内容不可能匹配）。
func m5TamperLockDigest(t *testing.T, proj string) {
	t.Helper()
	path := lock.Find(proj)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old := lockDigestField.FindString(string(raw))
	if old == "" {
		t.Fatalf("no archiveDigest field in ngm.lock:\n%s", raw)
	}
	repl := `"archiveDigest": "sha256:` + strings.Repeat("0", 64) + `"`
	out := strings.Replace(string(raw), old, repl, 1)
	if out == string(raw) {
		t.Fatal("failed to tamper the lock")
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestM5Acceptance 是 development/v0.1-plan.md 中 M5 阶段的**可执行验收**。
//
// 逐条对应 M5 的验收标准：
//
//  1. 退出码矩阵全绿（5 类场景 × 开关组合的代表性子集）
//  2. 预期更新默认不阻断（exit 0 + driftKind: expected）；篡改 content → exit 2
//  3. README 四问 #2 实测：branch 前进与 tag 重打可区分
func TestM5Acceptance(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	// =====================================================================
	// 基线：干净树
	// =====================================================================
	t.Run("clean tree passes with exit 0", func(t *testing.T) {
		m5Upstream(t, "github:m5/clean", "export const clean = 1\n")
		proj := m5Project(t, "github:m5/clean v1 tag")
		m5Install(t, proj, "github:m5/clean")

		code, out := runCaptureCode(t, "verify", "--dir="+proj)
		if code != 0 {
			t.Fatalf("verify exit=%d out=%s", code, out)
		}
		for _, want := range []string{"✓", "github:m5/clean@v1 (tag)", "match", "exit 0"} {
			if !strings.Contains(out, want) {
				t.Errorf("output should contain %q:\n%s", want, out)
			}
		}

		// --deep 在干净树上必须同样通过；否则它就是个不可用的开关
		if code, out := runCaptureCode(t, "verify", "--deep", "--dir="+proj); code != 0 {
			t.Fatalf("verify --deep on a clean tree exit=%d out=%s", code, out)
		}
	})

	// =====================================================================
	// 场景一：branch 前进 → expected → exit 0（--strict 升级为 1）
	// =====================================================================
	t.Run("branch advanced is an expected update", func(t *testing.T) {
		up := m5Upstream(t, "github:m5/moving", "v1\n")
		first := up.RevParse("HEAD")

		proj := m5Project(t, "github:m5/moving main branch")
		m5Install(t, proj, "github:m5/moving")

		// 分支快进一个 commit
		up.WriteFile("index.ts", "v2\n")
		second := up.Commit("v2")
		if second == first {
			t.Fatal("fixture did not advance the branch")
		}

		code, out := runCaptureCode(t, "verify", "--dir="+proj)
		if code != 0 {
			t.Fatalf("an expected update must not block by default; exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, "driftKind: expected") {
			t.Errorf("output should classify it as expected:\n%s", out)
		}
		if !strings.Contains(out, "ngm update github:m5/moving") {
			t.Errorf("output should point at `ngm update`:\n%s", out)
		}

		// --strict：预期更新也升级为失败
		if code, out := runCaptureCode(t, "verify", "--strict", "--dir="+proj); code != 1 {
			t.Errorf("--strict must upgrade an expected update to exit 1, got %d:\n%s", code, out)
		}

		// --json：CI 消费的字段必须齐全且可解析
		jcode, jout := runCaptureCode(t, "verify", "--json", "--dir="+proj)
		if jcode != 0 {
			t.Fatalf("--json exit=%d out=%s", jcode, jout)
		}
		var rep struct {
			Version      int `json:"version"`
			Dependencies []struct {
				Name           string `json:"name"`
				RefType        string `json:"refType"`
				Commit         string `json:"commit"`
				ResolvedCommit string `json:"resolvedCommit"`
				DriftKind      string `json:"driftKind"`
			} `json:"dependencies"`
			Summary struct {
				Total    int `json:"total"`
				Expected int `json:"expected"`
				ExitCode int `json:"exitCode"`
			} `json:"summary"`
		}
		if err := json.Unmarshal([]byte(jout), &rep); err != nil {
			t.Fatalf("--json is not valid JSON: %v\n%s", err, jout)
		}
		if rep.Version != 1 {
			t.Errorf("report version=%d want 1", rep.Version)
		}
		if len(rep.Dependencies) != 1 {
			t.Fatalf("dependencies=%d want 1", len(rep.Dependencies))
		}
		d := rep.Dependencies[0]
		if d.DriftKind != "expected" {
			t.Errorf("driftKind=%q want expected", d.DriftKind)
		}
		if d.Commit != first || d.ResolvedCommit != second {
			t.Errorf("commit=%s resolvedCommit=%s; want %s / %s", d.Commit, d.ResolvedCommit, first, second)
		}
		if rep.Summary.Expected != 1 || rep.Summary.ExitCode != 0 {
			t.Errorf("summary=%+v", rep.Summary)
		}
	})

	// =====================================================================
	// 场景二：tag 被重打 → unexpected → exit 1（--allow-drift 降级为 0）
	// =====================================================================
	t.Run("moved tag is unexpected drift", func(t *testing.T) {
		up := m5Upstream(t, "github:m5/retag", "v1\n")

		proj := m5Project(t, "github:m5/retag v1 tag")
		m5Install(t, proj, "github:m5/retag")

		// 同一 tag 指向新 commit
		up.WriteFile("index.ts", "v1 rewritten\n")
		up.Commit("v1 rewritten")
		up.ForceRetag("", "v1", false)

		code, out := runCaptureCode(t, "verify", "--dir="+proj)
		if code != 1 {
			t.Fatalf("a moved tag must exit 1, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "driftKind: unexpected") {
			t.Errorf("output should classify it as unexpected:\n%s", out)
		}
		if !strings.Contains(out, "tag v1 was moved") {
			t.Errorf("output should explain the tag move:\n%s", out)
		}

		// --allow-drift：非预期漂移降级为 0
		if code, out := runCaptureCode(t, "verify", "--allow-drift", "--dir="+proj); code != 0 {
			t.Errorf("--allow-drift must downgrade unexpected drift to exit 0, got %d:\n%s", code, out)
		}

		// --strict 不影响 unexpected 的降级语义（作用于不同分类）
		if code, _ := runCaptureCode(t, "verify", "--allow-drift", "--strict", "--dir="+proj); code != 0 {
			t.Errorf("--allow-drift should still win for unexpected drift, got %d", code)
		}
	})

	// =====================================================================
	// 场景三：分支历史被改写（force push）→ unexpected → exit 1
	//
	// 与场景一的区别只在"旧 commit 是否是新 commit 的祖先"。两者可观测现象
	// 完全相同（ref 指向了别的 commit），所以这是 fast-forward 判定的专项验收。
	// =====================================================================
	t.Run("rewritten branch history is unexpected drift", func(t *testing.T) {
		up := m5Upstream(t, "github:m5/rewrite", "v1\n")

		proj := m5Project(t, "github:m5/rewrite main branch")
		m5Install(t, proj, "github:m5/rewrite")

		// 保留旧 commit 可达（模拟 force push 后旧提交仍在对象库里），
		// 再 amend 分支顶端 —— 新 tip 不以旧 tip 为祖先。
		up.Exec("branch", "keep-old")
		up.WriteFile("index.ts", "amended\n")
		up.Exec("commit", "--amend", "-q", "-m", "amended tip")

		code, out := runCaptureCode(t, "verify", "--dir="+proj)
		if code != 1 {
			t.Fatalf("a rewritten branch must exit 1, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "driftKind: unexpected") {
			t.Errorf("output should classify it as unexpected:\n%s", out)
		}
		if !strings.Contains(out, "was rewritten") && !strings.Contains(out, "not an ancestor") {
			t.Errorf("output should explain that fast-forward could not be proven:\n%s", out)
		}
	})

	// =====================================================================
	// 场景四：被声明的 ref 在上游消失（删除/改名）→ unexpected → exit 1
	// =====================================================================
	t.Run("vanished ref is unexpected drift", func(t *testing.T) {
		up := m5Upstream(t, "github:m5/renamed", "v1\n")

		proj := m5Project(t, "github:m5/renamed main branch")
		m5Install(t, proj, "github:m5/renamed")

		up.Exec("branch", "-m", "main", "renamed")

		code, out := runCaptureCode(t, "verify", "--dir="+proj)
		if code != 1 {
			t.Fatalf("a vanished ref must exit 1, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "driftKind: unexpected") {
			t.Errorf("output should classify it as unexpected:\n%s", out)
		}
	})

	// =====================================================================
	// 场景五：digest 重放不匹配 → critical → exit 2
	// =====================================================================
	t.Run("digest replay mismatch is critical", func(t *testing.T) {
		m5Upstream(t, "github:m5/claim", "v1\n")
		proj := m5Project(t, "github:m5/claim v1 tag")
		m5Install(t, proj, "github:m5/claim")

		m5TamperLockDigest(t, proj)

		code, out := runCaptureCode(t, "verify", "--dir="+proj)
		if code != 2 {
			t.Fatalf("a digest replay mismatch must exit 2, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "driftKind: critical") {
			t.Errorf("output should classify it as critical:\n%s", out)
		}
		// --allow-drift 不得把完整性失败降级（observability.md：critical 禁止构建）
		if code, out := runCaptureCode(t, "verify", "--allow-drift", "--dir="+proj); code != 2 {
			t.Errorf("--allow-drift must not downgrade a critical failure, got %d:\n%s", code, out)
		}
	})

	// =====================================================================
	// 场景六：vendor 文件被删除 → critical → exit 2（默认模式即可发现）
	// =====================================================================
	t.Run("deleted vendor file is critical by default", func(t *testing.T) {
		m5Upstream(t, "github:m5/gone", "v1\n")
		proj := m5Project(t, "github:m5/gone v1 tag")
		d := m5Install(t, proj, "github:m5/gone")

		target := m5VendorFile(proj, d.VendorPath, d.SubPath, "index.ts")
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}

		code, out := runCaptureCode(t, "verify", "--dir="+proj)
		if code != 2 {
			t.Fatalf("a missing vendor file must exit 2, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "missing from vendor") {
			t.Errorf("output should name the missing file:\n%s", out)
		}
	})

	// =====================================================================
	// 场景七：content store 被等长篡改
	//
	// 这是 --deep 存在的理由，也是本测试最有价值的一条：
	// 默认 layout 下 vendor 与 content 是同一 inode，就地篡改会同时改变两侧，
	// 因此"vendor ↔ content 比对"看不出异常；只有 --deep 对着 lock 重放
	// digest 才能发现。两种模式的行为都被显式固定，避免未来有人把
	// --deep 做成装饰品、或误以为默认模式已经覆盖了字节级篡改。
	// =====================================================================
	t.Run("same-size content tampering requires --deep", func(t *testing.T) {
		const original = "export const v = 1\n"
		const tampered = "export const v = 2\n" // 与 original 等长（19 字节）

		m5Upstream(t, "github:m5/store", original)
		proj := m5Project(t, "github:m5/store v1 tag")
		d := m5Install(t, proj, "github:m5/store")

		p := m5ContentFile(t, d.ArchiveDigest, "index.ts")
		orig, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(orig) != original || len(tampered) != len(original) {
			t.Fatalf("fixture drift: read %q (len %d), want %q (len %d)",
				orig, len(orig), original, len(original))
		}
		if err := os.WriteFile(p, []byte(tampered), 0o644); err != nil {
			t.Fatal(err)
		}

		// 默认（结构级）校验看不见等长篡改——这是被文档明确的代价取舍
		if code, out := runCaptureCode(t, "verify", "--dir="+proj); code != 0 {
			t.Errorf("the default (structure-level) check is not expected to catch a same-size edit; got %d:\n%s", code, out)
		}

		// --deep 必须发现
		code, out := runCaptureCode(t, "verify", "--deep", "--dir="+proj)
		if code != 2 {
			t.Fatalf("--deep must catch a same-size content tampering (exit 2), got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "digest mismatch") && !strings.Contains(out, "content differs") {
			t.Errorf("output should explain the mismatch:\n%s", out)
		}
	})

	// =====================================================================
	// 场景八：内容去重不得被误报为篡改
	//
	// 层 2 是内容寻址的：两个依赖内容相同时 digest 相同，**合法共享同一个
	// 条目**，而 meta.json 记录的是首次写入者的来源（repo / commit）。
	// 如果 verify 把"meta 的来源字段与当前条目不同"当成完整性失败，
	// 正常仓库会被大面积误报——这个用例就是那条误报的回归防线。
	// =====================================================================
	t.Run("identical content across dependencies shares one store entry", func(t *testing.T) {
		const body = "export const shared = 1\n"
		m5Upstream(t, "github:m5/dup-a", body)
		m5Upstream(t, "github:m5/dup-b", body)

		proj := m5Project(t, "github:m5/dup-a v1 tag", "github:m5/dup-b v1 tag")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		lf, err := lock.Read(lock.Find(proj))
		if err != nil {
			t.Fatal(err)
		}
		a, okA := lf.Find("github:m5/dup-a", "")
		b, okB := lf.Find("github:m5/dup-b", "")
		if !okA || !okB {
			t.Fatal("both dependencies must be locked")
		}
		if a.ArchiveDigest != b.ArchiveDigest {
			t.Skipf("fixture produced different trees (%s vs %s); nothing shared to check",
				a.ArchiveDigest, b.ArchiveDigest)
		}
		if a.Commit == b.Commit {
			t.Fatal("fixture precondition: the commits must differ while the trees match")
		}

		// 默认与 --deep 都必须通过：共享条目不是问题，内容不同才是
		for _, args := range [][]string{
			{"verify", "--dir=" + proj},
			{"verify", "--deep", "--dir=" + proj},
		} {
			if code, out := runCaptureCode(t, args...); code != 0 {
				t.Fatalf("%v: shared content must not look like tampering; exit=%d out=%s", args, code, out)
			}
		}
	})

	// =====================================================================
	// 场景九：离线模式
	// =====================================================================
	t.Run("offline uses the local snapshot and marks it stale", func(t *testing.T) {
		m5Upstream(t, "github:m5/off", "v1\n")
		proj := m5Project(t, "github:m5/off v1 tag")
		m5Install(t, proj, "github:m5/off")

		code, out := runCaptureCode(t, "verify", "--offline", "--dir="+proj)
		if code != 0 {
			t.Fatalf("offline verify on a warm mirror exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, "[stale]") {
			t.Errorf("offline results must be marked stale:\n%s", out)
		}
		if !strings.Contains(out, "local mirror snapshot") {
			t.Errorf("offline run should say what it compared against:\n%s", out)
		}

		// 冷 mirror：--offline 的资源缺失按退出码表归为 4
		layout := defaultLayoutForTest(t)
		if err := os.RemoveAll(layout.MirrorRoot()); err != nil {
			t.Fatal(err)
		}
		code, out = runCaptureCode(t, "verify", "--offline", "--dir="+proj)
		if code != 4 {
			t.Fatalf("--offline with a cold mirror must exit 4, got %d:\n%s", code, out)
		}
	})

	// =====================================================================
	// 场景九：配置类错误
	// =====================================================================
	t.Run("missing lock exits 3", func(t *testing.T) {
		proj := t.TempDir()
		if code, out := runCaptureCode(t, "init", "github.com:m5-test/empty", "--dir="+proj); code != 0 {
			t.Fatalf("init: %s", out)
		}
		code, out := runCaptureCode(t, "verify", "--dir="+proj)
		if code != 3 {
			t.Fatalf("verify without a lock must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "ngm install") {
			t.Errorf("output should point at `ngm install`:\n%s", out)
		}
	})

	t.Run("unknown dependency argument exits 3", func(t *testing.T) {
		m5Upstream(t, "github:m5/filter", "v1\n")
		proj := m5Project(t, "github:m5/filter v1 tag")
		m5Install(t, proj, "github:m5/filter")

		// 指定存在的依赖 → 只查它
		if code, out := runCaptureCode(t, "verify", "github:m5/filter", "--dir="+proj); code != 0 {
			t.Errorf("filtering to an existing dependency should pass, got %d:\n%s", code, out)
		}
		// 指定 lock 中不存在的依赖 → 3（静默忽略会让"我验过了"变成假象）
		code, out := runCaptureCode(t, "verify", "github:m5/ghost", "--dir="+proj)
		if code != 3 {
			t.Fatalf("verifying an undeclared dependency must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "github:m5/ghost") {
			t.Errorf("output should name the unknown dependency:\n%s", out)
		}
	})

	// =====================================================================
	// 场景十：多依赖混合——退出码取最严重者
	// =====================================================================
	t.Run("exit code takes the worst dependency", func(t *testing.T) {
		m5Upstream(t, "github:m5/good", "good\n")
		bad := m5Upstream(t, "github:m5/bad", "bad\n")

		proj := m5Project(t, "github:m5/good v1 tag", "github:m5/bad v1 tag")
		m5Install(t, proj, "github:m5/good")

		// 让 bad 的 tag 移动（非预期漂移），good 保持匹配 → 退出码必须取最严重者
		bad.WriteFile("index.ts", "bad2\n")
		bad.Commit("bad2")
		bad.ForceRetag("", "v1", false)

		code, out := runCaptureCode(t, "verify", "--dir="+proj)
		if code != 1 {
			t.Fatalf("one unexpected dependency must make the whole run exit 1, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "github:m5/good@v1 (tag)") || !strings.Contains(out, "github:m5/bad@v1 (tag)") {
			t.Errorf("every dependency must appear in the report:\n%s", out)
		}
		if !strings.Contains(out, "1 ok, 1 unexpected") {
			t.Errorf("summary should count both outcomes:\n%s", out)
		}
	})
}
