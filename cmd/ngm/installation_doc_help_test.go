package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestV115InstallationDocListsTheSameCommandsAsHelp 把一条**手写纪律**变成判据。
//
// `help_golden_test.go` 的注释里写着新增命令要同步三处：`guides/cli.md`、
// `installation.md` 的 help 片段、以及 golden 本身。前两处一直只有"记得去做"，
// 而 golden 那条是自动的——于是它们各自漂了：
//
//	testdata/help.golden      27 条（`TestHelpGolden` 守着）
//	guides/installation.md    21 条（没有任何东西守着）
//
// 差的是 `adopt` / `transform` / `store` / `sbom` / `attest` 五条命令，外加
// `integrations` 的描述早已改过。**一份复制了 help 却没有判据的文档，唯一确定的事是它会旧。**
//
// 判据按"命令名 + 描述"逐条比对，顺序也算——因为这份片段的用途正是"让人看到 --help 长什么样"，
// 顺序不同就已经不是它了。
func TestV115InstallationDocListsTheSameCommandsAsHelp(t *testing.T) {
	want := commandsFromUsage(t, rootUsage)

	rel := filepath.Join("..", "..", "docs", "guides", "installation.md")
	body, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	got := commandsFromInstallationDoc(string(body))

	if len(got) == 0 {
		t.Fatalf("在 %s 里没有找到 help 片段（`# COMMANDS:` 到 `# FLAGS:` 之间的 `#   <cmd>  <desc>` 行）——判据的范围缩了", rel)
	}
	if len(want) == 0 {
		// 解析 rootUsage 失败会让两边都空而"相等"，那是这条判据最坏的失败方式。
		t.Fatal("从 rootUsage 里解析不出任何命令——判据本身坏了")
	}

	if len(want) != len(got) {
		t.Errorf("命令数不一致：--help 有 %d 条，%s 有 %d 条", len(want), rel, len(got))
	}
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	for i := 0; i < n; i++ {
		if want[i] != got[i] {
			t.Errorf("第 %d 条不一致:\n  --help: %s\n  文档:   %s", i+1,
				formatCmd(want[i]), formatCmd(got[i]))
		}
	}
	// 多出来的（只在一侧）单独说清楚，否则上面只报"第 N 条不一致"，看不出谁漏了谁。
	for i := n; i < len(want); i++ {
		t.Errorf("文档缺这一条: %s", formatCmd(want[i]))
	}
	for i := n; i < len(got); i++ {
		t.Errorf("文档多了这一条（--help 里没有）: %s", formatCmd(got[i]))
	}
}

var (
	reUsageCommand = regexp.MustCompile(`^  (\S+)\s{2,}(\S.*)$`)
	reDocCommand   = regexp.MustCompile(`^#   (\S+)\s{2,}(\S.*)$`)
)

// commandsFromUsage 取 rootUsage 的 COMMANDS 段，归一成 `name\tdesc`。
func commandsFromUsage(t *testing.T, usage string) []string {
	t.Helper()
	var out []string
	in := false
	for _, line := range strings.Split(usage, "\n") {
		switch {
		case strings.HasPrefix(line, "COMMANDS:"):
			in = true
			continue
		case in && strings.TrimSpace(line) == "":
			in = false
			continue
		}
		if !in {
			continue
		}
		if m := reUsageCommand.FindStringSubmatch(line); m != nil {
			out = append(out, m[1]+"\t"+strings.TrimSpace(m[2]))
		}
	}
	return out
}

// commandsFromInstallationDoc 取文档里那段 `# COMMANDS:` … `# FLAGS:` 的片段。
func commandsFromInstallationDoc(doc string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "# COMMANDS:":
			in = true
			continue
		case in && (trimmed == "# FLAGS:" || trimmed == "#" || trimmed == "```"):
			in = false
			continue
		}
		if !in {
			continue
		}
		if m := reDocCommand.FindStringSubmatch(line); m != nil {
			out = append(out, m[1]+"\t"+strings.TrimSpace(m[2]))
		}
	}
	return out
}

func formatCmd(s string) string {
	name, desc, _ := strings.Cut(s, "\t")
	return name + " — " + desc
}
