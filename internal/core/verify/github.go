package verify

import (
	"fmt"
	"strings"
)

// GitHub Actions 注解渲染（`ngm verify --format=github`）。
//
// 为什么需要它（v0.105）：SARIF 要上传到 code scanning（需要 `security-events:
// write` 权限、且私有仓库要 Advanced Security），而绝大多数项目只想要
// **PR 上的一个红叉 + 一句人话**。workflow command 是那条路的最小形态：
// 直接写在 stdout，GitHub 会把它变成注解。
//
// 格式（GitHub 文档，workflow commands）：
//
//	::error title=<title>::<message>
//
// 转义规则只有三条，但**必须做**：`%` → `%25`、`\r` → `%0D`、`\n` → `%0A`。
// 少做一条的后果不是"注解难看"，而是"注解被截断/伪造"——依赖名来自 lock，
// 而 lock 里的字符串可以是任何东西。

// RenderGitHub 把一次 verify 渲染成 GitHub Actions 注解。
//
// 只输出**失败项**：成功的依赖不需要注解（PR 上几十条"ok"会把真正的红叉埋掉）。
// 一条都不失败时输出为空字符串——调用方照常按退出码判定。
func RenderGitHub(rep *Report) string {
	if rep == nil {
		return ""
	}
	var b strings.Builder
	for i := range rep.Dependencies {
		d := &rep.Dependencies[i]
		emitted := false
		for _, c := range d.Checks {
			if c.Status == StatusOK && !c.Operational {
				continue
			}
			emitted = true
			writeAnnotation(&b, d, c)
		}
		if d.Err != "" && !emitted {
			writeAnnotation(&b, d, CheckResult{Check: "operational", Operational: true})
		}
	}
	return b.String()
}

func writeAnnotation(b *strings.Builder, d *DepResult, c CheckResult) {
	level := "warning"
	switch {
	case c.Operational:
		level = "warning"
	case d.DriftKind == DriftExpected:
		level = "notice"
	default:
		level = "error"
	}
	title := fmt.Sprintf("ngm: %s %s", d.Name, c.Check)
	if d.DriftKind != DriftNone {
		title += " (" + string(d.DriftKind) + ")"
	}
	fmt.Fprintf(b, "::%s title=%s::%s\n",
		level, ghEscape(title), ghEscape(failureMessage(d, c)))
}

// ghEscape 按 GitHub 的 workflow command 规则转义。
func ghEscape(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}
