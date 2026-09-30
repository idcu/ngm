package main

import (
	"regexp"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV02ObservabilityGolden 锁定 why / tree / outdated 的**输出格式**。
//
// 为什么需要快照而不是只有断言：这三个命令的输出格式（缩进、`└──`、`⚠ ✗ ↺`
// 标记、`unknown` vs `no` 的措辞）在 observability.md 里被当作承诺写下来了，
// 而 observability.md 又说"输出即当前真实行为"。断言测试保证语义正确，
// 快照测试保证**没有人悄悄改掉格式**——两者覆盖的是不同的失败模式。
//
// commit 短哈希与时间戳每轮都不同，因此先归一化再比对：
// 快照钉的是格式，不是值。
func TestV02ObservabilityGolden(t *testing.T) {
	setup := func(t *testing.T) string {
		t.Helper()
		isolateUserEnv(t)
		scUpstream(t, "github:snap/leaf", "export const leaf = 1\n", "")
		parent := `{"name":"snap-parent","version":"1.0.0","runtime":"node",` +
			`"dependencies":[{"name":"github:snap/leaf","ref":"v1","refType":"tag"}]}`
		scUpstream(t, "github:snap/parent", "export const parent = 1\n", parent)

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:snap/parent@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		return proj
	}

	t.Run("tree", func(t *testing.T) {
		proj := setup(t)
		code, out := runCaptureCode(t, "tree", "--offline", "--dir="+proj)
		if code != 0 {
			t.Fatalf("tree exit=%d:\n%s", code, out)
		}
		testutils.GoldenString(t, "tree.golden", normalizeOutput(out))
	})

	t.Run("why", func(t *testing.T) {
		proj := setup(t)
		code, out := runCaptureCode(t, "why", "github:snap/leaf", "--dir="+proj)
		if code != 0 {
			t.Fatalf("why exit=%d:\n%s", code, out)
		}
		testutils.GoldenString(t, "why.golden", normalizeOutput(out))
	})

	t.Run("outdated", func(t *testing.T) {
		proj := setup(t)
		code, out := runCaptureCode(t, "outdated", "--offline", "--dir="+proj)
		if code != 0 {
			t.Fatalf("outdated exit=%d:\n%s", code, out)
		}
		testutils.GoldenString(t, "outdated.golden", normalizeOutput(out))
	})
}

// reSHA 匹配 commit 短哈希（7–40 位十六进制）。
//
// 注意它**不会**误伤 archiveDigest：`sha256:<64 hex>` 里那 64 个字符没有内部
// 词边界，匹配不上 `\b…\b`。digest 因此留在快照里——它是内容寻址的、每轮相同，
// 留着才有意义（若哪天它变得不确定，这条快照会立刻失败）。
var reSHA = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)

// reTimestamp 匹配 RFC3339 时间戳（lock 的 resolvedAt）。
var reTimestamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z?`)

// normalizeOutput 把每轮都会变的值替换为占位符，使快照可比。
//
// 归一化的东西必须**穷举**：漏掉一个就会变成随机失败的测试，
// 而随机失败的测试比没有测试更糟。（第一版就漏了 resolvedAt，
// 是"不更新时再跑一遍"这一步把它抓出来的。）
func normalizeOutput(s string) string {
	s = reSHA.ReplaceAllString(s, "<sha>")
	return reTimestamp.ReplaceAllString(s, "<time>")
}
