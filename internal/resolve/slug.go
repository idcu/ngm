package resolve

import (
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// ParseSlug 解析 ngm.json / ngm.lock 中的依赖标识（name 字段）。
//
// 与 Normalize 的区别：
//   - Normalize 接受全部 Git 地址形式（含 `https://` / `git@` / `git://`）
//   - ParseSlug 是 name 字段的解析入口：拒绝完整 URL，要求 host:path 形态
//
// 宽容策略：`github.com:org/repo` 也会被接受（归一化为同一仓库），
// 因为它是 Normalize 的等价输入且不产生歧义；写回 lock / ngm.json 时统一用
// Canonical.Slug()，由此保证"声明可宽容、锁定必规范"。
//
// 失败返回 *errs.NgmError（Code = CodeConfigInvalid）。
func ParseSlug(s string) (Canonical, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return Canonical{}, errs.New(
			errs.CodeConfigInvalid,
			"dependency name is empty",
			"set `name` to the canonical slug form, e.g. `github:org/repo`",
		)
	}

	// 显式拒绝完整 URL：name 字段不是 URL 字段。
	if strings.Contains(raw, "://") {
		return Canonical{}, errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("dependency name %q must be a slug, not a URL", raw),
			"use `github:org/repo` form; full URLs are only accepted by `ngm add`",
		)
	}

	// name 字段不接受 ref / 子路径后缀（它们由独立字段 ref / path 承载）。
	if strings.ContainsAny(raw, "@") {
		return Canonical{}, errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("dependency name %q must not contain `@`", raw),
			"put the ref in the `ref` field and the ref type in `refType`",
		)
	}
	if strings.Contains(raw, "#") {
		return Canonical{}, errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("dependency name %q must not contain `#`", raw),
			"put the monorepo sub-path in the `path` field",
		)
	}

	c, err := Normalize(raw)
	if err != nil {
		return Canonical{}, err
	}
	// name 字段必须形如 `<host>:<path>`（含冒号）；防止把裸 canonical 写进 name。
	if !strings.Contains(raw, ":") {
		return Canonical{}, errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("dependency name %q must use the `<host>:<org>/<repo>` form", raw),
			fmt.Sprintf("write it as %q", c.Slug()),
		)
	}
	return c, nil
}
