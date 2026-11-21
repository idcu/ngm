package git

import (
	"os"
	"strings"
	"testing"
)

// TestV147GitRunCapturesAreBounded 固定审计 C-05：
// 通用 Run / RunAllowFailure 此前用无界 bytes.Buffer 捕获子进程输出。
// 证据是审计报告里已发生的缺陷条目，不是假想防御；这是替换式守卫，不新增网体系。
func TestV147GitRunCapturesAreBounded(t *testing.T) {
	for _, file := range []string{"exec.go", "runallow.go"} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("cannot read %s: %v", file, err)
		}
		src := string(body)
		if !strings.Contains(src, "boundbuf.New(boundbuf.DefaultLimit)") {
			t.Errorf("%s: git output capture is not bounded", file)
		}
		if strings.Contains(src, "var stdout, stderr bytes.Buffer") {
			t.Errorf("%s: an unbounded bytes.Buffer is still used for subprocess output", file)
		}
	}
}
