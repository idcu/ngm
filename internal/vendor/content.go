package vendor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
)

// ContentStore 是 vendor 层 2：内容寻址的不可变内容树存储。
//
// **两种布局并存**（[ADR-019](../docs/adr/adr-019-content-addressed-blobs.md)）：
//
//	v2（新写路径）：blobs/<aa>/<sha256> + trees/<digest>/{manifest.json,meta.json}
//	v1（遗留只读）：sha256/<digest>/{tree/,meta.json}
//
// 读路径两种都认——否则升级后旧项目会**立刻**验不过（本项目最忌的失败形态）；
// 写路径只写 v2，同一 digest 以 v2 写成功后旧的整棵树会被删掉（**不做就地迁移**）。
//
// 布局差异被**收在两个方法里**：`Entries`（列出条目）与 `ReadFile`（取字节）。
// 判定（verify）与落地（LinkTree）只吃条目，因此换布局不触碰安全关键路径。
//
// 不可变性：同一 digest 只存一份，跨项目共享。写入是原子的——先解包到临时目录，
// 再把内容逐文件发布进 blob 池、最后 rename 清单目录就位；因此中断不会留下
// "半个内容树"（**清单不存在 = 这份 digest 不存在**）。
//
// 与 mirror 的关系：mirror 是**对象库**（Git 裸仓），content 是**解包后的内容**。
// archiveDigest 从 mirror 生成（ADR-008）；content 只是 digest 的落地副本，
// 任何时刻可"重建清单 → 重算 digest"来校验它是否被篡改（verify 的本地重放）。
type ContentStore struct {
	root string
}

// NewContentStore 创建 content store 管理器。root 通常是 `~/.ngm/content`。
func NewContentStore(root string) *ContentStore {
	return &ContentStore{root: root}
}

// Root 返回 content store 根目录。
func (s *ContentStore) Root() string { return s.root }

// Meta 是 meta.json 的内容，记录内容树的来源与规范版本。
//
// 设计：字段刻意最少——能回答"这份内容是谁、哪个 commit、按哪个规范的 digest"即可。
// 时间戳不记录（保持内容树字节可复现；需要时间信息的场景由 lock 的 resolvedAt 承担）。
type Meta struct {
	// SchemaVersion 是 meta.json 自身的 schema 版本。
	SchemaVersion int `json:"schemaVersion"`
	// Repo 是依赖标识（slug 形式，如 github:org/repo）。
	Repo string `json:"repo"`
	// Commit 是内容树对应的 commit（完整 40 位 hash）。
	Commit string `json:"commit"`
	// Digest 是 `sha256:<hex>` 形式的 archiveDigest。
	Digest string `json:"digest"`
	// ManifestVersion 是生成该 digest 的清单规范版本（如 ngm-archive-digest/v1）。
	// 它让"规则变更后旧内容树不可复用"这件事可被检测。
	ManifestVersion string `json:"manifestVersion"`
	// SubPath 是 monorepo 子路径（空表示整仓）。
	SubPath string `json:"subPath,omitempty"`
}

// MetaSchemaVersion 是 meta.json 的当前 schema 版本。
const MetaSchemaVersion = 1

// digestHex 从 `sha256:<hex>` 中取出 `<hex>`；若输入已是裸 hex 则原样返回。
//
// 注意：参数名用 digestID 而非 digest——后者会遮蔽包名 digest。
func digestHex(digestID string) string {
	return strings.TrimPrefix(digestID, digest.Algorithm+":")
}

// PathForDigest 返回某个 digest 的内容树根目录（含 `tree/` 与 `meta.json`）。
func (s *ContentStore) PathForDigest(digestID string) string {
	return filepath.Join(s.root, digest.Algorithm, digestHex(digestID))
}

// TreePath 返回某个 digest 的 **v1** 内容树目录（解包结果所在处）。
//
// v2 布局下没有这样一棵已物化的树（内容在 blob 池里）——因此本方法只用于
// v1 兼容路径与"symlink 模式能否整目录链接"的判断，**不要**用它去读内容：
// 那件事请用 `Entries` / `ReadFile`。
func (s *ContentStore) TreePath(digestID string) string {
	return filepath.Join(s.PathForDigest(digestID), "tree")
}

// contentDir 返回该 digest **当前布局**下的目录（v2 的清单目录 / v1 的树目录）。
//
// 仅用于返回给调用方做展示与诊断；读内容请走 `Entries` / `ReadFile`。
func (s *ContentStore) contentDir(digestID string) string {
	if s.hasManifest(digestID) {
		return s.V2Dir(digestID)
	}
	return s.PathForDigest(digestID)
}

// MetaPath 返回 meta.json 路径。
func (s *ContentStore) MetaPath(digestID string) string {
	return filepath.Join(s.PathForDigest(digestID), "meta.json")
}

// Has 报告某个 digest 的内容是否已就绪。
//
// v2：清单存在且 meta.json 可解析；v1：meta.json 可解析且 tree/ 目录存在。
// 不做内容校验（那属于 verify 的职责，见 --deep）。
func (s *ContentStore) Has(digestID string) bool {
	if digestID == "" {
		return false
	}
	// v2 优先（清单 + meta），其次回落到 v1（meta + 已物化的树）——
	// 读路径必须同时认两种布局，否则升级后旧项目会**立刻**验不过。
	if s.hasManifest(digestID) {
		_, err := s.readMetaV2(digestID)
		return err == nil
	}
	if _, err := s.readMetaV1(digestID); err != nil {
		return false
	}
	st, err := os.Stat(s.TreePath(digestID))
	return err == nil && st.IsDir()
}

// ReadMeta 读取并校验 meta.json，**v2 在前、回落 v1**。
//
// 两处的 meta.json 内容结构完全相同，只是位置不同（v1 在 `sha256/<digest>/`、
// v2 在 `trees/<digest>/`），因此调用方不必知道布局——`verify` 的层 2 检查
// 正是靠这一点在换布局后一个字都不用改。
//
// 为什么先看**清单**存在性再决定读哪一个，而不是"先试 v2、失败再试 v1"：
// 后者会把"v2 的 meta 损坏"误判成"这份 digest 不存在"，从而回落到一棵
// 可能已被收敛删掉的 v1 树，报出误导性的错误。
func (s *ContentStore) ReadMeta(digestID string) (*Meta, error) {
	if s.hasManifest(digestID) {
		return s.readMetaV2(digestID)
	}
	return s.readMetaV1(digestID)
}

// readMetaV1 读取 v1 布局（`sha256/<digest>/meta.json`）的 meta.json。
func (s *ContentStore) readMetaV1(digestID string) (*Meta, error) {
	data, err := os.ReadFile(s.MetaPath(digestID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errs.Wrap(errs.CodeConfigInvalid,
				fmt.Sprintf("content store has no entry for %s", digestID),
				"run `ngm install` to populate the content store", err)
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read content meta", "", err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			fmt.Sprintf("content meta for %s is corrupt", digestID),
			"delete the entry and re-run `ngm install`", err)
	}
	return &m, nil
}

// PutResult 描述一次 Put 的结果。
type PutResult struct {
	// Path 是内容树根目录。
	Path string
	// AlreadyPresent 为 true 表示该 digest 已存在，本次未做任何写入。
	AlreadyPresent bool
}

// Put 把 mirror 中 commit 的内容**发布进 v2 布局**（blob 池 + 树清单）。
//
// 前置条件：meta.Digest 必须与 mirror 中该 commit 实际算出的 digest 一致——
// 本方法会**重新计算并校验**，防止"把 A 的内容写进 B 的槽位"这类错误
// （那会让 verify 永远通过，是最危险的失效模式）。
//
// 幂等：目标 digest 已存在时立即返回（content store 是不可变的）。
//
// 原子性：**先逐文件落 blob，再写清单、最后整体 rename 就位**。因此任何中断都
// 不会留下"半个可被引用的内容树"——清单不存在就等于这份 digest 不存在。
//
// 为什么不再"解包出一棵树"（v0.8 B 阶段去掉的一步）：[ADR-019](../docs/adr/adr-019-content-addressed-blobs.md)
// 的形态本来就是"blob 池 + 清单"，中间那棵树只是为了再走一遍把它拆开。直接从
// Git 的条目构建有两个实际好处，且都不是理论的：
//
//  1. **mode 以 Git 为准**，不再从解包后的文件权限反推。否则在 Windows 上
//     可执行位不可表示，同一 commit 会算出不同的清单（跨平台不一致）。
//  2. **store 不再需要创建 symlink**——symlink 的目标字符串直接进清单。
//     于是"仓库里有 symlink"不再让 Windows 用户需要开发者模式才能填充 store。
func (s *ContentStore) Put(ctx context.Context, opts git.Options, mirrorRepoPath string, meta Meta) (PutResult, error) {
	if meta.Commit == "" {
		return PutResult{}, errs.New(errs.CodeConfigInvalid, "content meta is missing commit", "")
	}
	if meta.Repo == "" {
		return PutResult{}, errs.New(errs.CodeConfigInvalid, "content meta is missing repo", "")
	}
	if meta.Digest == "" {
		return PutResult{}, errs.New(errs.CodeConfigInvalid, "content meta is missing digest", "")
	}
	if meta.SchemaVersion == 0 {
		meta.SchemaVersion = MetaSchemaVersion
	}
	if meta.ManifestVersion == "" {
		meta.ManifestVersion = digest.ManifestVersion
	}

	if s.Has(meta.Digest) {
		return PutResult{Path: s.contentDir(meta.Digest), AlreadyPresent: true}, nil
	}

	// 内容与 digest 的一致性校验（见方法文档）
	actual, err := git.BuildArchiveDigest(ctx, opts, mirrorRepoPath, meta.Commit)
	if err != nil {
		return PutResult{}, err
	}
	if actual != meta.Digest {
		return PutResult{}, errs.New(
			errs.CodeDigestMismatch,
			fmt.Sprintf("digest mismatch while populating content store: meta says %s, commit %s hashes to %s",
				meta.Digest, git.ShortSHA(meta.Commit), actual),
			"the lock file and the mirror disagree; run `ngm verify` to classify the drift")
	}

	// Git 权威的内容树：路径 + mode + blob sha。
	tree, err := git.ListTree(ctx, opts, mirrorRepoPath, meta.Commit)
	if err != nil {
		return PutResult{}, err
	}

	// 分类。与解包时代的口径完全一致（gitlink 与未知 mode 一律拒绝——
	// 它们无法被忠实地重放，放行就等于让 verify 对着一个假内容树通过）。
	var shas []string
	kept := make([]git.TreeEntry, 0, len(tree))
	for _, e := range tree {
		switch e.Mode {
		case digest.ModeTree:
			continue
		case digest.ModeRegular, digest.ModeExecutable, digest.ModeSymlink:
			shas = append(shas, e.SHA)
			kept = append(kept, e)
		case digest.ModeGitlink:
			return PutResult{}, errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("cannot unpack gitlink at %q: submodules are not supported in v0.1", e.Path),
				"vendor the submodule into this repository")
		default:
			return PutResult{}, errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("cannot unpack unsupported mode %q at %q", e.Mode, e.Path), "")
		}
	}

	contents, err := git.CatFileBatch(ctx, opts, mirrorRepoPath, shas)
	if err != nil {
		return PutResult{}, err
	}

	mf := Manifest{
		SchemaVersion:   layoutSchemaVersion,
		ManifestVersion: digest.ManifestVersion,
		Digest:          meta.Digest,
		Entries:         make([]ManifestEntry, 0, len(kept)),
	}
	for i, e := range kept {
		path := filepath.ToSlash(e.Path)
		if e.Mode == digest.ModeSymlink {
			// symlink 的"内容"就是目标字符串。它**不落 blob**：为几个字节多一个文件
			// 没有意义，而它也不会被 hardlink（落地时原样重建 symlink）。
			target := string(contents[i])
			mf.Entries = append(mf.Entries, ManifestEntry{
				Path: path, Mode: digest.ModeSymlink, Size: int64(len(target)), Target: target,
			})
			continue
		}
		sha, berr := s.writeBlob(contents[i], e.Mode)
		if berr != nil {
			return PutResult{}, berr
		}
		mf.Entries = append(mf.Entries, ManifestEntry{
			Path: path, Mode: e.Mode, SHA: sha, Size: int64(len(contents[i])),
		})
	}
	// 清单按 path 升序：它必须是确定的（可 diff、可快照），否则又会出现
	// "两次构建字节不同"这类假失败（v0.5 的 metafile 事件就是这么来的）。
	sort.Slice(mf.Entries, func(i, j int) bool { return mf.Entries[i].Path < mf.Entries[j].Path })

	if err := s.publishManifest(mf, meta); err != nil {
		return PutResult{}, err
	}
	return PutResult{Path: s.V2Dir(meta.Digest)}, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "marshal content meta", "", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "write content meta", "", err)
	}
	return nil
}
