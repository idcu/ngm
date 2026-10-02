package vendor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
)

// ContentStore 是 vendor 层 2：内容寻址的不可变内容树存储。
//
// 布局（architecture/vendor-layers.md §层 2）：
//
//	~/.ngm/content/sha256/<digest-hex>/
//	├── tree/        归一化内容树（文件 + symlink，真实存在于磁盘）
//	└── meta.json    repo / commit / digest 规范版本
//
// 不可变性：同一 digest 只存一份，跨项目共享。写入是原子的——先解包到临时目录，
// 再整体 rename 就位；因此中断不会留下"半个内容树"。
//
// 与 mirror 的关系：mirror 是**对象库**（Git 裸仓），content 是**解包后的树**。
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

// TreePath 返回某个 digest 的内容树目录（解包结果所在处）。
func (s *ContentStore) TreePath(digestID string) string {
	return filepath.Join(s.PathForDigest(digestID), "tree")
}

// MetaPath 返回 meta.json 路径。
func (s *ContentStore) MetaPath(digestID string) string {
	return filepath.Join(s.PathForDigest(digestID), "meta.json")
}

// Has 报告某个 digest 的内容树是否已就绪。
//
// 判定条件：meta.json 可解析 + tree/ 目录存在。不做内容校验
// （那属于 verify 的职责，见 --deep）。
func (s *ContentStore) Has(digestID string) bool {
	if digestID == "" {
		return false
	}
	if _, err := s.ReadMeta(digestID); err != nil {
		return false
	}
	st, err := os.Stat(s.TreePath(digestID))
	return err == nil && st.IsDir()
}

// ReadMeta 读取并校验 meta.json。
func (s *ContentStore) ReadMeta(digestID string) (*Meta, error) {
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

// Put 把 mirror 中 commit 的内容树解包到 store。
//
// 前置条件：meta.Digest 必须与 mirror 中该 commit 实际算出的 digest 一致——
// 本方法会**重新计算并校验**，防止"把 A 的内容写进 B 的槽位"这类错误
// （那会让 verify 永远通过，是最危险的失效模式）。
//
// 幂等：目标 digest 已存在时立即返回（content store 是不可变的）。
//
// 原子性：解包到同层临时目录再 rename 就位。
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

	finalDir := s.PathForDigest(meta.Digest)
	if s.Has(meta.Digest) {
		return PutResult{Path: finalDir, AlreadyPresent: true}, nil
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

	// 解包到临时目录
	if err := os.MkdirAll(filepath.Join(s.root, digest.Algorithm), 0o755); err != nil {
		return PutResult{}, errs.Wrap(errs.CodeConfigInvalid, "create content store root", "", err)
	}
	// 临时目录名前缀用 `unpackPrefix`（与 `Prune` 共用同一个常量）：
	// "哪些目录是残骸"这件事只能有一处定义。
	tmpDir, err := os.MkdirTemp(filepath.Join(s.root, digest.Algorithm), unpackPrefix+"*")
	if err != nil {
		return PutResult{}, errs.Wrap(errs.CodeConfigInvalid, "create temp unpack dir", "", err)
	}
	defer func() {
		// 成功路径已 rename；失败路径清理
		_ = os.RemoveAll(tmpDir)
	}()

	if err := unpackTree(ctx, opts, mirrorRepoPath, meta.Commit, filepath.Join(tmpDir, "tree")); err != nil {
		return PutResult{}, err
	}
	if err := writeJSON(filepath.Join(tmpDir, "meta.json"), meta); err != nil {
		return PutResult{}, err
	}

	if err := os.Rename(tmpDir, finalDir); err != nil {
		// 并发场景下可能已被另一进程就位
		if s.Has(meta.Digest) {
			return PutResult{Path: finalDir, AlreadyPresent: true}, nil
		}
		return PutResult{}, errs.Wrap(errs.CodeConfigInvalid, "publish content tree", "", err)
	}
	return PutResult{Path: finalDir}, nil
}

// unpackTree 把 commit 的内容树解包到 dst。
//
// 语义（与清单一致）：
//   - 100644 → 普通文件（0644）
//   - 100755 → 可执行文件（0755）
//   - 120000 → symlink，目标为 blob 内容
//
// 解包的条目集合与 digest 的输入完全一致，这是"内容树可重放"的前提。
func unpackTree(ctx context.Context, opts git.Options, repoPath, commit, dst string) error {
	entries, err := git.ListTree(ctx, opts, repoPath, commit)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "create content tree dir", "", err)
	}
	if len(entries) == 0 {
		return nil
	}

	shas := make([]string, 0, len(entries))
	kept := make([]git.TreeEntry, 0, len(entries))
	for _, e := range entries {
		if e.Mode == digest.ModeTree {
			continue
		}
		switch e.Mode {
		case digest.ModeRegular, digest.ModeExecutable, digest.ModeSymlink:
			shas = append(shas, e.SHA)
			kept = append(kept, e)
		case digest.ModeGitlink:
			return errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("cannot unpack gitlink at %q: submodules are not supported in v0.1", e.Path),
				"vendor the submodule into this repository")
		default:
			return errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("cannot unpack unsupported mode %q at %q", e.Mode, e.Path), "")
		}
	}

	contents, err := git.CatFileBatch(ctx, opts, repoPath, shas)
	if err != nil {
		return err
	}

	for i, e := range kept {
		target := filepath.Join(dst, filepath.FromSlash(e.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return errs.Wrap(errs.CodeConfigInvalid, "create parent dir for "+e.Path, "", err)
		}

		switch e.Mode {
		case digest.ModeSymlink:
			linkTarget := string(contents[i])
			if err := os.Symlink(linkTarget, target); err != nil {
				return errs.Wrap(errs.CodeConfigInvalid,
					fmt.Sprintf("cannot create symlink %q → %q in the content store", e.Path, linkTarget),
					"on Windows, enable Developer Mode (or run as Administrator) so symlinks can be created; "+
						"alternatively pin a commit that contains no symlinks", err)
			}
		case digest.ModeExecutable:
			if err := os.WriteFile(target, contents[i], 0o755); err != nil {
				return errs.Wrap(errs.CodeConfigInvalid, "write "+e.Path, "", err)
			}
		default:
			if err := os.WriteFile(target, contents[i], 0o644); err != nil {
				return errs.Wrap(errs.CodeConfigInvalid, "write "+e.Path, "", err)
			}
		}
	}
	return nil
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
