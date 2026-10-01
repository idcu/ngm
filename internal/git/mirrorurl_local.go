package git

import (
	"os"
	"path/filepath"
	"strings"
)

// MirrorRemoteURLLocal 尝试**直接读** mirror 的 config 文件取出 remote.origin.url，
// 不启动任何 git 子进程。
//
// 第二个返回值为 false 表示"这份 config 的形状不在本函数敢解读的范围内"——
// 调用方**必须**回退到 `git config --get`。**返回 false 永远是安全的**，
// 它只意味着这次没省下那 30 毫秒。
//
// ## 为什么要有它（ADR-015）
//
// `git config --get remote.origin.url` 实测约 **30.6 ms/次**。在线 verify 里每个
// tag / branch 型依赖都要取一次地址（commit 型已在 ADR-015 里短路掉），
// 100 依赖时这是整份 verify 的 spawn 成本里约 **17%** 的一块。而这个值就明文写在
// mirror 的 `config` 里——那是 git 自己写的文件，ngm 从不改写它。
//
// ## 与 run:git 门禁的关系（顺带记一笔）
//
// 见 [remoteurl.go] 里 CheckRunAccess 的注释：权限判定的**顺序**之所以要小心，
// 是因为"判定去哪"本身曾经要跑一次 git（读 origin URL）。走本函数时**没有任何进程
// 被执行**，因此不涉及 run:git；被 `deny run:git` 拒绝的配置不会因为走了快路径就
// 得到别的结论——verify 后续仍需要 git（ls-remote / ls-tree），那里照样会被拒。
//
// ## 保守是设计的一部分
//
// 只在"形状最简单且无歧义"时作答，其余一律 false。宁可多起一次 git，
// 也不要**猜一个地址**：猜错的后果是拿错误的远端去解析 ref，而那种错误看起来
// 完全正常（ref 解析成功，只是对着另一个仓库）——这正是本项目最忌讳的失败形态。
//
// 接受的形状（必须全部满足）：
//
//   - 文件可读，且**不含** `[include]` / `[includeIf ...]` / `[extensions]`
//     （前两者 git 会展开，后者可能让 git 去读 `config.worktree`——都属
//     "git 读的文件比我们多"，那是唯一可能让两边答案不一致的来源）
//   - 恰好一个 `[remote "origin"]` 段；`[remote]`（无子段）与旧式点号写法
//     `[remote.origin]` 都**不**接受（它们同样定义 remote.origin.url，
//     只认其中一种就可能与 git 的取值顺序不一致）
//   - 该段内**恰好一个** `url` 键（多值时 `git config --get` 取最后一个，
//     这个语义我们不复刻——歧义交回 git）
//   - 值未被引号包裹、无续行、不含 `#` / `;`
//   - 值里只出现 `\\` 这一种转义（见下）
//
// ## 一处**实测才知道**的细节：未加引号的值同样会被转义
//
// Windows 上的本地路径 URL，git 写进 config 的是 `url = C:\\Users\\me\\repo.git`，
// 而 `git config --get` 回来的是 `C:\Users\me\repo.git`——**没有引号，但反斜杠成对**。
// 第一版实现直接原样返回文件里的文本，于是答案与 git 不同：一个"看起来合理但相反"
// 的值，正是本函数最不能犯的错。现在只实现 `\\` → `\` 这一种还原，
// 其余转义序列一律交回 git。
//
// 返回的值已按 git 的规则去掉首尾空白。
func MirrorRemoteURLLocal(mirrorPath string) (string, bool) {
	if strings.TrimSpace(mirrorPath) == "" {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(mirrorPath, "config"))
	if err != nil {
		return "", false
	}

	const (
		secNone = iota
		secOrigin
		secOther
	)

	state := secNone
	sawOrigin := false
	urlValue := ""
	urlCount := 0

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") {
			name, sub, ok := parseConfigSection(line)
			if !ok {
				return "", false // 头部写法不认识，不猜
			}
			switch {
			case strings.EqualFold(name, "include"), strings.EqualFold(name, "includeif"):
				return "", false // 会改变取值，但我们不展开
			case strings.EqualFold(name, "extensions"):
				// `extensions.worktreeConfig = true` 会让 git 继续读 config.worktree，
				// 而本函数只读这一个文件。见到就交回 git。
				return "", false
			case strings.HasPrefix(strings.ToLower(name), "remote."):
				return "", false // 旧式点号写法：同样定义 remote.origin.*
			case strings.EqualFold(name, "remote"):
				switch {
				case sub == "":
					return "", false // [remote]：没有子段，不是我们认得的形态
				case sub == "origin":
					if sawOrigin {
						return "", false // 重复段：取值顺序有歧义
					}
					sawOrigin = true
					state = secOrigin
				case strings.EqualFold(sub, "origin"):
					return "", false // "Origin"：大小写歧义交回 git
				default:
					state = secOther
				}
			default:
				state = secOther
			}
			continue
		}

		if state != secOrigin {
			continue
		}

		key, value, ok := splitConfigKeyValue(line)
		if !ok {
			return "", false
		}
		if !strings.EqualFold(key, "url") {
			continue
		}
		// 带引号 / 含可能是注释的字符：交回 git
		if strings.HasPrefix(value, `"`) || strings.ContainsAny(value, "#;") {
			return "", false
		}
		if strings.Contains(value, `\`) {
			unescaped, uok := unescapeConfigBackslashes(value)
			if !uok {
				return "", false
			}
			value = unescaped
		}
		urlCount++
		if urlCount > 1 {
			return "", false
		}
		urlValue = value
	}

	if !sawOrigin || urlCount != 1 || urlValue == "" {
		return "", false
	}
	return urlValue, true
}

// unescapeConfigBackslashes 只处理 `\\` → `\`；出现任何其他转义序列即返回 false。
//
// 为什么只认这一种：git 的转义规则不止一条（`\n` / `\t` / `\"` …），而复刻全部规则
// 意味着"我们自己写一个 git config 解析器"——那正是本文件刻意避免的事。
// 实测最常需要的那条（Windows 本地路径 URL 里的双反斜杠）在这里被正确还原，
// 其余情形一律回退子进程。
func unescapeConfigBackslashes(v string) (string, bool) {
	var b strings.Builder
	b.Grow(len(v))
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' {
			b.WriteByte(v[i])
			continue
		}
		if i+1 >= len(v) || v[i+1] != '\\' {
			return "", false
		}
		b.WriteByte('\\')
		i++
	}
	return b.String(), true
}

// parseConfigSection 解析 `[section]` / `[section "subsection"]`。
//
// 只接受 git 自己写出来的那两种形态；其余一律拒绝——宽松地猜头部会连带把取值位置猜错。
func parseConfigSection(line string) (section, subsection string, ok bool) {
	if !strings.HasSuffix(line, "]") {
		return "", "", false
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
	if inner == "" {
		return "", "", false
	}

	quote := strings.IndexByte(inner, '"')
	if quote < 0 {
		if strings.ContainsAny(inner, " \t\"") {
			return "", "", false
		}
		return inner, "", true
	}

	section = strings.TrimSpace(inner[:quote])
	rest := strings.TrimSpace(inner[quote:])
	if section == "" || len(rest) < 2 || !strings.HasPrefix(rest, `"`) || !strings.HasSuffix(rest, `"`) {
		return "", "", false
	}
	subsection = rest[1 : len(rest)-1]
	if strings.ContainsAny(subsection, `"\\`) || strings.ContainsAny(section, " \t") {
		return "", "", false
	}
	return section, subsection, true
}

// splitConfigKeyValue 解析 `key = value`。
//
// 键里不允许空白（git 也不允许）；没有 `=` 的行（git 里等价于布尔真）一律拒绝，
// 因为对 url 而言那种写法没有意义。
func splitConfigKeyValue(line string) (key, value string, ok bool) {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	value = strings.TrimSpace(line[eq+1:])
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return key, value, true
}
