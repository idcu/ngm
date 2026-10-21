package resolve

import (
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
)

// Target 是 CLI 里的一个“依赖目标”：仓库 + 可选子路径。
//
// 它与 ngm.json 里的 `name` 不同：`name` 不允许 `#`，而 CLI 必须能区分
// 同一仓库的不同模块。`#path=` 是推荐写法，`#<sub>` 保留给已有文档/脚本。
type Target struct {
	Repo    Canonical
	SubPath string
	HasPath bool
}

// ParseTarget 解析 `slug`、`slug#path=<sub>` 或 `slug#<sub>`。
func ParseTarget(arg string) (Target, error) {
	raw := strings.TrimSpace(arg)
	if raw == "" {
		return Target{}, errs.New(
			errs.CodeConfigInvalid,
			"dependency target is empty",
			"pass a slug like `github:org/repo`, optionally with `#path=packages/x`")
	}

	var subPath string
	hasPath := false
	if i := strings.Index(raw, "#"); i >= 0 {
		frag := raw[i+1:]
		raw = raw[:i]
		hasPath = true
		if strings.HasPrefix(frag, "path=") {
			subPath = strings.TrimPrefix(frag, "path=")
		} else {
			subPath = frag
		}
		if strings.TrimSpace(subPath) == "" {
			return Target{}, errs.New(
				errs.CodeConfigInvalid,
				fmt.Sprintf("dependency target %q has an empty sub-path", arg),
				"write `#path=packages/x` with a non-empty, repository-relative path")
		}
		if err := ValidateSubPath(subPath); err != nil {
			return Target{}, err
		}
	}

	repo, err := ParseSlug(raw)
	if err != nil {
		return Target{}, err
	}
	return Target{Repo: repo, SubPath: subPath, HasPath: hasPath}, nil
}

// Key 返回 `name#subPath`（无子路径时就是 name），与 lock/图节点同口径。
func (t Target) Key() string {
	return nodeKey(t.Repo.Slug(), t.SubPath)
}

// String 返回 CLI 可再次解析的写法。
func (t Target) String() string {
	if t.HasPath {
		return t.Repo.Slug() + "#path=" + t.SubPath
	}
	return t.Repo.Slug()
}
