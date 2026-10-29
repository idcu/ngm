// Package envutil 提供环境变量列表的最小工具。
//
// 为什么单独成包（v0.105）：`deny: ["env:X"]` 这条策略有**三个**施加点——
// git 子进程（internal/core/git）、Deno 沙箱（internal/security）、
// 引擎子进程（internal/adapter）。共享语义只有一处，否则"修好一个、漏掉另一个"
// 是这类缺陷最稳定的复发形态：v0.105 正是在引擎侧发现它没被施加，
// 而 git 与沙箱两侧都做对了。
//
// 包放成叶子（不依赖任何 internal 包），使三个施力点都能引用它而不产生环。
package envutil

import "strings"

// Without 返回 env 去掉 drop 中列出的变量名后的副本。
//
// 变量名按**大小写不敏感**匹配（Windows 的环境变量不区分大小写，
// 而在 Unix 上多过滤一个大小写变体不会造成误伤——真正的风险是漏掉）。
// 同时接受 `NAME=value` 形态与裸 `NAME` 形态。
//
// drop 为空时原样返回（不复制）：调用方在绝大多数情况下没有拒绝项，
// 那条路径不该付出一次分配的代价。
func Without(env, drop []string) []string {
	if len(drop) == 0 {
		return env
	}
	dropSet := make(map[string]bool, len(drop))
	for _, d := range drop {
		dropSet[strings.ToUpper(d)] = true
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		name := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			name = e[:i]
		}
		if dropSet[strings.ToUpper(name)] {
			continue
		}
		out = append(out, e)
	}
	return out
}
