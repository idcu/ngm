package integrations

import (
	"fmt"
	"strings"
)

// diffLines 生成紧凑的行级差异，用于"用户自己的配置文件与 ngm 期望内容不一致"时。
//
// 只保留公共前后缀之外的部分：用户需要知道的是**要改哪几行**，
// 而不是把整份文件重读一遍。输出用 `-`（当前文件）/ `+`（ngm 期望）标记，
// 与 `git diff` 的习惯一致。
func diffLines(current, want string) string {
	cur := splitLines(current)
	exp := splitLines(want)

	// 公共前缀
	start := 0
	for start < len(cur) && start < len(exp) && cur[start] == exp[start] {
		start++
	}
	// 公共后缀
	endCur, endExp := len(cur), len(exp)
	for endCur > start && endExp > start && cur[endCur-1] == exp[endExp-1] {
		endCur--
		endExp--
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "  @@ after line %d @@\n", start)
	for _, l := range cur[start:endCur] {
		fmt.Fprintf(&sb, "  - %s\n", l)
	}
	for _, l := range exp[start:endExp] {
		fmt.Fprintf(&sb, "  + %s\n", l)
	}
	if sb.Len() == 0 {
		// 理论上不可达（内容相同会在更早的地方判为 up to date），
		// 但空 diff 会让人以为"没差别却报冲突"，宁可明说。
		return "  (files differ only in trailing whitespace or line endings)\n"
	}
	return sb.String()
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
