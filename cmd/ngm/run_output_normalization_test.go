package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// **归一化清单**（v0.51 立，起因是同一个形状踩了四次）
//
// 这一族缺陷的形态每次都一样：判据"造一份全新世界跑两次、比较输出"，
// 而两次 run 各自 `isolateUserEnv` 到**不同的** temp 目录——
// 于是输出里那句"本次运行生成的路径"不同，判据把"路径不同"当成"两种写法不等价"。
//
//	· v0.45 `store prune` 印 content store 的绝对路径
//	· v0.47 hint 里印隔离 home 的 `~/.ngm/config.json`
//	· v0.50 两个通道各跑一次，两边路径不同
//	· v0.51 错误信封里印锁文件路径（而 JSON 里它是**转义**过的 `C:\\…`）
//
// 四次里前三次都是**当场**在各自的网里补了一个 `strings.ReplaceAll`，
// 于是下一次换一张网又踩。这一版把规则收成一处：
//
//	**凡是要比较两次运行的输出，先过 normalizeRunPaths**——
//	它按三种形态替换：原样、JSON 转义后、以及斜杠归一化后。
//
// 为什么必须包含转义形态：JSON 里的字符串是**被转义**的文本
// （反斜杠写成 `\\`），拿原样的路径去 `ReplaceAll` 会**匹配不上**，
// 而匹配不上的表现与"两个通道说的不是同一句话"一模一样。
//
// 纪律怎么写进代码：助手只做替换、不做判断；**忘记调它**仍然可能发生，
// 但每次发生的表现都是同一句"输出不相等"，而这句话现在指向这里。
func normalizeRunPaths(t *testing.T, s string, paths ...string) string {
	t.Helper()
	for _, p := range paths {
		if p == "" {
			continue
		}
		s = strings.ReplaceAll(s, p, "<path>")
		if esc, err := json.Marshal(p); err == nil && len(esc) > 2 {
			// 去掉首尾引号，得到"字符串值在 JSON 里的样子"
			s = strings.ReplaceAll(s, string(esc[1:len(esc)-1]), "<path>")
		}
		s = strings.ReplaceAll(s, filepath.ToSlash(p), "<path>")
	}
	return s
}
