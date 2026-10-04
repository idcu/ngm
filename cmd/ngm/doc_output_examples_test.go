package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV17DocOutputExamplesMatchTheGoldens 固定"文档里的**输出示例**必须与真实输出同形"。
//
// v0.15 的网（TestV15DocExamplesAreRealInvocations）钉的是示例**命令**必须成形；
// 这一张钉的是另一半：示例**输出**。此前没有任何东西看着它——而
// docs/architecture/observability.md 自己声明"输出即当前真实行为"，
// 它的 why 与 tree 两段示例却都在画渲染器**从不产生**的形状
// （树形方块 + `├──`；真实的 tree 只用缩进，真实的 why 是扁平的标签行）。
//
// 判据是**快照**：`cmd/ngm/testdata/*.golden` 由 `TestV02ObservabilityGolden`
// 从真实输出生成。两边先归一化（依赖标识、sha、digest、时间、数字 → 占位符），
// 再**逐行比较**。归一化只抹掉"值与实例"，**形状必须逐字相同**：
// 缩进、标签、标点、标记（`⚠` / `↺` / `…`）一个都不许漂。
//
// 它证明什么、不证明什么：它证明文档示例与快照**同形**，不证明示例覆盖了所有输出形态
// （那要靠 golden 自己长）。改渲染器时，这张网与 golden 会一起红——那是设计意图：
// 输出的主人是快照，文档只是它的镜子。
func TestV17DocOutputExamplesMatchTheGoldens(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "architecture", "observability.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	for _, c := range []struct {
		section string
		golden  string
	}{
		{"## ngm why", "why.golden"},
		{"## ngm tree", "tree.golden"},
	} {
		t.Run(c.golden, func(t *testing.T) {
			goldenRaw, err := os.ReadFile(testutils.GoldenPath(t, c.golden))
			if err != nil {
				t.Fatal(err)
			}
			want := normalizeExample(string(goldenRaw))
			got := normalizeExample(fencedBlockUnder(t, doc, c.section, "### 输出"))

			if got == "" {
				t.Fatalf("no output example found under %q: the doc must show what the command prints", c.section)
			}
			if got != want {
				t.Errorf("the output example in observability.md no longer matches the snapshot %s\n"+
					"--- doc (normalized) ---\n%s\n--- snapshot (normalized) ---\n%s", c.golden, got, want)
			}
		})
	}
}

// fencedBlockUnder 取出 `section` 之下、`sub` 之后的**第一个**围栏代码块内容。
func fencedBlockUnder(t *testing.T, doc, section, sub string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")

	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == section {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("section %q not found in the document", section)
	}

	subAt := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			break // 出了这一节
		}
		if strings.TrimSpace(lines[i]) == sub {
			subAt = i
			break
		}
	}
	if subAt < 0 {
		t.Fatalf("subsection %q not found under %q", sub, section)
	}

	open := -1
	for i := subAt; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "```") {
			open = i
			break
		}
	}
	if open < 0 {
		t.Fatalf("no fenced block under %q/%q", section, sub)
	}
	for i := open + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "```") {
			return strings.Join(lines[open+1:i], "\n")
		}
	}
	t.Fatalf("unterminated fenced block under %q/%q", section, sub)
	return ""
}

var (
	// reExampleDigest 先于 sha 替换：digest 里是 64 位十六进制，先抹掉才不会只被抹一半。
	reExampleDigest = regexp.MustCompile(`sha256:[0-9a-f.]+`)
	reExampleSHA    = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	// 依赖标识：`github:o/r`、`github:o/r@ref`、`github:o/r#sub@ref`、`github.com:o/r`。
	reExampleDep   = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9.-]*:[A-Za-z0-9._-]+/[A-Za-z0-9._-]+(#[A-Za-z0-9._-]+)?(@[A-Za-z0-9._/-]+)?`)
	reExampleTime  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}Z?`)
	reExampleCount = regexp.MustCompile(`\d+`)
)

// normalizeExample 抹掉"值与实例"，只留下形状。
//
// 顺序有讲究：digest → sha → 依赖 → 时间 → 数字。反过来的话，
// 依赖规则会先吃掉 `@v1`，而 sha 规则会啃到 digest 的一半。
func normalizeExample(s string) string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = reExampleDigest.ReplaceAllString(line, "sha256:<digest>")
		line = reExampleSHA.ReplaceAllString(line, "<sha>")
		line = reExampleDep.ReplaceAllString(line, "<dep>")
		line = reExampleTime.ReplaceAllString(line, "<time>")
		line = reExampleCount.ReplaceAllString(line, "<n>")
		out = append(out, strings.TrimRight(line, " \t"))
	}
	return strings.Join(out, "\n")
}
