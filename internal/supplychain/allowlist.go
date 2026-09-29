// Package supplychain 实现供应链策略的加载与判定。
//
// 执行时机、作用域与失败语义见 ADR-009：策略在**解析阶段**生效（早于任何远端访问），
// 对**传递依赖同样生效**，失败即 exit 3 并输出完整来源链。
package supplychain

import "strings"

// MatchRepoPattern 判断 `host/org/repo` 是否命中白名单模式。
//
// 语义（ADR-009）：
//
//	`*`   不跨 `/`：github.com/my-org/*   匹配该 org 下的仓库
//	`**`  跨层：   gitlab.example.com/** 匹配任意深度
//
// 通配只在**段内**展开，段数必须一致（除非用了 `**`）——因此 `github.com/*`
// 不会意外放行 `github.com/org/repo`。这条限制是刻意的：白名单里"多一层"往往
// 意味着用户以为限定了一个组织，实际却放行了所有组织。
//
// 比较是 **ASCII 大小写不敏感**的。理由：Git host 与主流托管平台的 org/repo 名称
// 本身大小写不敏感（GitHub 把 `My-Org` 与 `my-org` 视为同一个仓库），
// 若这里区分大小写，就会出现"配置写对了、却因为大小写被拒"的失败。
// 忽略大小写只是接受同一仓库的另一种写法，不会放行另一个仓库。
func MatchRepoPattern(pattern, target string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(target, "/"))
}

func matchSegments(pat, target []string) bool {
	if len(pat) == 0 {
		return len(target) == 0
	}
	if pat[0] == "**" {
		// `**` 匹配 0 到 n 段
		for i := 0; i <= len(target); i++ {
			if matchSegments(pat[1:], target[i:]) {
				return true
			}
		}
		return false
	}
	if len(target) == 0 {
		return false
	}
	if !matchSegment(pat[0], target[0]) {
		return false
	}
	return matchSegments(pat[1:], target[1:])
}

// matchSegment 做单段通配匹配：`*` 在该段内匹配任意字符序列。
//
// 用迭代 + 回溯点实现（而不是正则）：输入来自配置与依赖名，长度可控，
// 但递归回溯在 `a*a*a*a*` 这类模式上会退化，迭代版本没有这个问题。
func matchSegment(pat, s string) bool {
	pi, si := 0, 0
	star, mark := -1, 0

	for si < len(s) {
		if pi < len(pat) && pat[pi] == '*' {
			star = pi
			mark = si
			pi++
			continue
		}
		if pi < len(pat) && eqFoldByte(pat[pi], s[si]) {
			pi++
			si++
			continue
		}
		if star >= 0 {
			// 回溯：让上一个 `*` 多吞一个字符
			pi = star + 1
			mark++
			si = mark
			continue
		}
		return false
	}
	// 模式尾部剩余的 `*` 可以匹配空串
	for pi < len(pat) && pat[pi] == '*' {
		pi++
	}
	return pi == len(pat)
}

// eqFoldByte 比较两个字节，ASCII 大小写不敏感。
func eqFoldByte(a, b byte) bool {
	if 'a' <= a && a <= 'z' {
		a -= 'a' - 'A'
	}
	if 'a' <= b && b <= 'z' {
		b -= 'a' - 'A'
	}
	return a == b
}
