package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestCoreBoundaryForbidsLanguageSpecificImports 是 [ADR-033](../docs/adr/adr-033-language-neutral-core.md)
// 的第一条机械判据：语言无关核心包不许 import JS/TS 专属层。
//
// 它守的是**包边界**，不是目录名字：`internal/core/...` 与尚未搬迁的
// `internal/{atomicfile,errs,git,lock,resolve,vendor}` 都算核心。目录可以
// 一个包一个包地搬，但边界从第一次新增依赖时就成立，否则“核心不认识 JS”
// 会退化成一句注释。字符串里出现 package.json 之类的耦合由后续判据处理。
func TestCoreBoundaryForbidsLanguageSpecificImports(t *testing.T) {
	root := repoRoot(t)

	topLevelCore := map[string]bool{}
	forbidden := map[string]bool{
		"adapter":      true,
		"attest":       true,
		"config":       true,
		"integrations": true,
		"mappings":     true,
		"sbom":         true,
	}

	scanned := map[string]bool{}
	filesChecked := 0
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath := rel(filepath.Join(root, "internal"), path)
		if d.IsDir() {
			if relPath == "." {
				return nil
			}
			if relPath == "core" {
				return nil
			}
			if strings.HasPrefix(relPath, "core/") {
				scanned["core/"+strings.TrimPrefix(relPath, "core/")] = true
				return nil
			}
			if !topLevelCore[relPath] {
				return filepath.SkipDir
			}
			scanned[relPath] = true
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		filesChecked++
		for _, imp := range f.Imports {
			pkg := strings.Trim(imp.Path.Value, `"`)
			for name := range forbidden {
				if strings.HasSuffix(pkg, "/internal/"+name) {
					t.Errorf("%s imports language-specific package internal/%s (%s)",
						rel(root, path), name, pkg)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan internal/: %v", err)
	}
	for _, pkg := range []string{"core/atomicfile", "core/digest", "core/errs", "core/git", "core/lock", "core/modproof", "core/resolve", "core/vendor", "core/verify"} {
		if !scanned[pkg] {
			t.Fatalf("%s was not scanned — the physical extraction regressed", pkg)
		}
	}
	if len(scanned) != 9 {
		t.Fatalf("core packages scanned = %d, want 9 — the boundary scope changed: %v",
			len(scanned), scanned)
	}
	if filesChecked < 20 {
		t.Fatalf("only %d core files checked — the boundary net is vacuous", filesChecked)
	}
	t.Logf("core boundary: %d package paths, %d files checked", len(scanned), filesChecked)
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}
