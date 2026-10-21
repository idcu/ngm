package resolve

import (
	"context"
	"fmt"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
)

// InferRefTypeFromRemote 用一次 ls-remote 的证据推断 ref 是 tag 还是 branch。
//
// 规则：只有远端**恰好有** `refs/tags/<ref>` 或 `refs/heads/<ref>` 之一时才作答；
// 两者都存在、都不存在、网络失败都返回错误，绝不猜。它是 `ngm add --infer-ref-type`
// 的唯一实现处；schema 的窄默认（完整 40 位 commit）不经过这里。
func InferRefTypeFromRemote(ctx context.Context, repo Canonical, ref string, opts ResolveOptions) (RefType, error) {
	if ref == "" {
		return "", errs.New(errs.CodeConfigInvalid, "ref is empty",
			"set `ref` to a tag, branch, or commit hash")
	}
	url, _ := pickLSRemoteURL(repo, opts)
	if err := git.CheckNetAccess(opts.Policy, url); err != nil {
		return "", err
	}

	gitOpts := git.Options{Secrets: opts.Secrets, Policy: opts.Policy}
	refs, err := git.LSRemote(ctx, gitOpts, url, "refs/tags/"+ref, "refs/heads/"+ref)
	if err != nil {
		return "", err
	}

	var hasTag, hasBranch bool
	for _, r := range refs {
		if r.Peeled {
			continue // annotated tag 的解引用行不是第二种 ref
		}
		switch r.Name {
		case "refs/tags/" + ref:
			hasTag = true
		case "refs/heads/" + ref:
			hasBranch = true
		}
	}

	switch {
	case hasTag && hasBranch:
		return "", errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("ref %q exists as both a tag and a branch", ref),
			"pass --ref-type tag or --ref-type branch explicitly")
	case hasTag:
		return RefTypeTag, nil
	case hasBranch:
		return RefTypeBranch, nil
	default:
		return "", errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("ref %q is not a tag or branch in %s", ref, repo.Slug()),
			"check the ref name, or pass a full 40-char commit hash with --ref-type commit")
	}
}
