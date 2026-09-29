package vendor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// treePair 建一对内容相同的目录树（模拟 vendor 树与 content 树）。
func treePair(t *testing.T, files map[string]string) (vendorTree, contentTree string) {
	t.Helper()
	root := t.TempDir()
	vendorTree = filepath.Join(root, "vendor")
	contentTree = filepath.Join(root, "content")
	for _, dir := range []string{vendorTree, contentTree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for rel, body := range files {
			writeTreeFile(t, dir, rel, body)
		}
	}
	return vendorTree, contentTree
}

func writeTreeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestVerifyTreeShallowVersusDeep 冻结**浅校验与深校验的能力差异**。
//
// 这个差异是 `ngm verify --deep` 存在的全部理由（architecture/observability.md
// §ngm verify 把"逐文件哈希"列为可选增强），因此必须由测试固定，
// 而不是靠注释里的约定——否则未来有人"顺手"把浅校验换成全量哈希，
// 或把 --deep 做成空操作，都不会被发现。
//
//	浅校验：路径集合 + symlink 目标 + 文件大小（不读内容）
//	深校验：在浅校验之上再做逐文件 sha256
func TestVerifyTreeShallowVersusDeep(t *testing.T) {
	base := map[string]string{
		"index.ts":    "export const x = 1\n",
		"nested/a.ts": "aaaa\n",
		"nested/b.ts": "bbbb\n",
	}

	t.Run("identical trees pass both", func(t *testing.T) {
		v, c := treePair(t, base)
		for _, tc := range []struct {
			name string
			fn   func(string, string) (VerifyResult, error)
		}{
			{"shallow", VerifyVendorTreeShallow},
			{"deep", VerifyVendorTree},
		} {
			res, err := tc.fn(v, c)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !res.OK() {
				t.Errorf("%s should pass on identical trees: %v", tc.name, res.Mismatches)
			}
			if res.Files != 3 {
				t.Errorf("%s counted %d files, want 3", tc.name, res.Files)
			}
		}
	})

	// -------------------------------------------------------------------
	// 核心：等长篡改——浅校验看不见，深校验必须看见
	// -------------------------------------------------------------------
	t.Run("same-size tampering is invisible to shallow and caught by deep", func(t *testing.T) {
		v, c := treePair(t, base)
		writeTreeFile(t, v, "nested/a.ts", "AAAA\n") // 与 "aaaa\n" 同为 5 字节

		shallow, err := VerifyVendorTreeShallow(v, c)
		if err != nil {
			t.Fatal(err)
		}
		if !shallow.OK() {
			t.Fatalf("shallow must not read file contents, but it reported: %v", shallow.Mismatches)
		}

		deep, err := VerifyVendorTree(v, c)
		if err != nil {
			t.Fatal(err)
		}
		if deep.OK() {
			t.Fatal("deep must catch a same-size content tampering")
		}
		joined := strings.Join(deep.Mismatches, "\n")
		if !strings.Contains(joined, "nested/a.ts") || !strings.Contains(joined, "content differs") {
			t.Errorf("deep mismatch should name the file and the reason:\n%s", joined)
		}
	})

	// -------------------------------------------------------------------
	// 变长篡改：浅校验也能看见（这是默认模式的检出能力上限）
	// -------------------------------------------------------------------
	t.Run("size change is caught by both", func(t *testing.T) {
		v, c := treePair(t, base)
		writeTreeFile(t, v, "nested/a.ts", "aaaaaa\n") // 7 字节

		for _, tc := range []struct {
			name string
			fn   func(string, string) (VerifyResult, error)
		}{
			{"shallow", VerifyVendorTreeShallow},
			{"deep", VerifyVendorTree},
		} {
			res, err := tc.fn(v, c)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if res.OK() {
				t.Errorf("%s must catch a size change", tc.name)
			}
			if joined := strings.Join(res.Mismatches, "\n"); !strings.Contains(joined, "size differs") {
				t.Errorf("%s mismatch should report the size delta:\n%s", tc.name, joined)
			}
		}
	})

	// -------------------------------------------------------------------
	// 缺失与多余：与内容无关，两者都必须看见
	// -------------------------------------------------------------------
	t.Run("missing and extra files are caught by both", func(t *testing.T) {
		v, c := treePair(t, base)
		if err := os.Remove(filepath.Join(v, "nested", "b.ts")); err != nil {
			t.Fatal(err)
		}
		writeTreeFile(t, v, "ghost.ts", "x\n")

		for _, tc := range []struct {
			name string
			fn   func(string, string) (VerifyResult, error)
		}{
			{"shallow", VerifyVendorTreeShallow},
			{"deep", VerifyVendorTree},
		} {
			res, err := tc.fn(v, c)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if res.OK() {
				t.Errorf("%s must catch missing/extra files", tc.name)
			}
			joined := strings.Join(res.Mismatches, "\n")
			if !strings.Contains(joined, "present in the content store but missing from vendor") {
				t.Errorf("%s should report the missing file:\n%s", tc.name, joined)
			}
			if !strings.Contains(joined, "present in vendor but missing from the content store") {
				t.Errorf("%s should report the extra file:\n%s", tc.name, joined)
			}
		}
	})

	// -------------------------------------------------------------------
	// symlink 目标：两者都比对（Windows 需要开发者模式，无法创建则跳过）
	// -------------------------------------------------------------------
	t.Run("symlink target mismatch is caught by both", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks are not reliably available on Windows")
		}
		v, c := treePair(t, base)
		if err := os.Remove(filepath.Join(v, "nested", "a.ts")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../index.ts", filepath.Join(v, "nested", "a.ts")); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}

		for _, tc := range []struct {
			name string
			fn   func(string, string) (VerifyResult, error)
		}{
			{"shallow", VerifyVendorTreeShallow},
			{"deep", VerifyVendorTree},
		} {
			res, err := tc.fn(v, c)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if res.OK() {
				t.Errorf("%s must catch a symlink that replaced a regular file", tc.name)
			}
		}
	})
}

// TestVerifyTreeShallow_MissingTreeIsNotEmpty 固定"不存在的树视为空树"的约定。
//
// 调用方（ngm verify 的落地检查）需要区分"目录不存在"与"目录存在但没有文件"，
// 前者由它自己 os.Stat 判定，后者在这里表现为"content 侧有文件、vendor 侧没有"。
func TestVerifyTreeShallow_MissingTreeIsNotEmpty(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "content")
	if err := os.MkdirAll(content, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTreeFile(t, content, "a.ts", "1\n")

	res, err := VerifyVendorTreeShallow(filepath.Join(root, "does-not-exist"), content)
	if err != nil {
		t.Fatalf("a missing tree must not be an error: %v", err)
	}
	if res.OK() {
		t.Fatal("a missing tree must not compare equal to a non-empty one")
	}
}
