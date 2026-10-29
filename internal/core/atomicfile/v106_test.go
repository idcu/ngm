package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

// v0.106：rename 之后必须同步**目录项**。
//
// 缺陷形态：`tmp.Sync()` 只把文件内容推盘，不保证"这个目录里多了一个名字"
// 也落了盘。崩溃后可能看到"内容在、文件名不在"，而这个函数写的是
// lock / ngm.json / ngm.mappings.json / sbom 与 attest 的 --out——
// 后两样是**要被签名的证据**。content store 一直在做这一步，这里此前没有。
func TestV106WriteSyncsTheDirectoryEntry(t *testing.T) {
	orig := syncDir
	t.Cleanup(func() { syncDir = orig })

	var got string
	syncDir = func(dir string) { got = dir }

	target := filepath.Join(t.TempDir(), "ngm.lock")
	if err := Write(target, []byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got == "" {
		t.Fatal("Write() never asked for a directory sync — " +
			"the file content is durable but the name may not be")
	}
	if want := filepath.Dir(target); got != want {
		t.Errorf("synced %q, want the file's own directory %q", got, want)
	}

	// 内容仍然要正确（同步不能顶替写入）。
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello\n" {
		t.Errorf("content = %q", body)
	}
}

// 失败路径**不该**同步目录：临时文件都没 rename 成功，目录项没有任何变化。
// 这条断言防的是"把 syncDir 提到 rename 之前"这种看起来无害的重排。
func TestV106NoDirectorySyncWhenTheWriteFails(t *testing.T) {
	orig := syncDir
	t.Cleanup(func() { syncDir = orig })

	called := false
	syncDir = func(string) { called = true }

	// 目标目录不存在 ⇒ CreateTemp 失败。
	if err := Write(filepath.Join(t.TempDir(), "no-such-dir", "x.lock"), []byte("y")); err == nil {
		t.Fatal("writing into a missing directory must fail")
	}
	if called {
		t.Error("a failed write must not claim to have synced a directory entry")
	}
}
