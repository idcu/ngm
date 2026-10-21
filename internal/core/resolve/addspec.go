package resolve

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
)

// AddSpec 是 `ngm add` 的命令行参数解析结果。
//
// 语法（architecture/dependency-resolution.md §1 的 monorepo 子路径示例）：
//
//	<git-url>[#path=<sub/path>][@<ref>]
//
// 例：
//
//	github:org/repo@v1.2.3
//	github:org/monorepo#path=packages/utils@v1.0.0
//	git@github.com:org/repo.git@main
//	https://github.com/org/repo.git#path=pkg/core@abc1234
type AddSpec struct {
	// Repo 是归一化后的仓库地址。
	Repo Canonical
	// Path 是 monorepo 子路径（可空）。
	Path string
	// Ref 是 ref 名（可空——为空时由 --ref-type 之外的交互路径补全）。
	Ref string
}

// ParseAddSpec 解析 `ngm add` 的位置参数。
//
// 解析顺序很关键——`@ref` 必须**先于** `#path=` 剥离，因为 `#path=` 片段可能
// 位于 `@ref` 之前（`github:org/mono#path=pkg/core@v1.0.0`）：
//
//  1. `@<ref>` 后缀——仅当最后一个 `@` 之后不含 `/` 与 `:` 时才认定为 ref，
//     否则视为 URL 的 userinfo（如 `git@host:org/repo`）
//  2. `#path=<sub>` 片段
//  3. 剩余部分交给 Normalize 归一化
func ParseAddSpec(input string) (AddSpec, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return AddSpec{}, errs.New(
			errs.CodeConfigInvalid,
			"empty dependency spec",
			"usage: ngm add <git-url>[@<ref>] --ref-type <tag|branch|commit>")
	}

	var spec AddSpec

	// 1) 剥离 `@ref`（仅当尾部片段像 ref 时）
	if at := strings.LastIndex(raw, "@"); at >= 0 {
		tail := raw[at+1:]
		if tail != "" && !strings.ContainsAny(tail, "/:") {
			spec.Ref = tail
			raw = raw[:at]
		}
	}

	// 2) 剥离 `#path=...`
	if hash := strings.Index(raw, "#"); hash >= 0 {
		frag := raw[hash+1:]
		raw = raw[:hash]
		// 允许 `#path=` 与裸 `#packages/utils` 两种写法
		switch {
		case strings.HasPrefix(frag, "path="):
			spec.Path = strings.TrimPrefix(frag, "path=")
		case frag != "":
			spec.Path = frag
		default:
			return AddSpec{}, errs.New(
				errs.CodeConfigInvalid,
				fmt.Sprintf("empty fragment in %q", input),
				"use `#path=<sub/path>` to select a monorepo sub-path")
		}
		spec.Path = strings.Trim(spec.Path, "/")
		if spec.Path != "" {
			if err := validateDepSubPath(spec.Path); err != nil {
				return AddSpec{}, err
			}
		}
	}

	// 3) 归一化 URL
	repo, err := Normalize(raw)
	if err != nil {
		return AddSpec{}, err
	}
	spec.Repo = repo
	return spec, nil
}

// validateDepSubPath 校验 monorepo 子路径。
//
// v0.82：规则搬到 [ValidateSubPath]（唯一定义处）。此前这里只按 `/` 切分，
// 于是 `#path=..\..\x` 能通过校验——而 Windows 上 `\` 是分隔符。
// 两份实现不一致的问题由此消失。
func validateDepSubPath(p string) error {
	return ValidateSubPath(p)
}

// 推断规则（guides/configuration.md §"refType 为什么必填"）：
//
//	匹配 `v*.*.*` 或 `*.*.*` → tag
//	匹配 7 位以上 hex       → commit
//	其他                    → branch
var (
	reSemver = regexp.MustCompile(`^v?\d+\.\d+\.\d+([-+].*)?$`)
	reHex    = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
)

// InferRefType 依据 ref 字面量推断 refType。
//
// ⚠️ 推断**可能出错**（branch 可以叫 `v1.2.3`，tag 可以叫 `main`），
// 因此 ngm 从不用推断结果静默写入 ngm.json——它只用于在用户漏写 --ref-type 时
// 给出可操作的纠错建议。
func InferRefType(ref string) (RefType, bool) {
	s := strings.TrimSpace(ref)
	if s == "" {
		return "", false
	}
	if reSemver.MatchString(s) {
		return RefTypeTag, true
	}
	if reHex.MatchString(s) {
		return RefTypeCommit, true
	}
	return RefTypeBranch, true
}
