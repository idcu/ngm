package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
)

// CloneMirror 目标位置已存在且是有效裸仓库时返回的哨兵错误文案片段。
// 调用方应先检查 ExistsMirror，或直接调用 EnsureMirror 处理幂等。
//
// 本文件提供三个层次：
//
//	CloneMirror    --mirror 克隆一个裸仓库
//	FetchMirror    在既有裸仓库上增量 fetch（--prune，保持 mirror 语义）
//	IsBareMirror   校验一个目录是否为可用裸仓库
//
// vendor 层的 Mirror 类型（internal/vendor/mirror.go）在此之上管理布局与生命周期。

// CloneMirror 执行 `git clone --mirror <remoteURL> <localPath>`。
//
// 语义：--mirror 会创建裸仓库并把 remote.origin.fetch 设为 `+refs/*:refs/*`，
// 使后续 `git fetch --prune` 具备完整的镜像更新能力（含 tag 与分支删除）。
//
// 前置条件：localPath 的父目录会被创建；localPath 本身必须不存在。
// 若需要"存在即更新"的幂等语义，使用 vendor.Mirror.Ensure。
func CloneMirror(ctx context.Context, opts Options, remoteURL, localPath string) error {
	if strings.TrimSpace(remoteURL) == "" {
		return errs.New(errs.CodeGitFetch, "clone: empty remote URL", "check the dependency declaration")
	}
	if localPath == "" {
		return errs.New(errs.CodeGitFetch, "clone: empty local path", "pass the mirror path explicitly")
	}
	if exists(localPath) {
		return errs.New(errs.CodeGitFetch,
			"clone target already exists: "+localPath,
			"remove it or use the mirror update path")
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return errs.Wrap(errs.CodeGitFetch,
			"create mirror parent directory", "check filesystem permissions", err)
	}

	_, err := Run(ctx, opts, "clone", "--mirror", "--quiet", remoteURL, localPath)
	return err
}

// FetchMirror 在既有裸仓库上执行增量更新。
//
// 命令：`git -C <localPath> fetch --prune`
// 因该仓库由 --mirror 创建，其 refspec 覆盖全部 refs，`--prune` 会同步删除上游已删除的 ref。
func FetchMirror(ctx context.Context, opts Options, localPath string) error {
	if !IsBareMirror(localPath) {
		return errs.New(errs.CodeGitFetch,
			"not a valid bare mirror: "+localPath,
			"delete the directory and re-run to re-create the mirror")
	}
	if !IsMirror(localPath) {
		// 形状对、但**没走完**的克隆（v0.101，ADR-031）。必须拒绝：
		// 在它上面 `git fetch --prune` 会**退出 0 且什么都不抓**（实测），
		// 于是调用方会以为"更新成功"，而失败会跑到下游去伪装成"你的 ref 不存在"。
		return errs.New(errs.CodeGitFetch,
			"mirror clone never finished (no "+MirrorRefspec+" in its config): "+localPath,
			"delete the directory and re-run — ngm will clone it again")
	}
	o := opts
	o.Dir = localPath
	_, err := Run(ctx, o, "fetch", "--prune", "--quiet")
	return err
}

// 注：这里此前有一个 `FetchMirrorTags`（`fetch --prune --tags`），**全仓库零调用**
// （v0.66 评估报告 P3 量出）。它的注释说"部分 host 对 tag 更新需要显式 --tags"，
// 而那个问题在本项目的语义下**不存在**：mirror 由 `git clone --mirror` 创建，
// 其 `remote.origin.fetch` 是 `+refs/*:refs/*`（见 CloneMirror 的文档），
// `git fetch --prune` 因此覆盖**全部** refs（含 tags 的更新与删除）。
// 死代码已删除——留着它会让"tag 同步"看起来需要额外机制，而实际不需要。

// PartialFilter 是 module cache 使用的 partial clone 过滤器。
//
// 它保留 commit/tree，只按需取 blob——这正是“只证明/只落地一个模块”所需的对象集。
const PartialFilter = "blob:none"

// ClonePartial 执行 `git clone --bare --filter=blob:none <remoteURL> <localPath>`。
//
// 与 CloneMirror 的区别：它**不是 mirror**，不宣称任意 ref 可离线重放；它只服务
// 已被 lock 钉住的 (commit, subPath)。克隆完成后会校验 filter 真的生效，
// 否则删除这个刚创建的目录并返回错误——静默留一个全量克隆冒充缓存比失败更糟。
func ClonePartial(ctx context.Context, opts Options, remoteURL, localPath string) error {
	if strings.TrimSpace(remoteURL) == "" {
		return errs.New(errs.CodeGitFetch, "clone: empty remote URL", "check the dependency declaration")
	}
	if localPath == "" {
		return errs.New(errs.CodeGitFetch, "clone: empty local path",
			"pass the module cache path explicitly")
	}
	if exists(localPath) {
		return errs.New(errs.CodeGitFetch,
			"clone target already exists: "+localPath,
			"remove it or use the module cache update path")
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return errs.Wrap(errs.CodeGitFetch,
			"create module cache parent directory", "check filesystem permissions", err)
	}

	_, err := Run(ctx, opts, "clone", "--bare", "--filter="+PartialFilter, "--quiet", remoteURL, localPath)
	if err != nil {
		return err
	}
	if !IsPartialClone(localPath) {
		_ = os.RemoveAll(localPath)
		return errs.New(errs.CodeGitFetch,
			"partial clone filter was not applied by "+remoteURL,
			"the Git server does not support `--filter=blob:none`; use a full mirror instead")
	}
	return nil
}

// FetchPartial 在既有 partial clone 上按 ref 增量取对象。
//
// ref 可以是 `refs/tags/<tag>` / `refs/heads/<branch>` / 完整 commit；
// 调用方负责选择。失败由 git 自己报出（含认证/网络）。
func FetchPartial(ctx context.Context, opts Options, localPath, ref string) error {
	if !IsPartialClone(localPath) {
		return errs.New(errs.CodeGitFetch,
			"not a partial clone: "+localPath,
			"delete the directory and re-run to re-create the module cache")
	}
	o := opts
	o.Dir = localPath
	_, err := Run(ctx, o, "fetch", "--filter="+PartialFilter, "--quiet", "origin", ref)
	return err
}

// FetchPartialCommit 尽力把 lock 钉住的那个 commit 取进 partial clone。
//
// 先试精确 commit（`--depth=1`）；服务端不允许按 SHA fetch 时，再试调用方给的
// tag/branch。无论走了哪条路，最后都必须能解析出该 commit——否则明确失败，
// 不把“取到了分支尖端”当成“取到了锁定的 commit”。
func FetchPartialCommit(ctx context.Context, opts Options, localPath, commit, fallbackRef, refType string) error {
	if err := FetchPartial(ctx, opts, localPath, commit); err == nil {
		if ok, _ := CommitExists(ctx, opts, localPath, commit); ok {
			return nil
		}
	}

	ref := strings.TrimSpace(fallbackRef)
	if ref == "" || ref == commit {
		return errs.New(errs.CodeGitFetch,
			"commit "+ShortSHA(commit)+" could not be fetched into the module cache",
			"the server may not allow fetching by SHA; run without the module cache to use a full mirror")
	}
	qualified := ref
	switch refType {
	case "tag":
		qualified = "refs/tags/" + ref
	case "branch":
		qualified = "refs/heads/" + ref
	}
	if err := FetchPartial(ctx, opts, localPath, qualified); err != nil {
		return err
	}
	if ok, _ := CommitExists(ctx, opts, localPath, commit); !ok {
		return errs.New(errs.CodeGitFetch,
			"commit "+ShortSHA(commit)+" is not reachable from "+qualified,
			"the lock pins a commit the ref no longer reaches; re-run `ngm update`")
	}
	return nil
}

// IsPartialClone 报告 path 是否是一个** filter 真的生效**的 partial clone。
//
// 判据是 config 里的 `remote.origin.promisor=true` 与 `partialclonefilter`；
// 只判裸仓库形状不够——本地 file:// 之外的一些传输会忽略 `--filter`，
// 那样磁盘上留下的是一个全量克隆，而调用方会以为省到了。
func IsPartialClone(path string) bool {
	if !IsBareMirror(path) {
		return false
	}
	// 与 url/refspec 同一条读取器：git config 的形状判断只能有一处
	// （TestGitConfigIsParsedInExactlyOnePlace）。
	keys, _, ok := originConfigKeys(path)
	if !ok {
		return false
	}
	promisor := keys["promisor"]
	filter := keys["partialclonefilter"]
	return len(promisor) == 1 && strings.EqualFold(promisor[0], "true") &&
		len(filter) == 1 && filter[0] == PartialFilter
}

// IsBareMirror 校验 path 的**形状**是不是一个裸仓库。
//
// 判定：存在 `HEAD` 文件、`objects/` 与 `refs/` 目录，且 `config` 中 core.bare=true。
// 不调用 git 子进程（快速路径，供 Ensure 在热路径上使用）。
//
// **它不回答"能不能用"**（v0.101 更正措辞）：一次被中断的 `git clone --mirror`
// 留下的正是这个形状（实测：`HEAD` / `objects/` / `refs/` / `core.bare=true` 全齐），
// 而它解析不出任何 commit。要问"能不能用"用 [IsMirror]——这正是 v0.66 评估报告
// P2#10 那类"注释描述的是意图、不是现状"的形态，不能再犯一次。
func IsBareMirror(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return false
	}
	for _, f := range []string{"HEAD", "objects", "refs"} {
		if !exists(filepath.Join(path, f)) {
			return false
		}
	}
	cfg, err := os.ReadFile(filepath.Join(path, "config"))
	if err != nil {
		return false
	}
	// 宽松匹配：core.bare = true（允许空白变体）
	for _, line := range strings.Split(string(cfg), "\n") {
		l := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(line), " ", ""))
		if l == "bare=true" {
			return true
		}
	}
	return false
}

// IsMirror 报告 path 是不是一个**走完了的** mirror：形状是裸仓库（[IsBareMirror]）
// **且** config 里有镜像 refspec（[MirrorRefspec]）。
//
// ## 为什么需要第二个条件（v0.101 实测，见 ADR-031）
//
// `git clone --mirror` 是**先建好仓库、再抓对象、最后**才把镜像 refspec 写进 config。
// 因此"有一次克隆被中断"这件事在磁盘上留下的指纹不是"没有对象"，而是
// **config 里没有那条 refspec**——而那个形状的后果比"坏掉"更糟：
// 在它上面 `git fetch --prune` 退出 0 且什么都不抓，于是调用方会报
// "我复用了既有 mirror"，而真正的失败跑到下游，伪装成"你的分支上游不存在"。
//
// 判据必须是 refspec 而不是"有没有 ref"：从**空上游**成功克隆出来的 mirror
// 一个 ref 都没有，却**有** refspec（实测，两条都量过）。
//
// ## 保守方向是**不拒绝**
//
// 读不出 config 的形状（`[include]`、重复段、多值……）时返回 true——即**照旧按 mirror 用**。
// 方向与 [MirrorRemoteURLLocal] 相反（那个读不准必须交回 git），因为这里判错的代价不对称：
// 把一座废墟当 mirror 用，最多复现今天那个"误导人的下游报错"；
// 而把一个正常的 mirror 判死，会**当场弄坏一个能用的安装**。
//
// 不调用 git 子进程（与 IsBareMirror 同一类的快速路径）。
func IsMirror(mirrorPath string) bool {
	if !IsBareMirror(mirrorPath) {
		return false
	}
	spec, ok := MirrorRefspecLocal(mirrorPath)
	if !ok {
		return true
	}
	return spec == MirrorRefspec
}

// MirrorRemoteURL 读取裸仓库的 origin URL（用于诊断与取数，不含写入路径）。
//
// v0.5 起**先读 config 文件**（MirrorRemoteURLLocal），形状不认识时才回退到
// `git config --get remote.origin.url`。理由：该调用实测约 30.6ms/次，
// 而在线 verify 与 mirror 更新都是**每个依赖一次**（ADR-015）；
// 而那个值就明文写在 mirror 的 config 里。
//
// 语义**完全不变**：仍是"这个 mirror 当初用的那个地址"；读不出来时行为与从前一致
// （返回错误或空串，由调用方按既有方式降级）。回退路径保证了
// "我们不认识的 config"不会变成"另一个答案"。
func MirrorRemoteURL(ctx context.Context, opts Options, localPath string) (string, error) {
	if url, ok := MirrorRemoteURLLocal(localPath); ok {
		return url, nil
	}
	o := opts
	o.Dir = localPath
	res, err := Run(ctx, o, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
