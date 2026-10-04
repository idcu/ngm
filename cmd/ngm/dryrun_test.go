package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// snapshotTree 把一个目录走一遍，返回 relpath -> sha256(内容)。
//
// 目录本身不进快照：ngm 不会凭空造空目录，而"空目录多了/少了"也会让断言在
// 无关紧要的差异上变红（**会误报的门禁会被忽略**）。
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if _, err := os.Stat(root); err != nil {
		return out // 目录还不存在 = 空快照，不是错误
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(body)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return out
}

// describeSnapshotDiff 给出人能读的差异（空 = 一模一样）。
func describeSnapshotDiff(before, after map[string]string) []string {
	var diffs []string
	for path, sum := range after {
		switch prev, ok := before[path]; {
		case !ok:
			diffs = append(diffs, "created: "+path)
		case prev != sum:
			diffs = append(diffs, "changed: "+path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			diffs = append(diffs, "removed: "+path)
		}
	}
	sort.Strings(diffs)
	return diffs
}

// TestV14DryRunAndReadOnlyCommandsWriteNothing 固定一条到处都该成立的承诺：
// **带 `--dry-run` 的命令一个字节都不写；只读命令也是。**
//
// 判据是**快照比对**（项目目录 + ngm home 各走一遍，比对路径集合与每个文件的 sha256），
// 不是读代码——v0.12 的审计把这一批命令逐个读过一遍并确认"都不落盘"，
// 但那是一次人工阅读，而本项目的纪律是"能与代码同源检查的一律做成机械检查"。
//
// 三处陪衬缺一不可（少了任何一个，这条网都会变成"绿的但不证明任何事"）：
//
//  1. **走到了那条分支**：dry-run 的输出里必须有它自己的措辞（`would bundle` 等）。
//     否则"什么都没写"可能只是因为命令根本没跑起来。
//  2. **残骸是真的能被清掉**：`store prune --dry-run` 的用例先造一个 `.unpack-*` 残骸，
//     并在后面真的跑一次 prune 确认它能被清——否则"dry-run 没删它"只是因为**它本来就不在那儿**。
//  3. **对照**：同一个 harness 必须能看见写入（末尾用一条不带 `--dry-run` 的 `add` 验证）。
//     一个恒为"快照相同"的仪器能让上面所有断言永远变绿。
//
// 快照**排除 `cache/`**：按 vendor 4 层的契约，缓存层是"可随时整层删除"的，
// 因此它被写不算违约——mirror 与 content store 才是"证明"所在的那两层。
func TestV14DryRunAndReadOnlyCommandsWriteNothing(t *testing.T) {
	home := isolateUserEnv(t)
	testutils.MustHaveGit(t)

	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", "export const x = 1\n")
	r.Commit("feat: x")
	r.Tag("v1.0.0", false)
	seedMirror(t, "github:v14/dry", r.Dir)

	proj := newProject(t)
	if code, out := runCaptureCode(t, "add", "github:v14/dry@v1.0.0", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}
	testutils.WriteFile(t, proj, "src/index.css", "body { color: red }\n")

	// 给 `store prune` 造一个**真的会被清掉**的残骸（见上面第 2 条陪衬）。
	layout, lerr := vendor.DefaultLayout()
	if lerr != nil {
		t.Fatal(lerr)
	}
	// 目录要先建：v2 布局下 `content/sha256/` 可能还不存在（blob 池是 `blobs/`），
	// 而 `Prune` 对"目录不存在"是合法的跳过——残骸得**真的在那儿**才算证据。
	residueDir := filepath.Join(layout.ContentRoot(), digest.Algorithm)
	if merr := os.MkdirAll(residueDir, 0o755); merr != nil {
		t.Fatal(merr)
	}
	residue := filepath.Join(residueDir, ".unpack-v14dry")
	if werr := os.WriteFile(residue, []byte("leftover\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}

	// home 快照（排除 cache/，理由见函数头）。
	homeSnapshot := func() map[string]string {
		snap := snapshotTree(t, home)
		for k := range snap {
			if strings.HasPrefix(k, "cache/") || strings.Contains(k, "/cache/") {
				delete(snap, k)
			}
		}
		return snap
	}

	cases := []struct {
		name string
		args []string
		// mustSee 证明命令**走到了那条分支**；措辞取自各自实现（不是猜的）。
		mustSee string
		// noDir 为真时不追加 `--dir`：`ngm store` 是**全局**命令（层 2 属于 ngm home，
		// 不属于某个项目），它连 `--dir` 这个 flag 都没有。
		noDir bool
	}{
		{name: "add --dry-run", args: []string{"add", "github:v14/other@v1.0.0", "--ref-type=tag", "--dry-run"}, mustSee: `"dependencies"`},
		{name: "build --dry-run", args: []string{"build", "src/index.ts", "--dry-run"}, mustSee: "would bundle"},
		{name: "transform --dry-run", args: []string{"transform", "src/index.ts", "--dry-run"}, mustSee: "would transform"},
		// typeCheck / typeDecl / css **没有**内置默认选择（见 engine_defaults_test.go），
		// 因此这里必须显式给引擎——否则命令走不到 dry-run 分支（exit 3）。
		{name: "typecheck --dry-run", args: []string{"typecheck", "--engine=typescript", "--dry-run"}, mustSee: "would type-check"},
		{name: "typedecl --dry-run", args: []string{"typedecl", "--engine=typescript", "--outdir=types", "--dry-run"}, mustSee: "would emit declarations"},
		{name: "css --dry-run", args: []string{"css", "src/index.css", "--engine=postcss", "--dry-run"}, mustSee: "would compile"},
		{name: "integrations add vite --dry-run", args: []string{"integrations", "add", "vite", "--dry-run"}},
		{name: "store prune --dry-run", args: []string{"store", "prune", "--dry-run"}, mustSee: "would remove", noDir: true},
		{name: "store usage (read-only)", args: []string{"store", "usage"}, noDir: true},
		{name: "config show (read-only)", args: []string{"config", "show"}},
		{name: "engines list (read-only)", args: []string{"engines", "list"}},
		{name: "why (read-only)", args: []string{"why", "github:v14/dry"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{}, tc.args...)
			if !tc.noDir {
				args = append(args, "--dir="+proj)
			}

			beforeProj, beforeHome := snapshotTree(t, proj), homeSnapshot()

			code, out := runCaptureCode(t, args...)
			if code != 0 {
				t.Fatalf("exit=%d — a dry-run or read-only command must not fail on a healthy project:\n%s", code, out)
			}
			if strings.TrimSpace(out) == "" {
				t.Error("no output at all: nothing was demonstrated")
			}
			if tc.mustSee != "" && !strings.Contains(out, tc.mustSee) {
				t.Errorf("output lacks %q, so the command never reached its dry-run branch:\n%s", tc.mustSee, out)
			}
			if diffs := describeSnapshotDiff(beforeProj, snapshotTree(t, proj)); len(diffs) > 0 {
				t.Errorf("the project directory changed:\n  %s", strings.Join(diffs, "\n  "))
			}
			if diffs := describeSnapshotDiff(beforeHome, homeSnapshot()); len(diffs) > 0 {
				t.Errorf("the ngm home changed (mirror / content store are the layers that matter):\n  %s", strings.Join(diffs, "\n  "))
			}
		})
	}

	// 第 2 条陪衬：残骸必须**还在**（dry-run 说了会删，但一个字节都不许动），
	// 而且它确实**可以被清掉**——否则上面那条只是"它本来就不在那儿"的假绿。
	if _, err := os.Stat(residue); err != nil {
		t.Fatalf("--dry-run removed the residue: %v", err)
	}
	if code, out := runCaptureCode(t, "store", "prune"); code != 0 {
		t.Fatalf("real prune failed: %s", out)
	}
	if _, err := os.Stat(residue); !os.IsNotExist(err) {
		t.Errorf("the residue survived a real prune, so the dry-run case proved nothing (stat err=%v)", err)
	}

	// 第 3 条陪衬：对照。同一个 harness 必须能看见写入。
	t.Run("control: the harness can see a write", func(t *testing.T) {
		before := snapshotTree(t, proj)
		code, out := runCaptureCode(t, "add", "github:v14/written@v1.0.0", "--ref-type=tag", "--dir="+proj)
		if code != 0 {
			t.Fatalf("add: %s", out)
		}
		if diffs := describeSnapshotDiff(before, snapshotTree(t, proj)); len(diffs) == 0 {
			t.Error("a real add changed nothing — the harness is blind, so every case above is vacuous")
		}
	})
}
