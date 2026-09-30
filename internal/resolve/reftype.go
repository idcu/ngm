package resolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
)

// ResolveOptions 控制 refType → commit 的解析。
type ResolveOptions struct {
	// MirrorDir 非空时优先在本地 mirror 中解析（离线可用，见 M1.3）。
	// 期望布局：<MirrorDir>/<host>/<org>/<repo>.git
	MirrorDir string
	// GitURL 覆盖默认的远端 URL（测试注入本地 fixture 路径用）。
	// 为空时由 repo.CloneURL(Protocol) 生成。
	GitURL string
	// Protocol 是访问远端时使用的协议；空值回退到 ProtocolHTTPS。
	// 调用方通常从全局配置 `git.defaultProtocol` 读取。
	Protocol Protocol
	// Secrets 是需要在错误输出中脱敏的字面量（通常来自 tokenEnvVars 的值）。
	Secrets []string
	// Policy 是权限判定器（可为 nil）。
	//
	// 传进来是为了让本包发出的 git 子进程同样受 `run:git` 约束：
	// ngm 里不应存在"绕过门禁的 git 调用路径"，否则那条路径就是审计的盲区。
	Policy git.Permissions
}

// ResolveRef 把 (repo, ref, refType) 解析为确定的 commit hash（40 位小写十六进制）。
//
// 解析规则（architecture/dependency-resolution.md §2）：
//
//	tag    → 查 refs/tags/<ref>；annotated tag 必须解引用（`^{}`）得到 commit
//	branch → 查 refs/heads/<ref>
//	commit → 校验 hash 形态；M1.3 起在 mirror 中验证对象存在
//
// 优先顺序：本地 mirror（若存在）→ 远端。
//
// 错误分类：
//   - ref 在远端不存在 → CodeConfigInvalid（退出码 3）：声明与实际不符
//   - 网络 / 认证失败   → CodeGitFetch（退出码 4）
//   - git 未安装        → CodeGitFetch（退出码 4）
func ResolveRef(ctx context.Context, repo Canonical, ref string, refType RefType, opts ResolveOptions) (string, error) {
	if !refType.IsValid() {
		return "", errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("invalid refType %q", refType),
			fmt.Sprintf("must be one of %v", ValidRefTypes()))
	}
	if strings.TrimSpace(ref) == "" {
		return "", errs.New(
			errs.CodeConfigInvalid,
			"ref is empty",
			"set `ref` to a tag, branch, or commit hash")
	}

	switch refType {
	case RefTypeCommit:
		return normalizeCommitRef(ref)
	case RefTypeTag, RefTypeBranch:
		return resolveNamedRef(ctx, repo, ref, refType, opts)
	default:
		// 理论上不可达（IsValid 已过滤）
		return "", errs.New(errs.CodeConfigInvalid, "unhandled refType "+refType.String(), "")
	}
}

// ResolveRefTypes 批量解析同仓库下的多个 ref（便于 M1.3 复用一次 ls-remote 结果）。
//
// 返回顺序与输入一致；任一失败即整体失败。
func ResolveRefTypes(ctx context.Context, repo Canonical, specs []RefSpec, opts ResolveOptions) ([]string, error) {
	out := make([]string, len(specs))
	for i, s := range specs {
		c, err := ResolveRef(ctx, repo, s.Ref, s.Type, opts)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

// RefSpec 是 (ref, refType) 组合。
type RefSpec struct {
	Ref  string
	Type RefType
}

// normalizeCommitRef 校验 commit hash 形态并规范化为小写。
//
// 接受 7–40 位 hex（短 hash 在里程碑 M1.3 建立 mirror 后可被 rev-parse 补齐）。
// M1.2 不做对象存在性验证——那需要本地 mirror（见 ResolveRef 文档）。
func normalizeCommitRef(ref string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(ref))
	if len(s) < 7 || len(s) > 40 {
		return "", errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("commit ref %q has invalid length", ref),
			"use a full 40-character or abbreviated 7+ character hex commit hash")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return "", errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("commit ref %q is not hexadecimal", ref),
			"use a hex commit hash, or set refType to `tag`/`branch`")
	}
	return s, nil
}

// resolveNamedRef 处理 tag / branch。
func resolveNamedRef(ctx context.Context, repo Canonical, ref string, refType RefType, opts ResolveOptions) (string, error) {
	lsURL, isLocalMirror := pickLSRemoteURL(repo, opts)

	// net 门禁：ls-remote 是网络访问。放在这里而不是调用方，是因为
	// 只有本函数知道最终用哪个地址（本地 mirror 路径不算网络访问，
	// CheckNetAccess 会识别出来，不会把离线操作拦下）。
	if err := git.CheckNetAccess(opts.Policy, lsURL); err != nil {
		return "", err
	}

	refs, err := git.LSRemote(ctx, git.Options{Secrets: opts.Secrets, Policy: opts.Policy}, lsURL)
	if err != nil {
		return "", decorateFetchError(err, repo, lsURL, isLocalMirror)
	}
	idx := git.NewRefIndex(refs)

	fullName := "refs/tags/" + ref
	if refType == RefTypeBranch {
		fullName = "refs/heads/" + ref
	}

	// annotated tag：优先取解引用后的 commit
	if refType == RefTypeTag {
		if sha, ok := idx.Peeled(fullName); ok {
			return sha, nil
		}
	}
	if r, ok := idx.Lookup(fullName); ok {
		return r.SHA, nil
	}

	return "", refNotFoundError(repo, lsURL, ref, refType, isLocalMirror, refs)
}

// pickLSRemoteURL 决定用哪个地址做 ls-remote，返回 (url, 是否本地路径)。
//
// 优先顺序：
//  1. GitURL（调用方直接指定的仓库路径，通常是已就绪的 mirror）
//  2. MirrorDir 下按布局推导出的镜像
//  3. 远端 URL（CloneURL）
//
// "是否本地"用于错误消息的措辞（local mirror / remote），判定依据是该路径
// 在文件系统上存在——而不是字符串形态。
func pickLSRemoteURL(repo Canonical, opts ResolveOptions) (string, bool) {
	if opts.GitURL != "" {
		if _, err := os.Stat(opts.GitURL); err == nil {
			return opts.GitURL, true
		}
		return opts.GitURL, false
	}
	if opts.MirrorDir != "" {
		p := filepath.Join(opts.MirrorDir, filepath.FromSlash(repo.MirrorRelPath()+".git"))
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, true
		}
	}
	proto := opts.Protocol
	if !proto.IsValid() {
		proto = ProtocolHTTPS
	}
	return repo.CloneURL(proto), false
}

// decorateFetchError 给网络类失败补充上下文（镜像/远端），并把 hint 指向凭证配置。
func decorateFetchError(err error, repo Canonical, lsURL string, isLocalMirror bool) error {
	var ne *errs.NgmError
	if !errors.As(err, &ne) {
		return err
	}
	src := "remote"
	if isLocalMirror {
		src = "local mirror"
	}
	return errs.Wrap(ne.Code,
		fmt.Sprintf("%s: resolve ref via %s (%s)", repo.Slug(), src, lsURL),
		ne.Hint, err)
}

// refNotFoundError 构造 ref 不存在的错误，并附带"可用 ref 样例"以帮助纠错。
func refNotFoundError(repo Canonical, lsURL, ref string, refType RefType, isLocalMirror bool, refs []git.RemoteRef) error {
	source := "remote"
	if isLocalMirror {
		source = "local mirror"
	}
	hint := fmt.Sprintf("check the %s name, or run `ngm update` after fixing ngm.json", refType)
	if samples := sampleRefs(refs, refType); len(samples) > 0 {
		hint += fmt.Sprintf("; available %s refs include: %s", refType, strings.Join(samples, ", "))
	}
	return errs.New(
		errs.CodeConfigInvalid,
		fmt.Sprintf("%s: %s %q not found in %s", repo.Slug(), refType, ref, source),
		hint,
	)
}

// sampleRefs 从 ls-remote 结果里取若干条同类型 ref 名（去掉前缀），用于错误提示。
func sampleRefs(refs []git.RemoteRef, refType RefType) []string {
	prefix := "refs/tags/"
	if refType == RefTypeBranch {
		prefix = "refs/heads/"
	}
	var out []string
	for _, r := range refs {
		if r.Peeled {
			continue
		}
		if !strings.HasPrefix(r.Name, prefix) {
			continue
		}
		out = append(out, strings.TrimPrefix(r.Name, prefix))
		if len(out) >= 5 {
			break
		}
	}
	return out
}
