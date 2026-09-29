package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/mappings"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/vendor"
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
	// Store 是层 2（内容寻址存储）管理器。
	Store *vendor.ContentStore
	// GitOpts 是所有 git 子进程共享的选项（含 Secrets 脱敏集合）。
	GitOpts git.Options
	// Protocol 是访问远端时使用的协议。
	Protocol resolve.Protocol
	// Secrets 是需要在输出中脱敏的字面量。
	Secrets []string
}

// newProjectEnv 构造运行环境。
//
// 注意：本函数**不创建任何目录**——只有真正要写入时才 EnsureDirs，
// 避免只读命令（如将来的 `ngm verify --offline`）意外改动用户态目录。
func newProjectEnv(dirFlag string) (*projectEnv, error) {
	projectDir, err := filepath.Abs(dirFlag)
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "resolve --dir", "", err)
	}

	layout, err := vendor.DefaultLayout()
	if err != nil {
		return nil, err
	}

	secrets := git.SecretsFromEnvVars(git.MergeTokenEnvVars(nil))
	gitOpts := git.Options{Secrets: secrets}

	// 协议：全局配置 git.defaultProtocol 优先，缺省 https
	proto := resolve.ProtocolHTTPS
	if r, cerr := config.Load(projectDir, homeDirOrEmpty()); cerr == nil && r.GlobalEffective.Git != nil {
		proto = resolve.ParseProtocol(r.GlobalEffective.Git.DefaultProtocol, resolve.ProtocolHTTPS)
	}

	return &projectEnv{
		ProjectDir: projectDir,
		Layout:     layout,
		Mirror:     vendor.NewMirror(layout.MirrorRoot(), gitOpts),
		Store:      vendor.NewContentStore(layout.ContentRoot()),
		GitOpts:    gitOpts,
		Protocol:   proto,
		Secrets:    secrets,
	}, nil
}

// EnsureMirror 是 resolve.GraphOptions.EnsureMirror 的实现。
//
// mirror 已就绪时完全不触网；缺失时才 clone（那需要网络或可访问的本地路径）。
func (e *projectEnv) EnsureMirror(ctx context.Context, repo resolve.Canonical) (string, error) {
	res, err := e.Mirror.Ensure(ctx, repo, "", e.Protocol)
	if err != nil {
		return "", err
	}
	return res.Path, nil
}

// GraphOptions 返回依赖图解析选项。
func (e *projectEnv) GraphOptions() resolve.GraphOptions {
	return resolve.GraphOptions{
		EnsureMirror: e.EnsureMirror,
		Protocol:     e.Protocol,
		Secrets:      e.Secrets,
		Concurrency:  resolve.DefaultConcurrency,
	}
}

// ManifestPath 返回 ngm.json 路径。
func (e *projectEnv) ManifestPath() string { return filepath.Join(e.ProjectDir, "ngm.json") }

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
}

// digestNodeFromResolved 把图节点转为落地输入。
func digestNodeFromResolved(n *resolve.Node, digest string) digestNode {
	return digestNode{Name: n.Name, Repo: n.Repo, SubPath: n.SubPath, Digest: digest}
}

// digestNodeFromLock 把 lock 条目转为落地输入（尊重 lock 路径使用）。
func digestNodeFromLock(d *lock.Dependency, repo resolve.Canonical) digestNode {
	return digestNode{Name: d.Name, Repo: repo, SubPath: d.SubPath, Digest: d.ArchiveDigest}
}

// ContentReader 返回从 content store 读取某依赖根下文件的 reader。
//
// monorepo 时根即子路径——与 vendor 落地的源目录保持同一口径，
// 因此 mappings 里的入口路径一定真实存在于 vendor 中。
func (e *projectEnv) ContentReader(dn digestNode) mappings.FileReader {
	root := e.Store.TreePath(dn.Digest)
	if sub := strings.Trim(dn.SubPath, "/"); sub != "" {
		root = filepath.Join(root, filepath.FromSlash(sub))
	}
	return func(rel string) ([]byte, bool, error) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			return nil, false, nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil, false, rerr
		}
		return data, true, nil
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

		res, err := lt.Materialize(dn.Digest, dn.Repo.MirrorRelPath(), dn.SubPath)
		if err != nil {
			return out, err
		}
		out.Links = append(out.Links, res)

		inputs = append(inputs, mappings.GenerateInput{
			From:          dn.Name,
			VendorRelRoot: vendorRootRepr,
			VendorRelPath: relPath,
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
