package main

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV114EveryGoFileIsGofmtClean 把 CI 的 `gofmt -l .` 搬进本地。
//
// 为什么需要它（v0.114）：CI 早就有 `fmt-check` 这个 job，而我在本地**连续几轮**
// 把 `gofmt -l` 的输出当成噪声忽略了——因为 Windows 工作树里的文件是 CRLF，
// gofmt 会把**每一个**这样的文件都报成"未格式化"。于是这条真实的信号被
// 行尾问题淹没：六个真正没格式化的文件（`sarif.go`、`adopt.go`、`subprocess.go` …）
// 和几十个仅仅因为 CRLF 被报的文件混在同一份列表里，看不出区别。
//
// 冷启动的检验方式是一次 `git clone` 到临时目录再跑 `gofmt -l`——那才是
// CI 看到的东西（`.gitattributes` 把 `*.go` 强制成 LF）。本判据用同一份内容
// 但不需要克隆：读字节、把 CRLF 归一成 LF、交给标准库的 `go/format`。
//
// 归一化是**有意的**：这条判据问的是"格式对不对"，不是"行尾对不对"——
// 行尾由 `.gitattributes` 与 CI 的第二条命令负责，两件事分开才不会互相淹没。
func TestV114EveryGoFileIsGofmtClean(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	var bad []string
	scanned := 0
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			// `.git` 是版本库；其余是依赖与产物，CI 里也不会被 gofmt 扫到。
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
		scanned++
		out, ferr := format.Source(src)
		if ferr != nil {
			rel, _ := filepath.Rel(root, path)
			bad = append(bad, filepath.ToSlash(rel)+" (does not parse: "+ferr.Error()+")")
			return nil
		}
		if !bytes.Equal(out, src) {
			rel, _ := filepath.Rel(root, path)
			bad = append(bad, filepath.ToSlash(rel))
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}

	// 规模守卫：路径算错时这条判据会"扫到 0 个文件"从而静默通过，
	// 而静默通过比红更危险（v0.42 的教训）。
	if scanned < 100 {
		t.Fatalf("只扫到 %d 个 .go 文件——判据的范围缩了，它现在什么也证明不了", scanned)
	}
	if len(bad) > 0 {
		t.Errorf("%d 个 Go 文件未格式化（CI 的 `gofmt -l .` 会红）：\n  %s\n"+
			"修法：gofmt -w <file>。注意先在**冷启动的克隆**里核对行尾——"+
			"Windows 工作树的 CRLF 会让每一个文件都被报成未格式化，把真信号淹掉。",
			len(bad), strings.Join(bad, "\n  "))
	}
}
