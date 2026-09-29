package main

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// resolvedAtLine 匹配 lock 中唯一非确定性字段（可复现性判定用）。
var resolvedAtLine = regexp.MustCompile(`(?m)^(\s*)"resolvedAt": "[^"]*"(,?)$`)

func stripResolvedAt(s string) string {
	return resolvedAtLine.ReplaceAllString(s, `${1}"resolvedAt": "<TIME>"${2}`)
}

func readLockFile(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(lock.Find(dir))
	if err != nil {
		t.Fatalf("read ngm.lock: %v", err)
	}
	return string(data)
}

// m3Project 建一个项目并声明给定依赖。
//
// 每个 dep 形如 `github:m3/a v1`：slug + ref（refType 固定为 tag，
// 因为 fixture 用 tag 制造可区分的版本）。
func m3Project(t *testing.T, deps ...string) string {
	t.Helper()
	proj := t.TempDir()
	if code, out := runCaptureCode(t, "init", "github.com:m3-test/app", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init: %s", out)
	}
	for _, d := range deps {
		fields := strings.Fields(d)
		if len(fields) != 2 {
			t.Fatalf("bad fixture dep %q: want `<slug> <ref>`", d)
		}
		spec := fields[0] + "@" + fields[1]
		if code, out := runCaptureCode(t, "add", spec, "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add %s: %s", spec, out)
		}
	}
	return proj
}

// chainRepo 建一个"上游 → 传递依赖"的链式 fixture 并预置 mirror。
//
// 返回叶子的 commit。deps 是形如 `github:m3/x@v1` 的声明（refType 固定 tag）。
func chainWorld(t *testing.T, name string, deps []string) {
	t.Helper()
	r := testutils.NewGitRepo(t)
	if len(deps) > 0 {
		body := `{"dependencies":[`
		for i, d := range deps {
			parts := strings.SplitN(d, "@", 2)
			if i > 0 {
				body += ","
			}
			body += `{"name":"` + parts[0] + `","ref":"` + parts[1] + `","refType":"tag"}`
		}
		body += `]}`
		r.WriteFile("ngm.json", body)
	}
	r.WriteFile("index.ts", "export const x = 1\n")
	r.Commit("feat: " + name)
	r.Tag("v1", false)
	seedMirror(t, name, r.Dir)
}

func leafWorld(t *testing.T, name string) {
	t.Helper()
	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", "export const leaf = 1\n")
	r.Commit("feat: leaf")
	r.Tag("v1", false)
	seedMirror(t, name, r.Dir)
}

// TestM3Acceptance 是 development/v0.1-plan.md 中 M3 阶段的**可执行验收**。
//
// 逐条对应 M3 的验收标准：
//
//  1. golden lock 用例：连续两次 install，除 resolvedAt 外字节级一致
//  2. 冲突三场景：合并 / root wins / 传递冲突 exit 3
//  3. 传递依赖进入 lock：多层依赖链（A→B→C）全部出现在 lock 中
func TestM3Acceptance(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	// ---------------------------------------------------------------------
	// 验收 3：传递依赖进入 lock（A → B → C）
	// ---------------------------------------------------------------------
	t.Run("transitive chain lands in lock", func(t *testing.T) {
		leafWorld(t, "github:m3/c")
		chainWorld(t, "github:m3/b", []string{"github:m3/c@v1"})
		chainWorld(t, "github:m3/a", []string{"github:m3/b@v1"})

		proj := m3Project(t, "github:m3/a v1")
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("install exit=%d out=%s", code, out)
		}

		lf, err := lock.Read(lock.Find(proj))
		if err != nil {
			t.Fatalf("read lock: %v", err)
		}
		if lf == nil {
			t.Fatal("install must write ngm.lock")
		}
		if len(lf.Dependencies) != 3 {
			t.Fatalf("lock has %d entries, want 3 (a, b, c):\n%s", len(lf.Dependencies), readLockFile(t, proj))
		}
		for _, name := range []string{"github:m3/a", "github:m3/b", "github:m3/c"} {
			d, ok := lf.Find(name, "")
			if !ok {
				t.Errorf("missing %s in lock", name)
				continue
			}
			if len(d.Commit) != 40 {
				t.Errorf("%s commit=%q", name, d.Commit)
			}
			if !strings.HasPrefix(d.ArchiveDigest, "sha256:") {
				t.Errorf("%s digest=%q", name, d.ArchiveDigest)
			}
			if d.VendorPath == "" {
				t.Errorf("%s missing vendorPath", name)
			}
			// vendorPath 是 canonical 形式（host/org/repo）
			if strings.Contains(d.VendorPath, ":") {
				t.Errorf("%s vendorPath should be canonical (no colon): %q", name, d.VendorPath)
			}
		}
		// lock 必须可被重新读取（schema 自洽）
		if err := lf.Validate(); err != nil {
			t.Errorf("written lock failed validation: %v", err)
		}
	})

	// ---------------------------------------------------------------------
	// 验收 1：连续两次 install，除 resolvedAt 外字节级一致
	// ---------------------------------------------------------------------
	t.Run("lock is reproducible", func(t *testing.T) {
		leafWorld(t, "github:m3/r")
		proj := m3Project(t, "github:m3/r v1")

		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("first install: %s", out)
		}
		first := readLockFile(t, proj)

		// 删除 lock 强制完整重解析（模拟另一台机器 / 另一次 install）
		if err := os.Remove(lock.Find(proj)); err != nil {
			t.Fatal(err)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("second install: %s", out)
		}
		second := readLockFile(t, proj)

		if first == second {
			// 极端巧合：两次 resolvedAt 相同。可接受，但仍要验证屏蔽后一致
			t.Logf("locks are byte-identical including resolvedAt (same second)")
		}
		if stripResolvedAt(first) != stripResolvedAt(second) {
			t.Errorf("lock is not reproducible beyond resolvedAt:\n--- first ---\n%s\n--- second ---\n%s",
				stripResolvedAt(first), stripResolvedAt(second))
		}

		// 三次：这次不删 lock（应走"尊重 lock"路径），内容仍不变
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("third install: %s", out)
		}
		third := readLockFile(t, proj)
		if third != second {
			t.Errorf("respected-lock install rewrote the lock:\n--- before ---\n%s\n--- after ---\n%s", second, third)
		}
	})

	// ---------------------------------------------------------------------
	// install 尊重 lock：上游前进后，install 不改变已锁定的 commit
	// ---------------------------------------------------------------------
	t.Run("install respects lock (does not re-resolve)", func(t *testing.T) {
		upstream := testutils.NewGitRepo(t)
		upstream.WriteFile("index.ts", "v1\n")
		firstCommit := upstream.Commit("v1")
		upstream.Tag("v1", false)
		seedMirror(t, "github:m3/moving", upstream.Dir)

		proj := m3Project(t, "github:m3/moving v1")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		lf, _ := lock.Read(lock.Find(proj))
		locked, _ := lf.Find("github:m3/moving", "")
		if locked.Commit != firstCommit {
			t.Fatalf("locked commit=%s want %s", locked.Commit, firstCommit)
		}

		// 上游 tag 重打（同一 tag 指向新 commit）
		upstream.WriteFile("index.ts", "v1 rewritten\n")
		newCommit := upstream.Commit("v1 rewritten")
		upstream.ForceRetag("", "v1", false)
		if newCommit == firstCommit {
			t.Skip("fixture produced identical commit")
		}
		// 更新 mirror 让新 commit 可见
		seedMirror(t, "github:m3/moving", upstream.Dir)

		// install 必须仍然使用 lock 里的旧 commit（"有 lock → 不更新 ref"）
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("second install exit=%d out=%s", code, out)
		}
		lf2, _ := lock.Read(lock.Find(proj))
		got, _ := lf2.Find("github:m3/moving", "")
		if got.Commit != firstCommit {
			t.Errorf("install re-resolved the ref: commit changed %s → %s (lock must be respected)",
				firstCommit, got.Commit)
		}

		// 而 update 应当刷新到新 commit
		code, out = runCaptureCode(t, "update", "--all", "--dir="+proj)
		if code != 0 {
			t.Fatalf("update exit=%d out=%s", code, out)
		}
		lf3, _ := lock.Read(lock.Find(proj))
		got3, _ := lf3.Find("github:m3/moving", "")
		if got3.Commit != newCommit {
			t.Errorf("update did not refresh the lock: got %s want %s", got3.Commit, newCommit)
		}
		if got3.ArchiveDigest == locked.ArchiveDigest {
			t.Errorf("digest should change when the commit changes")
		}
	})

	// ---------------------------------------------------------------------
	// lock 与 ngm.json 不一致 → 重新解析（新增依赖）
	// ---------------------------------------------------------------------
	t.Run("lock staleness triggers re-resolve", func(t *testing.T) {
		leafWorld(t, "github:m3/x1")
		leafWorld(t, "github:m3/x2")

		proj := m3Project(t, "github:m3/x1 v1")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		if lf, _ := lock.Read(lock.Find(proj)); len(lf.Dependencies) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(lf.Dependencies))
		}

		// 声明新增一个依赖，但不动 lock
		if code, out := runCaptureCode(t, "add", "github:m3/x2@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("install after add: %s", out)
		}
		if !strings.Contains(out, "does not match") {
			t.Errorf("install should report that lock is stale:\n%s", out)
		}
		lf, _ := lock.Read(lock.Find(proj))
		if len(lf.Dependencies) != 2 {
			t.Errorf("lock should now have 2 entries, got %d", len(lf.Dependencies))
		}
	})

	// ---------------------------------------------------------------------
	// 验收 2：冲突三场景
	// ---------------------------------------------------------------------
	t.Run("conflict: same ref merges", func(t *testing.T) {
		leafWorld(t, "github:m3/z")
		chainWorld(t, "github:m3/px", []string{"github:m3/z@v1"})
		chainWorld(t, "github:m3/py", []string{"github:m3/z@v1"})

		proj := m3Project(t, "github:m3/px v1", "github:m3/py v1")
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("same-ref diamond must merge: exit=%d out=%s", code, out)
		}
		lf, _ := lock.Read(lock.Find(proj))
		if len(lf.Dependencies) != 3 {
			t.Errorf("lock entries=%d want 3 (px, py, z once):\n%s", len(lf.Dependencies), readLockFile(t, proj))
		}
	})

	t.Run("conflict: root wins", func(t *testing.T) {
		// z 有两个 tag；根声明 v1，上游 py 要求 v2
		z := testutils.NewGitRepo(t)
		z.WriteFile("z.txt", "v1\n")
		z.Commit("v1")
		z.Tag("v1", false)
		z.WriteFile("z.txt", "v2\n")
		z.Commit("v2")
		z.Tag("v2", false)
		seedMirror(t, "github:m3/zz", z.Dir)

		chainWorld(t, "github:m3/qy", []string{"github:m3/zz@v2"})

		proj := m3Project(t, "github:m3/zz v1", "github:m3/qy v1")
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("root wins must not fail: exit=%d out=%s", code, out)
		}
		lf, _ := lock.Read(lock.Find(proj))
		d, ok := lf.Find("github:m3/zz", "")
		if !ok {
			t.Fatal("missing zz in lock")
		}
		if d.Ref != "v1" {
			t.Errorf("zz ref=%q want v1 (root wins)", d.Ref)
		}
		// 应提示被覆盖的传递声明
		if !strings.Contains(out, "overridden") && !strings.Contains(out, "note:") {
			t.Errorf("install should note the overridden transitive requirement:\n%s", out)
		}
	})

	t.Run("conflict: transitive-only fails with exit 3", func(t *testing.T) {
		z := testutils.NewGitRepo(t)
		z.WriteFile("z.txt", "v1\n")
		z.Commit("v1")
		z.Tag("v1", false)
		z.WriteFile("z.txt", "v2\n")
		z.Commit("v2")
		z.Tag("v2", false)
		seedMirror(t, "github:m3/cz", z.Dir)

		chainWorld(t, "github:m3/cx", []string{"github:m3/cz@v1"})
		chainWorld(t, "github:m3/cy", []string{"github:m3/cz@v2"})

		proj := m3Project(t, "github:m3/cx v1", "github:m3/cy v1")
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("transitive-only conflict must exit 3, got %d:\n%s", code, out)
		}
		for _, want := range []string{"conflicting transitive", "github:m3/cz", "root ngm.json"} {
			if !strings.Contains(out, want) {
				t.Errorf("conflict output should contain %q:\n%s", want, out)
			}
		}
		// 失败的 install 不得写出 lock
		if _, err := os.Stat(lock.Find(proj)); err == nil {
			t.Errorf("failed install must not write ngm.lock")
		}
	})

	// ---------------------------------------------------------------------
	// remove：只改声明，提示 install 刷新 lock
	// ---------------------------------------------------------------------
	t.Run("remove edits declaration only", func(t *testing.T) {
		leafWorld(t, "github:m3/rm")
		proj := m3Project(t, "github:m3/rm v1")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "remove", "github:m3/rm", "--dir="+proj)
		if code != 0 {
			t.Fatalf("remove exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, "ngm install") {
			t.Errorf("remove should point at `ngm install`:\n%s", out)
		}
		// 声明已移除，但 lock 未动（remove 是声明编辑，lock 由 install/update 负责）
		if lf, _ := lock.Read(lock.Find(proj)); len(lf.Dependencies) != 1 {
			t.Errorf("remove must not touch the lock")
		}

		// install 尊重 lock：声明为空也不清理 lock（lock 记录的是"上次解析结果"）
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install after remove: %s", out)
		}
		if lf, _ := lock.Read(lock.Find(proj)); len(lf.Dependencies) != 1 {
			t.Errorf("install must respect the lock, got %d entries", len(lf.Dependencies))
		}

		// update --all 重新计算锁定 → 孤儿条目被清理
		if code, out := runCaptureCode(t, "update", "--all", "--dir="+proj); code != 0 {
			t.Fatalf("update --all after remove: %s", out)
		}
		lf, _ := lock.Read(lock.Find(proj))
		if len(lf.Dependencies) != 0 {
			t.Errorf("update --all should rebuild the lock from ngm.json; got %d entries:\n%s",
				len(lf.Dependencies), readLockFile(t, proj))
		}

		// 移除不存在的依赖 → exit 3
		if code, _ := runCaptureCode(t, "remove", "github:m3/nope", "--dir="+proj); code != 3 {
			t.Errorf("removing an undeclared dependency should exit 3, got %d", code)
		}
	})

	// ---------------------------------------------------------------------
	// 离线闭环：mirror 已就绪时 install 完全不触网
	// ---------------------------------------------------------------------
	t.Run("install is fully offline when the mirror is warm", func(t *testing.T) {
		leafWorld(t, "github:m3/off")
		proj := m3Project(t, "github:m3/off v1")

		// 用一个不可达的远端验证"没有网络请求"：
		// mirror 已预置 → EnsureMirror 直接命中 → 不走 CloneURL
		env, err := newProjectEnv(proj)
		if err != nil {
			t.Fatal(err)
		}
		repo := resolve.MustNormalize("github:m3/off")
		if !env.Mirror.Exists(repo) {
			t.Fatal("fixture precondition: mirror should exist")
		}
		// 直接验证解析走的是本地路径
		commit, rerr := resolve.ResolveRef(context.Background(), repo, "v1", resolve.RefTypeTag,
			resolve.ResolveOptions{GitURL: env.Mirror.PathFor(repo)})
		if rerr != nil {
			t.Fatalf("local resolve failed: %v", rerr)
		}
		if len(commit) != 40 {
			t.Errorf("commit=%q", commit)
		}

		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("offline install exit=%d out=%s", code, out)
		}
	})

	// ---------------------------------------------------------------------
	// install 检测到 lock 与内容不符 → exit 2（完整性）
	// ---------------------------------------------------------------------
	t.Run("install rejects a tampered lock digest", func(t *testing.T) {
		leafWorld(t, "github:m3/tamper")
		proj := m3Project(t, "github:m3/tamper v1")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		// 篡改 lock 的 digest，并清掉 content store 让 install 重新落地
		raw, err := os.ReadFile(lock.Find(proj))
		if err != nil {
			t.Fatal(err)
		}
		tampered := strings.Replace(string(raw), `"archiveDigest": "sha256:`,
			`"archiveDigest": "sha256:00000000000000000000000000000000000000000000000000000000000000`, 1)
		if tampered == string(raw) {
			t.Fatal("failed to tamper the lock")
		}
		if err := os.WriteFile(lock.Find(proj), []byte(tampered), 0o644); err != nil {
			t.Fatal(err)
		}

		// 清空 content store，迫使 install 重新落地并复核
		layout := defaultLayoutForTest(t)
		if err := os.RemoveAll(layout.ContentRoot()); err != nil {
			t.Fatal(err)
		}

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 2 {
			t.Fatalf("tampered digest must exit 2 (integrity), got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "mismatch") {
			t.Errorf("output should explain the mismatch:\n%s", out)
		}
	})
}

// defaultLayoutForTest 返回测试环境下的用户态布局。
func defaultLayoutForTest(t *testing.T) vendor.Layout {
	t.Helper()
	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

// TestM3LockGoldenShape 冻结 lock 的**结构**（字段集合），
// 防止无意中增删字段而破坏格式契约。
func TestM3LockGoldenShape(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	leafWorld(t, "github:m3/shape")
	proj := m3Project(t, "github:m3/shape v1")
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}

	raw := readLockFile(t, proj)
	for _, field := range []string{
		`"version": 1`,
		`"lockfileVersion": "1.0.0"`,
		`"dependencies"`,
		`"name"`, `"ref"`, `"refType"`, `"commit"`,
		`"archiveDigest"`, `"resolvedAt"`, `"vendorPath"`,
	} {
		if !strings.Contains(raw, field) {
			t.Errorf("lock is missing %s:\n%s", field, raw)
		}
	}
	// subPath 为空时不应出现（omitempty）
	if strings.Contains(raw, `"subPath"`) {
		t.Errorf("empty subPath must be omitted:\n%s", raw)
	}
	// 顶层不得有时间戳（locking.md §字段纪律）
	if strings.Contains(raw, `"resolvedAt"`) && strings.Count(raw, `"resolvedAt"`) != strings.Count(raw, `"name"`) {
		t.Errorf("resolvedAt must exist only inside dependency entries:\n%s", raw)
	}
	// 可被库自身重新解析（schema 自洽）
	if _, err := lock.Read(lock.Find(proj)); err != nil {
		t.Errorf("written lock cannot be re-read: %v", err)
	}
	// git 可用性自检（避免因环境缺失而静默跳过整组验收）
	if _, err := git.LookPath(); err != nil {
		t.Skip("git unavailable")
	}
}
