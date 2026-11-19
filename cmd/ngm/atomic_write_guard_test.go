package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// bareArtifactWrites 是**允许**直接 os.WriteFile 的站点，以及为什么允许。
//
// v0.82 把 atomicfile 定为"写用户可见产物"的唯一出口，但那条规则只写在注释里：
// init / transform / wasm 三个出口一直是裸写，而且没有任何判会发现第四处。
// 这张表把例外变成**登记的例外**——想新增一处，必须先在这里说清它为什么
// 不是产物；已经改成 atomicfile 的站点也必须把条目删掉，否则判据会报告
// 过期的登记。
var bareArtifactWrites = map[string]string{
	// 全新的 MkdirTemp 目录，文件只是 git hash-object 的输入，不会被当成产物交付。
	"cmd/ngm/modules.go": "staging files for one git hash-object call; the directory is removed on return",
	// 幂等的布局标记，写失败按设计不阻断；它不承载内容。
	"internal/core/vendor/content_v2.go": "idempotent layout marker; failure is deliberately ignored",
	// 沙箱探针脚本必须是 0600；atomicfile.Write 强制 0644，不能走它。
	"internal/security/sandbox.go": "0600 probe script — atomicfile.Perm is 0644",
	// 缓存是优化不是证据：写坏一份缓存应当下次重算，而不是把命令判失败。
	"internal/supplychain/osv.go": "OSV cache is an optimization, not evidence",
}

// TestUserVisibleWritesGoThroughAtomicfile 从源码派生"谁在裸写文件"。
//
// 用 AST 而不是文本 grep：atomicfile / sbom / attest 的注释里就写着
// `os.WriteFile`，文本扫描会把这些解释文字当成违规站点。
// 测试脚手架（testutils / testdata）不算产品代码，因此不扫。
func TestUserVisibleWritesGoThroughAtomicfile(t *testing.T) {
	root := repoRoot(t)
	found := map[string]int{}
	filesChecked := 0

	for _, top := range []string{filepath.Join("cmd", "ngm"), "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if rel == top || rel == filepath.ToSlash(top) {
					return nil
				}
				if d.Name() == "testdata" || d.Name() == "testutils" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			filesChecked++

			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			alias := osImportAlias(f)
			if alias == "" {
				return nil
			}
			hits := 0
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "WriteFile" {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if ok && pkg.Name == alias {
					hits++
				}
				return true
			})
			if hits > 0 {
				found[rel] = hits
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", top, err)
		}
	}

	if filesChecked < 120 {
		t.Fatalf("only %d product files checked — the scan net is vacuous", filesChecked)
	}
	for path, n := range found {
		why, registered := bareArtifactWrites[path]
		if !registered {
			t.Errorf("%s has %d bare os.WriteFile call(s) — user-visible artifacts must go "+
				"through atomicfile.Write; if this really is not an artifact, register it with a reason", path, n)
			continue
		}
		t.Logf("%s: %d bare write(s) — %s", path, n, why)
	}
	for path := range bareArtifactWrites {
		if found[path] == 0 {
			t.Errorf("stale allowlist entry %s — it no longer writes directly; remove the entry "+
				"so the registry describes the code", path)
		}
	}
}

// TestInitForceWritesArtifactsWithTheSharedPermissions 是上面那条源码判据的
// 行为侧对应物：一个已存在、权限 0600 的 ngm.json 在 --force 之后必须变成
// 可共享的 0644。裸 os.WriteFile 会**保留**原文件的权限，因此这条在改造前
// 会红；它同时确认新模板确实替换了旧内容、临时文件没有残骸。
func TestInitForceWritesArtifactsWithTheSharedPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "ngm.json")
	const stale = "{\"name\":\"stale\"}\n"
	if err := os.WriteFile(cfg, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out := runCaptureCode(t, "init", "demo", "--dir="+dir, "--force")
	if code != 0 {
		t.Fatalf("init --force exit=%d:\n%s", code, out)
	}
	body, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "stale") {
		t.Errorf("the old config survived --force:\n%s", body)
	}
	for _, path := range []string{cfg, filepath.Join(dir, "src", "index.ts")} {
		info, serr := os.Stat(path)
		if serr != nil {
			t.Fatalf("stat %s: %v", path, serr)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Errorf("%s mode = %04o, want 0644 — a shared artifact must stay world-readable", path, got)
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".ngm-") && strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("atomic write left a temp file behind: %s", e.Name())
		}
	}
}

// osImportAlias 返回该文件里 `os` 包被叫成什么（含显式别名）。
func osImportAlias(f *ast.File) string {
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		if p != "os" {
			continue
		}
		if imp.Name == nil {
			return "os"
		}
		if imp.Name.Name == "_" || imp.Name.Name == "." {
			return ""
		}
		return imp.Name.Name
	}
	return ""
}

// TestAtomicArtifactWritesAreActuallyUsed 守的是反方向：三个**曾经裸写**的出口
// 必须仍从 atomicfile 调用 Write。只扫"没有 os.WriteFile"不够——把整段写入
// 删掉也能让上一条判据变绿。
func TestAtomicArtifactWritesAreActuallyUsed(t *testing.T) {
	root := repoRoot(t)
	for _, path := range []string{
		filepath.Join("cmd", "ngm", "init.go"),
		filepath.Join("cmd", "ngm", "transform.go"),
		filepath.Join("internal", "adapter", "wasm.go"),
	} {
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "atomicfile.Write(") {
			t.Errorf("%s no longer writes its artifact atomically", filepath.ToSlash(path))
		}
	}
}
