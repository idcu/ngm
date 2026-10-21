package resolve

import (
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
)

// subPathHint 是"该往哪个方向改"的那句话。
//
// 它是**字段级**的建议，由唯一知道这个字段该怎么填的那一层给出
// （v0.48 的纪律：为 path 挨骂，却被告知去检查别的地方，是最糟的一种建议）。
const subPathHint = "use a path relative to the repository root, " +
	"e.g. `packages/core` (no leading `/` or `\\`, no `.` or `..`, no empty segments)"

// ValidateSubPath 校验 monorepo 子路径（`ngm.json` 的 `path`、
// `ngm.lock` 的 `subPath`、`ngm add` 的 `#path=` 三处同一套规则）。
//
// v0.82：这条规则此前有**两份实现**，而且不一致：
//
//	internal/config/validate.go  validateDepPath      认 `/` 与 `\` 两种分隔符
//	internal/resolve/addspec.go  validateDepSubPath   只认 `/`
//
// 于是 `ngm add <url>#path=..\..\x` 能通过校验（`..\..\x` 被当成一个普通段名），
// 而 Windows 上 `\` 是分隔符。**同一件事写在两个地方，就一定会有一处不一致**——
// 本项目记过多次，这次是它自己又发生了一次。
//
// 放在 resolve 包：config 已经 import resolve（用它解析 slug），
// 而 resolve 不能 import config——这让 resolve 成为唯一能被两边共用的位置。
//
// 规则：
//   - 空串合法（"没有子路径"）；
//   - 相对：不得有前导 `/` 或 `\`（由"空段"检查自然覆盖）；
//   - 两种分隔符都当分隔符（Windows 上 `\` 与 `/` 等价）；
//   - 不得有空段 —— 因此也拒绝 `//`、尾部 `/`、前导 `/`；
//   - 不得有 `.` 或 `..` 段。
func ValidateSubPath(p string) error {
	if p == "" {
		return nil // 没有子路径
	}
	// 归一到 `/` 再切分：这样空段（前导/尾部/重复分隔符）能被看见。
	segs := strings.Split(strings.ReplaceAll(p, `\`, "/"), "/")
	for _, s := range segs {
		if s == "" {
			return errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("path %q contains an empty segment (leading, trailing or duplicated separator)", p),
				subPathHint)
		}
		if s == "." || s == ".." {
			return errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("path %q must not contain `.` or `..`", p),
				subPathHint)
		}
	}
	return nil
}
