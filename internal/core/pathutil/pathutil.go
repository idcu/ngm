// Package pathutil 提供 ngm 内部**仓库相对路径**的唯一归一化口径。
//
// 它是 core 层的叶子包：不 import 任何其它 ngm 包，因此 config / resolve / git
// 都可以安全地依赖它——这正是它能成为"唯一定义"的原因。
//
// 存在理由（v0.136）：此前子路径的分隔符规则有三份实现，且不一致：
//
//	resolve.ValidateSubPath   把 `\` 无条件换成 `/`
//	config.normalizeSubPath   用 filepath.ToSlash —— **只在 Windows 转换**
//	git.normalizeModulePath   只按 `/` 切分
//
// 于是同一份 ngm.json 在 Windows 判"重复根声明"（exit 3）、在 Linux 静默通过；
// `#path=packages\core` 通过校验却在 install 期按字面量传给 git pathspec。
// 规则写在第二个地方，就一定会有一处不一致——这里把它收成一处。
package pathutil

import "strings"

// NormalizeSeparators 把路径里的 `\` 统一换成 `/`，**不 trim**。
//
// 校验器用它做"两种分隔符等价"的判断：前导/尾部/重复斜杠必须原样保留，
// 才能被 resolve.ValidateSubPath 的空段检查拒绝。
func NormalizeSeparators(p string) string {
	return strings.ReplaceAll(p, `\`, "/")
}

// NormalizeSubPath 把子路径归一到 ngm 的唯一口径：`\` → `/`、去首尾 `/`。
//
// 用在**已通过校验**的路径上（比较键、git pathspec）。空串与纯斜杠归一为 ""。
// 注意它**不做** `.`/`..`/空段校验——那是 resolve.ValidateSubPath 的事。
func NormalizeSubPath(p string) string {
	return strings.Trim(NormalizeSeparators(p), "/")
}
