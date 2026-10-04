package integrations

import (
	"os"
	"path/filepath"
	"testing"
)

// TestV12ApplyIsAllOrNothingOnWriteFailure 固定 Apply 声明的那条不变量：
// **一个字节都不写**——不只是"没有冲突时不写"，I/O 失败时同样成立。
//
// 此前那道门只挡冲突：真正写盘是一个循环，写到第 k 个失败时前 k-1 个已经落盘，
// 于是磁盘上留下半套脚手架，而错误只点名失败的那个文件，
// 不告诉用户已经写下了什么。这个包在文件头写明的正是"全有或全无"。
//
// 构造方式：让**第二个**产物的父路径是一个普通文件（MkdirAll 必然失败），
// 于是第一个产物的临时文件必须被清掉、且不能被改名就位。
func TestV12ApplyIsAllOrNothingOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "blocked"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	arts := []Artifact{
		{Path: "ok/b.txt", Content: []byte("first\n"), Owned: true},
		{Path: "blocked/a.txt", Content: []byte("second\n"), Owned: true},
	}

	if _, err := Apply(dir, arts, false); err == nil {
		t.Fatal("expected the write to fail (the parent of the second artifact is a file)")
	}

	if _, err := os.Stat(filepath.Join(dir, "ok", "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("a half-applied scaffold must not survive: ok/b.txt exists (stat err=%v)", err)
	}

	// 临时文件同样不能留下：否则下次运行会看到"ngm 留下的、没人认领的东西"。
	leftover, _ := filepath.Glob(filepath.Join(dir, "*", "*.ngm-tmp"))
	if len(leftover) > 0 {
		t.Errorf("temp files must be cleaned up on failure, found: %v", leftover)
	}
}

// TestV12ApplyStillWritesWhenNothingFails 是上面那条的**对照**：
// 一个"永远失败"的实现也能让"什么都没写"变绿，所以必须有"正常路径仍然成功"在旁。
func TestV12ApplyStillWritesWhenNothingFails(t *testing.T) {
	dir := t.TempDir()
	arts := []Artifact{
		{Path: "ok/b.txt", Content: []byte("first\n"), Owned: true},
		{Path: "ok/deep/c.txt", Content: []byte("second\n"), Owned: true},
	}

	outcomes, err := Apply(dir, arts, false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes=%d want 2", len(outcomes))
	}
	for _, a := range arts {
		body, rerr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(a.Path)))
		if rerr != nil {
			t.Fatalf("artifact %s was not written: %v", a.Path, rerr)
		}
		if string(body) != string(a.Content) {
			t.Errorf("%s content=%q want %q", a.Path, body, a.Content)
		}
	}
	if leftover, _ := filepath.Glob(filepath.Join(dir, "*", "*.ngm-tmp")); len(leftover) > 0 {
		t.Errorf("no temp file may survive a successful run, found: %v", leftover)
	}
}
