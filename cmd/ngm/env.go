package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/mappings"
	"github.com/idcu/ngm/internal/security"
)

// projectEnv 是子命令共享的运行环境：用户态布局、mirror、content store、
// 凭证脱敏集合与传输协议。
//
// 抽成一处的原因：install / update / verify 都需要同一套"从项目目录推导出
// 一切外部资源"的逻辑；分散实现会让凭证脱敏或协议选择在不同命令间漂移。
type projectEnv struct {
	// ProjectDir 是项目根目录（含 ngm.json / ngm.lock）。
	ProjectDir string
	// Layout 是 ~/.ngm 的目录布局。
	Layout vendor.Layout
	// Mirror 是层 1（Git 裸仓库镜像）管理器。
	Mirror *vendor.Mirror
	// ModuleCache 是层 1.5（partial clone 模块缓存，ADR-034）管理器。
	ModuleCache *vendor.ModuleCache
	// Store 是层 2（内容寻址存储）管理器。
	Store *vendor.ContentStore
	// GitOpts 是所有 git 子进程共享的选项（含 Secrets 脱敏集合）。
	GitOpts git.Options
	// Protocol 是访问远端时使用的协议。
	Protocol resolve.Protocol
	// Secrets 是需要在输出中脱敏的字面量。
	Secrets []string
	// Policy 是权限判定器（`~/.ngm/config.json` 的 permissions 段）。
	//
	// 可能为 nil（配置文件不存在）：此时判定按默认档位进行，net / run 仍需授权。
	Policy *security.Policy
}

// newProjectEnv 构造运行环境。
//
// 注意：本函数**不创建任何目录**——只有真正要写入时才 EnsureDirs，
// 避免只读命令（如将来的 `ngm verify --offline`）意外改动用户态目录。
func newProjectEnv(dirFlag string) (*projectEnv, error) {
	projectDir, err := filepath.Abs(dirFlag)
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "resolve --dir",
			"pass an absolute --dir, or run ngm from the directory you mean", err)
	}

	layout, err := vendor.DefaultLayout()
	if err != nil {
		return nil, err
	}

	// 协议、权限与**凭证来源**都来自全局配置。因此 `secrets` 在这里还不该构造——
	// 见下方 "读配置之后" 那一段。
	//
	// 配置**不存在**是正常的（Load 返回 nil 错）；而配置**坏了**必须报出来（v0.81 修）。
	//
	// 此前这里写的是 `if r, cerr := config.Load(...); cerr == nil { ... }`，
	// 理由写在注释里："读不出来时策略为 nil ⇒ 判定退化为默认档位 ⇒ 更严"。
	// 那个论证只覆盖**策略**这一个消费者，而另外两个的退化**不是更严**（实测）：
	//
	//	· `git.defaultProtocol` 悄悄退回 https——用户写 `sshh` 这类笔误时协议被换掉，
	//	  而报错会指向网络（"连不上"），真因（配置根本没被采用）不可见；
	//	· `git.tokenEnvVars` 丢掉用户声明的 token 变量名 ⇒ 脱敏集合漏掉它
	//	  ⇒ token 会被原样打进日志与报错——正是下面 secrets 那段历史事故的形态。
	//
	// 所以：坏配置 = exit 3，点名文件与字段。配置是用户唯一能改的那半边，
	// 它坏了就该由用户知道，而不是由工具替他挑一份默认值。
	proto := resolve.ProtocolHTTPS
	var pol *security.Policy
	tokenEnvVars := map[string]string(nil)

	// **只读全局配置**（projectDir 传空）：这里关心的是协议与 token 来源，
	// 而项目清单的错误归它自己那条既有路径（`ReadManifest`，它的建议是
	// "跑 `ngm config validate`"——v0.83 实测：从这条路径抢着报会把它压掉）。
	//
	// 错误**原样向上传**：全局配置与项目清单的建议不同，各自在**配置层**定型。
	r, cerr := config.Load("", homeDirOrEmpty())
	if cerr != nil {
		return nil, cerr
	}
	if r.GlobalEffective.Git != nil {
		proto = resolve.ParseProtocol(r.GlobalEffective.Git.DefaultProtocol, resolve.ProtocolHTTPS)
		tokenEnvVars = r.GlobalEffective.Git.TokenEnvVars
	}
	// 权限写错必须立刻报出来：把它当成"没配"会让用户以为
	// 自己写的 deny 已经生效，而实际上整份列表都没被采用。
	p, perr := newPermissionPolicy(r.GlobalEffective.Permissions)
	if perr != nil {
		return nil, perr
	}
	pol = p
	// 读配置**之后**才构造脱敏集合：它由 `git.tokenEnvVars` 决定
	// （用户用它声明"公司这台 git 的 token 在 CORP_TOKEN 里"）。
	//
	// 早先这里写的是 `git.MergeTokenEnvVars(nil)`——配置从未被读进来，而
	// internal/git/auth.go 的注释明文承诺"配置优先"。症状是安全相关的：
	// 用户以为自己的 token 已被脱敏，实际它会被原样打进日志与报错。
	//
	// 读不出配置时退化为"只有内置表"：不比原来差，也绝不静默放宽。
	secrets := git.SecretsFromEnvVars(git.MergeTokenEnvVars(tokenEnvVars))
	gitOpts := git.Options{Secrets: secrets, Policy: pol}

	return &projectEnv{
		ProjectDir:  projectDir,
		Layout:      layout,
		Mirror:      vendor.NewMirror(layout.MirrorRoot(), gitOpts),
		ModuleCache: vendor.NewModuleCache(layout.ModuleCacheRoot(), gitOpts),
		Store:       vendor.NewContentStore(layout.ContentRoot()),
		GitOpts:     gitOpts,
		Protocol:    proto,
		Secrets:     secrets,
		Policy:      pol,
	}, nil
}

// globalConfigSource 返回权限提示里要指出的配置文件。
//
// 用真实路径而不是写死 `~/.ngm/config.json`：设置过 NGM_HOME 的人看到的是
// 另一个文件，指错地方等于让提示失效。
func globalConfigSource() string {
	home := homeDirOrEmpty()
	if home == "" {
		return "~/.ngm/config.json"
	}
	return filepath.Join(home, ".ngm", "config.json")
}

// EnsureMirror 是 resolve.GraphOptions.EnsureMirror 的实现。
//
// mirror 已就绪时完全不触网；缺失时才 clone（那需要网络或可访问的本地路径）。
//
// `net:` 的判定在 `vendor.Mirror.Ensure` 里，不在本函数：只有那一层知道实际要访问
// 的 URL（本地路径不该被当成网络访问），而本函数只看得到 slug。
func (e *projectEnv) EnsureMirror(ctx context.Context, repo resolve.Canonical) (string, error) {
	res, err := e.Mirror.Ensure(ctx, repo, "", e.Protocol)
	if err != nil {
		return "", err
	}
	return res.Path, nil
}

// RemoteRefResolver 返回"问远端要 ref"的解析器（ADR-010）。
//
// 它先读 mirror 里记录的 origin URL（**本地读配置，不触网**），再对那个 URL 做
// 一次 `ls-remote`——因此整个过程只有一次 ref 广播，**不传输对象**。
//
// 拿不到 origin URL 时退回本地 mirror 解析：那是罕见的降级模式（行为与 v0.2 一致，
// 会因此失去"比 mirror 更新"这一点），但比直接失败更有用。
func (e *projectEnv) RemoteRefResolver() func(context.Context, resolve.Canonical, string, resolve.RefType) (string, error) {
	return func(ctx context.Context, repo resolve.Canonical, ref string, rt resolve.RefType) (string, error) {
		// commit 型**不需要解析**：它自己就是答案。
		//
		// 这里此前无条件先取一次 mirror 的远端地址（`git config --get remote.origin.url`，
		// 一个 git 子进程），而 commit 分支**根本不用那个地址**——
		// 于是每个 commit 型依赖白起一个进程（v0.5 实测，见 ADR-015）。
		//
		// 内容寻址的对象不需要远端广播的 refs：`ls-remote` 列的是 ref，
		// 它回答不了"某个未被引用的 commit 是否还在上游"。
		// 对象**是否存在**，由必须读它的那一步负责（ADR-010 的按需取物：
		// digest 重放会明确报"commit 不在本地 mirror 里"），不在这里假装知道。
		if rt == resolve.RefTypeCommit {
			return resolve.ResolveRef(ctx, repo, ref, rt, resolve.ResolveOptions{})
		}

		url, err := git.MirrorRemoteURL(ctx, e.GitOpts, e.Mirror.PathFor(repo))
		if err != nil || strings.TrimSpace(url) == "" {
			// 降级分支：只读本地 mirror，不触网，因此不需要 net 权限
			return resolve.ResolveRef(ctx, repo, ref, rt, resolve.ResolveOptions{
				MirrorDir: e.Layout.MirrorRoot(),
				Protocol:  e.Protocol,
				Secrets:   e.GitOpts.Secrets,
				Policy:    e.Policy,
			})
		}
		// 这一次会做 ls-remote（网络访问）。net 门禁在 resolve 内部：
		// 那里才是最终地址确定的地方，也只有那里能覆盖其他调用方。
		return resolve.ResolveRef(ctx, repo, ref, rt, resolve.ResolveOptions{
			GitURL:   url,
			Protocol: e.Protocol,
			Secrets:  e.GitOpts.Secrets,
			Policy:   e.Policy,
		})
	}
}

// GraphOptions 返回依赖图解析选项。
func (e *projectEnv) GraphOptions() resolve.GraphOptions {
	return resolve.GraphOptions{
		EnsureMirror: e.EnsureMirror,
		Protocol:     e.Protocol,
		Secrets:      e.Secrets,
		Policy:       e.Policy,
		Concurrency:  resolve.DefaultConcurrency,
	}
}

// ManifestPath 返回 ngm.json 路径。
func (e *projectEnv) ManifestPath() string { return filepath.Join(e.ProjectDir, "ngm.json") }

// ---------- 落盘前的凭证检查（v0.66） ----------

// guardNoSecrets 在写入**可提交文件**前断言内容里不含任何已知 token 字面量。
//
// 为什么值得有（v0.66，评估报告 P3）：`git.AssertNoSecrets` 从写下那天起就带着
// "在写入 lock / ngm.json / 打印用户可见输出前调用"的注释，而全仓库**零调用**——
// 一条描述着不存在的机制的注释。ngm 的设计是"认证交给 git、自己从不触碰 token"，
// 但那是设计意图；这一步让"token 意外混进声明或 URL"这类事故在**落盘前**
// 变成硬失败，而不是靠事后审计发现。
//
// 接线范围（如实记）：install / update 的 ngm.lock 与 ngm.mappings.json。
// `add` / `init` 写的 ngm.json 目前不经过这里（内容是用户显式输入的声明，
// 且那两个命令没有构造 projectEnv）——那一天需要时从这里接。
//
// 命中时只说"检测到"，绝不回显 secret 本身（由 AssertNoSecrets 保证）。
func guardNoSecrets(data []byte, secrets []string, what string) error {
	if len(secrets) == 0 {
		return nil
	}
	if hit := git.AssertNoSecrets(string(data), secrets); hit != "" {
		return errs.New(errs.CodeConfigInvalid,
			"refusing to write "+what+": it would contain a credential literal ("+hit+")",
			"ngm never writes tokens to project files — check the dependency URL and "+
				"`git.tokenEnvVars`, and rotate the token if it leaked")
	}
	return nil
}

// writeLockChecked 是 lock.Write 的落盘前保护版。
func writeLockChecked(env *projectEnv, lf *lock.File) error {
	if data, merr := lf.Marshal(); merr == nil {
		if gerr := guardNoSecrets(data, env.Secrets, "ngm.lock"); gerr != nil {
			return gerr
		}
	}
	return lock.Write(env.LockPath(), lf)
}

// writeMappingsChecked 是 mappings.Write 的落盘前保护版。
func writeMappingsChecked(env *projectEnv, f *mappings.File) error {
	if data, merr := f.Marshal(); merr == nil {
		if gerr := guardNoSecrets(data, env.Secrets, mappings.FileName); gerr != nil {
			return gerr
		}
	}
	return mappings.Write(mappings.Find(env.ProjectDir), f)
}

// LockPath 返回 ngm.lock 路径。
func (e *projectEnv) LockPath() string { return lock.Find(e.ProjectDir) }

// ReadManifest 读取并校验项目的 ngm.json。
func (e *projectEnv) ReadManifest() (*config.ProjectFile, error) {
	return config.ReadProjectFile(e.ManifestPath())
}

// toDepSpecs 把 ngm.json 的依赖声明转换为图解析用的规格。
func toDepSpecs(deps []config.Dependency) []resolve.DepSpec {
	out := make([]resolve.DepSpec, 0, len(deps))
	for _, d := range deps {
		out = append(out, resolve.DepSpec{
			Name:    d.Name,
			Ref:     d.Ref,
			RefType: d.RefType,
			SubPath: d.Path,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// vendor 层 3（link tree）与 mappings
// ---------------------------------------------------------------------------

// VendorRoot 返回 vendor 落地的根目录。
//
//	"local"（默认）→ <project>/ngm.vendor
//	"global"       → vendor.globalPath（展开 ~）或 <ngm-home>/global-vendor
func (e *projectEnv) VendorRoot(pf *config.ProjectFile) string {
	if pf != nil && pf.Vendor != nil && pf.Vendor.Mode == "global" {
		if p := strings.TrimSpace(pf.Vendor.GlobalPath); p != "" {
			return expandUserPath(p)
		}
		return e.Layout.GlobalVendorRoot()
	}
	return filepath.Join(e.ProjectDir, vendor.VendorDirName)
}

// LinkTree 构造 link tree 管理器。
func (e *projectEnv) LinkTree(pf *config.ProjectFile) *vendor.LinkTree {
	mode := vendor.LinkAuto
	if pf != nil && pf.Vendor != nil {
		mode = vendor.ParseLinkMode(pf.Vendor.LinkMode)
	}
	return vendor.NewLinkTree(e.VendorRoot(pf), e.Store, mode)
}

// MappingsVendorRoot 返回 mappings `to` 字段使用的 vendor 根表示。
//
// local 模式用协议默认的 `./ngm.vendor`（相对项目根，可移植）；
// global 模式用实际位置（绝对路径），否则构建工具找不到代码。
func (e *projectEnv) MappingsVendorRoot(pf *config.ProjectFile) string {
	if pf != nil && pf.Vendor != nil && pf.Vendor.Mode == "global" {
		return e.VendorRoot(pf)
	}
	return mappings.VendorRelRoot
}

// expandUserPath 展开前导 `~`。
func expandUserPath(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	rest := strings.TrimPrefix(p, "~")
	rest = strings.TrimPrefix(strings.TrimPrefix(rest, "/"), "\\")
	if rest == "" {
		return home
	}
	return filepath.Join(home, rest)
}

// digestNode 是"已解析且已算好 digest"的依赖，供 vendor 落地与 mappings 生成使用。
//
// 刻意只用最少字段（而非 resolve.Node）：install 的"尊重 lock"路径不构建立图，
// 它直接从 lock 条目构造本结构。
type digestNode struct {
	Name    string
	Repo    resolve.Canonical
	SubPath string
	Digest  string
	// Module 表示 Digest 是 moduleDigest，条目路径已经相对模块根。
	Module bool
}

// digestNodeFromResolved 把图节点转为落地输入。
func digestNodeFromResolved(n *resolve.Node, archiveDigest, moduleDigest string) digestNode {
	dn := digestNode{Name: n.Name, Repo: n.Repo, SubPath: n.SubPath, Digest: archiveDigest}
	if n.SubPath != "" && moduleDigest != "" {
		dn.Digest = moduleDigest
		dn.Module = true
	}
	return dn
}

// digestNodeFromLock 把 lock 条目转为落地输入（尊重 lock 路径使用）。
func digestNodeFromLock(d *lock.Dependency, repo resolve.Canonical) digestNode {
	dn := digestNode{Name: d.Name, Repo: repo, SubPath: d.SubPath, Digest: d.ArchiveDigest}
	if d.SubPath != "" && d.ModuleDigest != "" {
		dn.Digest = d.ModuleDigest
		dn.Module = true
	}
	return dn
}

// ContentReader 返回从 content store 读取某依赖根下文件的 reader。
//
// monorepo 时根即子路径——与 vendor 落地的源目录保持同一口径，
// 因此 mappings 里的入口路径一定真实存在于 vendor 中。
//
// 读取走 `ContentStore.ReadFile`（按条目），**不拼 v1 的树目录路径**：
// v2 布局下那棵目录不存在（ADR-019 §阶段 A 的解耦就包含这一处）。
func (e *projectEnv) ContentReader(dn digestNode) mappings.FileReader {
	prefix := strings.Trim(dn.SubPath, "/")
	return func(rel string) ([]byte, bool, error) {
		p := filepath.ToSlash(rel)
		// 模块 scope 的清单路径已经相对模块根，不能再拼 subPath。
		if prefix != "" && !dn.Module {
			p = prefix + "/" + p
		}
		return e.Store.ReadFile(dn.Digest, p)
	}
}

// MaterializeResult 汇总一次 vendor 落地 + mappings 生成的结果。
type MaterializeResult struct {
	// Links 是每个依赖的落地结果（按依赖顺序）。
	Links []vendor.LinkResult
	// Mappings 是生成的 mappings（未写入磁盘）。
	Mappings *mappings.File
	// Warnings 是入口推断等非致命提示。
	Warnings []string
}

// MaterializeVendorAndMappings 把已解析的节点落地到 vendor 并生成 mappings。
//
// install / update 共用此流程，保证"vendor 内容"与"mappings 指向"永远一致：
// 两者都从同一个 content 子树派生。
func (e *projectEnv) MaterializeVendorAndMappings(pf *config.ProjectFile, items []digestNode) (MaterializeResult, error) {
	var out MaterializeResult

	lt := e.LinkTree(pf)
	vendorRootRepr := e.MappingsVendorRoot(pf)

	inputs := make([]mappings.GenerateInput, 0, len(items))
	for _, dn := range items {
		relPath := vendor.VendorPathFor(dn.Repo.MirrorRelPath(), dn.SubPath)

		var res vendor.LinkResult
		var err error
		if dn.Module {
			res, err = lt.MaterializeModule(dn.Digest, dn.Repo.MirrorRelPath(), dn.SubPath)
		} else {
			res, err = lt.Materialize(dn.Digest, dn.Repo.MirrorRelPath(), dn.SubPath)
		}
		if err != nil {
			return out, err
		}
		out.Links = append(out.Links, res)

		inputs = append(inputs, mappings.GenerateInput{
			From:          dn.Name,
			VendorRelRoot: vendorRootRepr,
			VendorRelPath: relPath,
			SubPath:       dn.SubPath,
			Read:          e.ContentReader(dn),
		})
	}

	f, warns := mappings.Generate(inputs)
	out.Mappings = f
	out.Warnings = warns
	return out, nil
}

// homeDirOrEmpty 返回用户主目录（失败时为空串，表示不加载全局配置）。
func homeDirOrEmpty() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(h)
}
