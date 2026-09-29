package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/mappings"
	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// m4LeafRepo 建一个可作为依赖的仓库：带 ngm.json 入口声明 + 一个 index.ts 回退。
func m4LeafRepo(t *testing.T, slug string, extra map[string]string) {
	t.Helper()
	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", "export const leaf = 1\n")
	for rel, content := range extra {
		r.WriteFile(rel, content)
	}
	r.Commit("feat: leaf")
	r.Tag("v1", false)
	seedMirror(t, slug, r.Dir)
}

// TestM4Acceptance 是 development/v0.1-plan.md 中 M4 阶段的**可执行验收**。
//
// 逐条对应 M4 的验收标准：
//
//  1. vendor 文件与 content **逐文件哈希一致**（全量校验，非抽样）
//  2. copy 模式与 symlink 模式各有测试
//  3. Windows 上 hardlink 失败自动降级 copy（由 linkMode 契约 + CI 覆盖）
//  4. `mappings validate` 能检出缺失入口 / 不存在的 `to` 路径
func TestM4Acceptance(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	// ---------------------------------------------------------------------
	// 验收 1 + 端到端：install 落地 vendor 并生成 mappings
	// ---------------------------------------------------------------------
	t.Run("install materializes vendor and generates mappings", func(t *testing.T) {
		m4LeafRepo(t, "github:m4/utils", nil)

		proj := m3Project(t, "github:m4/utils v1")
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("install exit=%d out=%s", code, out)
		}

		// vendor 目录存在
		vendorRoot := filepath.Join(proj, vendor.VendorDirName)
		vendorTree := filepath.Join(vendorRoot, "github.com", "m4", "utils")
		if st, err := os.Stat(vendorTree); err != nil || !st.IsDir() {
			t.Fatalf("vendor tree missing: %v", err)
		}
		if _, err := os.Stat(filepath.Join(vendorTree, "index.ts")); err != nil {
			t.Errorf("vendor file missing: %v", err)
		}

		// mappings 生成
		mf, err := mappings.Read(mappings.Find(proj))
		if err != nil {
			t.Fatalf("read mappings: %v", err)
		}
		if mf == nil {
			t.Fatal("install must write ngm.mappings.json")
		}
		if len(mf.Mappings) != 1 {
			t.Fatalf("mappings=%d want 1", len(mf.Mappings))
		}
		m := mf.Mappings[0]
		if m.From != "github:m4/utils" {
			t.Errorf("from=%q", m.From)
		}
		if m.To != "./ngm.vendor/github.com/m4/utils" {
			t.Errorf("to=%q", m.To)
		}
		// index 约定推断出入口
		if m.Main != "./index.ts" {
			t.Errorf("main=%q want ./index.ts", m.Main)
		}
	})

	// ---------------------------------------------------------------------
	// 验收 1（续）：vendor 与 content **逐文件哈希一致**（全量校验）
	// ---------------------------------------------------------------------
	t.Run("vendor matches content file-by-file", func(t *testing.T) {
		// 用多样的内容：嵌套目录、可执行、空文件
		r := testutils.NewGitRepo(t)
		r.WriteFile("index.ts", "export const x = 1\n")
		r.WriteFile("src/deep/mod.ts", "export const m = 2\n")
		r.WriteFile("empty.txt", "")
		r.AddExecutable("scripts/run.sh", "#!/bin/sh\necho hi\n")
		r.Commit("feat: varied")
		r.Tag("v1", false)
		seedMirror(t, "github:m4/varied", r.Dir)

		proj := m3Project(t, "github:m4/varied v1")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		lf, err := lock.Read(lock.Find(proj))
		if err != nil {
			t.Fatal(err)
		}
		d, _ := lf.Find("github:m4/varied", "")

		layout := defaultLayoutForTest(t)
		contentTree := vendor.NewContentStore(layout.ContentRoot()).TreePath(d.ArchiveDigest)
		vendorTree := filepath.Join(proj, vendor.VendorDirName, "github.com", "m4", "varied")

		vr, err := vendor.VerifyVendorTree(vendorTree, contentTree)
		if err != nil {
			t.Fatalf("VerifyVendorTree: %v", err)
		}
		if !vr.OK() {
			t.Errorf("vendor must match content exactly:\n%s", strings.Join(vr.Mismatches, "\n"))
		}
		if vr.Files != 4 {
			t.Errorf("verified files=%d want 4 (index/mod/empty/run.sh)", vr.Files)
		}
	})

	// ---------------------------------------------------------------------
	// 验收 2：linkMode 矩阵
	// ---------------------------------------------------------------------
	t.Run("linkMode matrix", func(t *testing.T) {
		m4LeafRepo(t, "github:m4/modes", nil)

		cases := []struct {
			mode         string
			wantSymlink  bool
			wantSameFile bool // 与 content 是否同一 inode（hardlink 的特征）
		}{
			{"auto", false, true},
			{"copy", false, false},
		}
		if runtime.GOOS != "windows" {
			// symlink 模式需要文件系统支持；上面已用 probe 在 links_test 中处理，
			// 这里在 Windows 上直接跳过
			cases = append(cases, struct {
				mode         string
				wantSymlink  bool
				wantSameFile bool
			}{"symlink", true, false})
		}

		for _, tc := range cases {
			t.Run(tc.mode, func(t *testing.T) {
				proj := m3Project(t, "github:m4/modes v1")
				// 写 vendor.linkMode
				setVendorConfig(t, proj, `{"mode":"local","linkMode":"`+tc.mode+`"}`)

				code, out := runCaptureCode(t, "install", "--dir="+proj)
				if code != 0 {
					if tc.mode == "symlink" && strings.Contains(out, "symlink") {
						t.Skipf("symlinks unavailable: %s", out)
					}
					t.Fatalf("install exit=%d out=%s", code, out)
				}

				entry := filepath.Join(proj, vendor.VendorDirName, "github.com", "m4", "modes")
				info, err := os.Lstat(entry)
				if err != nil {
					t.Fatalf("lstat vendor entry: %v", err)
				}

				isSymlink := info.Mode()&os.ModeSymlink != 0
				if isSymlink != tc.wantSymlink {
					t.Errorf("symlink=%v want %v (mode=%s)", isSymlink, tc.wantSymlink, tc.mode)
				}

				// 内容始终可读且正确
				body, err := os.ReadFile(filepath.Join(entry, "index.ts"))
				if err != nil {
					t.Fatalf("read vendored file: %v", err)
				}
				if string(body) != "export const leaf = 1\n" {
					t.Errorf("content=%q", body)
				}

				if !tc.wantSymlink {
					lf, _ := lock.Read(lock.Find(proj))
					d, _ := lf.Find("github:m4/modes", "")
					layout := defaultLayoutForTest(t)
					contentFile := filepath.Join(
						vendor.NewContentStore(layout.ContentRoot()).TreePath(d.ArchiveDigest), "index.ts")
					got := vendor.SameFile(filepath.Join(entry, "index.ts"), contentFile)
					if got != tc.wantSameFile {
						t.Errorf("hardlink=%v want %v (mode=%s)", got, tc.wantSameFile, tc.mode)
					}
				}
			})
		}
	})

	// ---------------------------------------------------------------------
	// 验收 4：mappings validate
	// ---------------------------------------------------------------------
	t.Run("mappings validate passes after install", func(t *testing.T) {
		m4LeafRepo(t, "github:m4/ok", map[string]string{
			"ngm.json":      `{"main":"./dist/lib.js","types":"./dist/lib.d.ts"}`,
			"dist/lib.js":   "x",
			"dist/lib.d.ts": "y",
		})

		proj := m3Project(t, "github:m4/ok v1")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		code, out := runCaptureCode(t, "mappings", "validate", "--dir="+proj)
		if code != 0 {
			t.Fatalf("validate exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, "OK") {
			t.Errorf("out=%s", out)
		}
	})

	t.Run("mappings validate detects a missing entry file", func(t *testing.T) {
		m4LeafRepo(t, "github:m4/entry", map[string]string{
			"ngm.json":     `{"main":"./dist/gone.js"}`,
			"dist/gone.js": "x",
		})
		proj := m3Project(t, "github:m4/entry v1")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		// 删掉入口文件（模拟被删/写坏）
		if err := os.Remove(filepath.Join(proj, vendor.VendorDirName,
			"github.com", "m4", "entry", "dist", "gone.js")); err != nil {
			t.Fatal(err)
		}

		code, out := runCaptureCode(t, "mappings", "validate", "--dir="+proj)
		if code != 3 {
			t.Fatalf("missing entry must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "`main` file does not exist") {
			t.Errorf("output should name the missing entry:\n%s", out)
		}
	})

	t.Run("mappings validate detects a missing to-path", func(t *testing.T) {
		m4LeafRepo(t, "github:m4/topath", nil)
		proj := m3Project(t, "github:m4/topath v1")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		// 删除整个 vendor 树（模拟 vendor 未落地 / 被清理）
		if err := os.RemoveAll(filepath.Join(proj, vendor.VendorDirName)); err != nil {
			t.Fatal(err)
		}

		code, out := runCaptureCode(t, "mappings", "validate", "--dir="+proj)
		if code != 3 {
			t.Fatalf("missing to-path must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "does not exist on disk") {
			t.Errorf("output should explain the missing path:\n%s", out)
		}
	})

	t.Run("mappings validate detects a stale from", func(t *testing.T) {
		m4LeafRepo(t, "github:m4/stale", nil)
		proj := m3Project(t, "github:m4/stale v1")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		// 手工往 mappings 里塞一条 lock 中不存在的条目
		path := mappings.Find(proj)
		f, err := mappings.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		f.Mappings = append(f.Mappings, mappings.Mapping{
			From: "github:m4/ghost",
			To:   "./ngm.vendor/github.com/m4/stale", // 路径存在，但 from 不在 lock
		})
		if err := mappings.Write(path, f); err != nil {
			t.Fatal(err)
		}

		code, out := runCaptureCode(t, "mappings", "validate", "--dir="+proj)
		if code != 3 {
			t.Fatalf("stale from must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "github:m4/ghost") || !strings.Contains(out, "ngm.lock") {
			t.Errorf("output should name the stale entry and the lock:\n%s", out)
		}
	})

	// ---------------------------------------------------------------------
	// cache clean
	// ---------------------------------------------------------------------
	t.Run("cache clean leaves provability intact", func(t *testing.T) {
		m4LeafRepo(t, "github:m4/cache", nil)
		proj := m3Project(t, "github:m4/cache v1")
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		layout := defaultLayoutForTest(t)
		cache := vendor.NewCache(layout.CacheRoot())
		if err := cache.EnsureDirs(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cache.MetadataRoot(), "junk.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		code, out := runCaptureCode(t, "cache", "clean")
		if code != 0 {
			t.Fatalf("cache clean exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, "cleaned") {
			t.Errorf("out=%s", out)
		}

		// 可证明性不受影响：install 仍能工作（走 lock 路径）
		code, out = runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("install after cache clean exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, "lock respected") {
			t.Errorf("install should still respect the lock:\n%s", out)
		}
	})
}

// setVendorConfig 重写项目 ngm.json 的 vendor 段。
func setVendorConfig(t *testing.T, proj, vendorJSON string) {
	t.Helper()
	path := filepath.Join(proj, "ngm.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, `"vendor"`) {
		// 已有 vendor 段：替换整个对象（简单起见定位到下一个顶层键）
		start := strings.Index(s, `"vendor"`)
		if start < 0 {
			t.Fatalf("cannot locate vendor section")
		}
		// 找到该对象的结束位置（缩进为 2 空格的 `}` 后跟逗号或换行）
		rest := s[start:]
		depth := 0
		end := -1
		for i, c := range rest {
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
				if depth == 0 {
					end = start + i + 1
					break
				}
			}
		}
		if end < 0 {
			t.Fatalf("cannot find end of vendor object")
		}
		s = s[:start] + `"vendor": ` + vendorJSON + s[end:]
	} else {
		t.Fatalf("fixture ngm.json has no vendor section to replace:\n%s", s)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
