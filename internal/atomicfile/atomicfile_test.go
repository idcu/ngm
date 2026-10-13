package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// v0.82：原子写是 ngm 写盘的唯一实现处（此前三份，见包注释）。

func TestV82WriteProducesAReadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ngm.json")
	if err := Write(path, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the written file must be readable by another user: %v", err)
	}
	if string(data) != `{"a":1}` {
		t.Errorf("content = %q", data)
	}
}

// TestV82WriteIsWorldReadable 钉住权限。
//
// 这是 v0.82 的**主要发现**：`os.CreateTemp` 建的文件是 0600，rename 之后
// 目标文件就变成 0600。v0.66 在 lock 那一份修了它（加了 Chmod），
// 但 config 与 mappings 那份**至今没有**——于是 `ngm.json` 与
// `ngm.mappings.json` 每次原子写之后都是 0600。
// 而这两个文件与 lock 一样，是要**提交进 Git、供团队共享**的。
//
// 牙齿：删掉 atomicfile.Write 里的 `tmp.Chmod(Perm)` ⇒ 本测试红。
func TestV82WriteIsWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 的 Chmod 只影响只读位；权限语义不同")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "ngm.json")
	if err := Write(path, []byte("{}")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != Perm {
		t.Errorf("mode = %o, want %o（0600 会让同机其他用户与部分 CI 缓存读不到它）", got, Perm)
	}
	if got := info.Mode().Perm(); got&0o044 == 0 {
		t.Errorf("mode %o is not readable by group/other", got)
	}
}

// TestV82ChmodHappensBeforeRename 平台无关的那半边（Windows 也生效）。
//
// 为什么需要它：上面那条权限判据在 Windows 上是 `t.Skip`
// （那里的 Chmod 只影响只读位，测不出 0600/0644 的差别）——
// 于是**在这台开发机上它是绿的，却什么也没看**。这正好是本项目反复记过的
// 那种形态：「绿」不等于「看见了什么」。
//
// 补一条**源码级**判据：权限必须在 rename **之前**设置（rename 之后改会让
// 文件短暂以错误权限存在，且那时它已经以目标名字可见了）。
// 与 `internal/git/spawn_gate_test.go` 同族，并带**空跑保护**——
// 扫不到实现文件时必须红，否则判据会在文件改名后静默失效。
func TestV82ChmodHappensBeforeRename(t *testing.T) {
	const src = "atomicfile.go"
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("空跑保护：读不到实现文件 %s：%v", src, err)
	}
	text := string(data)

	chmodAt := strings.Index(text, "tmp.Chmod(")
	renameAt := strings.Index(text, "os.Rename(")
	if chmodAt < 0 {
		t.Fatal("空跑保护：实现里找不到 tmp.Chmod( —— 判据已失效（或权限设置被删掉了）")
	}
	if renameAt < 0 {
		t.Fatal("空跑保护：实现里找不到 os.Rename( —— 判据已失效")
	}
	if chmodAt > renameAt {
		t.Errorf("Chmod 出现在 rename 之后（%d > %d）："+
			"rename 之后改权限会让文件短暂以错误权限存在于目标路径上", chmodAt, renameAt)
	}
	if !strings.Contains(text, "Perm = 0o644") {
		t.Error("Perm 常量应为 0o644（这些文件要提交进 Git、供团队共享）")
	}
}

// TestV82WriteLeavesNoTempResidue 失败与成功两条路径都不留 `.ngm-*.tmp`。
func TestV82WriteLeavesNoTempResidue(t *testing.T) {
	dir := t.TempDir()
	if err := Write(filepath.Join(dir, "a.json"), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	// 失败路径：目标是一个目录 ⇒ rename 必然失败
	if err := Write(dir, []byte("{}")); err == nil {
		t.Fatal("writing to a directory path must fail")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if len(e.Name()) > 4 && e.Name()[:5] == ".ngm-" {
			t.Errorf("temp residue left behind: %s", e.Name())
		}
	}
}

// TestV82WriteReplacesAtomically 覆盖既有文件时，内容整体替换（不留旧字节）。
func TestV82WriteReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ngm.lock")
	if err := Write(path, []byte("this is a much longer first version of the file")); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("short")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "short" {
		t.Errorf("after replacement content = %q, want \"short\"（残留了旧字节 = 非原子）", data)
	}
}
