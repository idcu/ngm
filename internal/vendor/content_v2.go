package vendor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
)

// v2 布局（[ADR-019](../docs/adr/adr-019-content-addressed-blobs.md)）：
//
//	blobs/<aa>/<sha>                         文件内容，只存一份（两位 hex 分片）
//	trees/<digest>/{manifest.json,meta.json} 内容树变成一份清单
//
// v1（`sha256/<digest>/tree/`）**仍然可读**，只是不再新增；同一 digest 以 v2 写成功后，
// 旧的整棵树会被删掉（内容已等价）——这就是"不就地迁移"的收敛方式。
const (
	layoutSchemaVersion = 2
	blobsDirName        = "blobs"
	treesDirName        = "trees"
	layoutFile          = "schema"
)

func (s *ContentStore) blobsRoot() string { return filepath.Join(s.root, blobsDirName) }
func (s *ContentStore) treesRoot() string { return filepath.Join(s.root, treesDirName) }

// V2Dir 返回 v2 布局下某个 digest 的目录（清单 + meta 所在处）。
func (s *ContentStore) V2Dir(digestID string) string {
	return filepath.Join(s.treesRoot(), digestHex(digestID))
}

// ManifestPath 返回 v2 的清单路径。
func (s *ContentStore) ManifestPath(digestID string) string {
	return filepath.Join(s.V2Dir(digestID), "manifest.json")
}

// MetaPathV2 返回 v2 的 meta.json 路径（与 v1 的 MetaPath 不同位置）。
func (s *ContentStore) MetaPathV2(digestID string) string {
	return filepath.Join(s.V2Dir(digestID), "meta.json")
}

// BlobPath 返回某个 blob 名对应的路径。
func (s *ContentStore) BlobPath(sha string) string {
	if len(sha) < 3 {
		return ""
	}
	return filepath.Join(s.blobsRoot(), sha[:2], sha)
}

// Manifest 是 v2 的内容树清单。
//
// 字段刻意最少：能回答"有哪些条目、各是什么内容"即可。
type Manifest struct {
	SchemaVersion   int             `json:"schemaVersion"`
	ManifestVersion string          `json:"manifestVersion"`
	Digest          string          `json:"digest"`
	Entries         []ManifestEntry `json:"entries"`
}

// ManifestEntry 是清单里的一条。Mode 用 Git 的写法（100644 / 100755 / 120000）。
type ManifestEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	// SHA 是 **blob 名**。注意它**不是**"内容的 sha256"：它是 `mode` 与内容的
	// 联合哈希（见 `blobKey`）。这样做是为了让 hardlink 能忠实保留可执行位，
	// 理由写在 `blobKey` 的注释里。symlink 条目没有 blob（见 Target）。
	SHA string `json:"sha,omitempty"`
	// Size 是字节数；symlink 时是链接目标字符串的长度。
	Size int64 `json:"size"`
	// Target 只对 symlink 有意义：链接目标字符串就是它的"内容"，因此**不落 blob**
	// （为几个字节再多一个文件没有意义，且多一次 IO）。
	Target string `json:"target,omitempty"`
}

func (s *ContentStore) readManifest(digestID string) (*Manifest, error) {
	data, err := os.ReadFile(s.ManifestPath(digestID))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"content manifest for "+digestID+" is corrupt",
			"delete the entry and re-run `ngm install`", err)
	}
	return &m, nil
}

func (s *ContentStore) readMetaV2(digestID string) (*Meta, error) {
	data, err := os.ReadFile(s.MetaPathV2(digestID))
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "content meta for "+digestID+" is corrupt", "", err)
	}
	return &m, nil
}

// hasManifest 报告 v2 的清单是否存在。
//
// 它是 **v2 布局的权威标记**：清单不存在就等于"这份 digest 不存在"——
// 发布顺序（先落 blob、再写清单、最后 rename）保证了不会有"清单在、内容不在"。
func (s *ContentStore) hasManifest(digestID string) bool {
	if digestID == "" {
		return false
	}
	_, err := os.Stat(s.ManifestPath(digestID))
	return err == nil
}

// hasV2 报告该 digest 的 v2 内容**可用**（清单在且 meta.json 可解析）。
func (s *ContentStore) hasV2(digestID string) bool {
	if !s.hasManifest(digestID) {
		return false
	}
	m, err := s.readMetaV2(digestID)
	return err == nil && m != nil
}

// entriesFromManifest 把 v2 清单转成与布局无关的条目。
func (s *ContentStore) entriesFromManifest(m *Manifest) []ContentEntry {
	out := make([]ContentEntry, 0, len(m.Entries))
	for _, e := range m.Entries {
		ce := ContentEntry{Path: e.Path, Size: e.Size, Mode: e.Mode}
		if e.Mode == digest.ModeSymlink {
			ce.Symlink = true
			ce.LinkTarget = e.Target
		} else {
			ce.Full = s.BlobPath(e.SHA)
		}
		out = append(out, ce)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// blobKey 返回 blob 名：**`mode` 与内容的联合哈希**，而不是"内容的 sha256"。
//
// 为什么把 mode 掺进去（v0.8 B 阶段实现时发现的、ADR-019 没写到的点）：
//
//	落地层（层 3）默认用 hardlink，而 hardlink **共享 inode**，inode 上的 mode 是
//	共享的。于是"同一份字节，一处要 100644、另一处要 100755"这件事，在纯内容寻址的
//	blob 池里**无法表达**——要么副本丢掉可执行位（静默错），要么落地时被迫复制
//	（`hardlink` 模式承诺的"零重复磁盘"就破了）。
//
// 把 mode 作为身份的一部分之后：blob 在池里就以正确的权限落盘（见 writeBlob），
// hardlink 天然带对可执行位，落地层不再有"按 mode chmod"这一步（对 hardlink 而言
// 那一步本来就会改到共享的 blob）。
//
// 代价只有一个，而且是可忽略的：**同一份内容若在两处 mode 不同，会存两份**。
// 内容去重（本布局存在的理由）在"同一个文件在多个 commit 里不变"这个主导场景下
// 完全不受影响；而 ADR-019 关心的 20× 放大正属于那个场景。
//
// 另注：mode 取自 Git（不是文件系统），因此清单与 blob 名**跨平台一致**——
// Windows 上不可表示的可执行位不再让同一 commit 算出不同的清单。
func blobKey(gitMode string, content []byte) string {
	h := sha256.New()
	h.Write([]byte(gitMode))
	h.Write([]byte{0})
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// permForMode 把 Git 的 mode 翻成落盘权限位。
//
// 只区分可执行与否：Git 的树里本来也只有 100644 / 100755 两种普通文件 mode，
// 其余位（读/写）由 umask 与平台决定，不参与可复现性。
func permForMode(gitMode string) os.FileMode {
	if gitMode == digest.ModeExecutable {
		return 0o755
	}
	return 0o644
}

// writeBlob 把单个文件写进 blob 池（已存在则跳过），返回 blob 名。
//
// 原子性：写临时文件 → 设权限 → rename。因此读侧**永远看不到半个 blob**；
// 并发写同一 blob 时，rename 失败即视为"另一个进程赢了"，返回同一个名字。
func (s *ContentStore) writeBlob(content []byte, gitMode string) (string, error) {
	sha := blobKey(gitMode, content)

	bp := s.BlobPath(sha)
	if _, serr := os.Stat(bp); serr == nil {
		return sha, nil
	}
	if merr := os.MkdirAll(filepath.Dir(bp), 0o755); merr != nil {
		return "", errs.Wrap(errs.CodeConfigInvalid, "create blob shard dir", "", merr)
	}

	tmp, cerr := os.CreateTemp(filepath.Dir(bp), ".blob-*")
	if cerr != nil {
		return "", errs.Wrap(errs.CodeConfigInvalid, "create temp blob", "", cerr)
	}
	name := tmp.Name()
	cleanup := func() {
		tmp.Close()
		_ = os.Remove(name)
	}
	if _, werr := tmp.Write(content); werr != nil {
		cleanup()
		return "", errs.Wrap(errs.CodeConfigInvalid, "write blob", "", werr)
	}
	// **权限随 blob 一起落地**：这是 hardlink 能带对可执行位的前提（见 blobKey）。
	// 必须在 rename 之前设置——rename 之后改权限会短暂暴露错误状态。
	if cherr := tmp.Chmod(permForMode(gitMode)); cherr != nil {
		cleanup()
		return "", errs.Wrap(errs.CodeConfigInvalid, "set blob permissions", "", cherr)
	}
	if cerr := tmp.Close(); cerr != nil {
		_ = os.Remove(name)
		return "", errs.Wrap(errs.CodeConfigInvalid, "close temp blob", "", cerr)
	}
	if rerr := os.Rename(name, bp); rerr != nil {
		_ = os.Remove(name)
		if _, serr := os.Stat(bp); serr == nil {
			return sha, nil // 并发：已被另一进程写入
		}
		return "", errs.Wrap(errs.CodeConfigInvalid, "publish blob", "", rerr)
	}
	return sha, nil
}

// publishManifest 把清单与 meta 写进 `trees/` 下的临时目录，再整体 rename 就位。
//
// 顺序的意义：**blob 先落盘（由 Put 完成）、清单后就位**。因此任何时刻
// "清单存在"都蕴含"它引用的 blob 已在池里"，不会出现半个可被引用的内容树。
//
// 临时目录前缀用 `unpackPrefix`（与 `Prune` 共用同一个常量）：
// "哪些目录是残骸"这件事只能有一处定义。
func (s *ContentStore) publishManifest(mf Manifest, meta Meta) error {
	if merr := os.MkdirAll(s.treesRoot(), 0o755); merr != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "create "+s.treesRoot(), "", merr)
	}
	tmpDir, err := os.MkdirTemp(s.treesRoot(), unpackPrefix+"*")
	if err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "create temp manifest dir", "", err)
	}
	defer func() {
		// 成功路径已 rename；失败路径清理
		_ = os.RemoveAll(tmpDir)
	}()

	if werr := writeJSON(filepath.Join(tmpDir, "manifest.json"), mf); werr != nil {
		return werr
	}
	if werr := writeJSON(filepath.Join(tmpDir, "meta.json"), meta); werr != nil {
		return werr
	}

	final := s.V2Dir(meta.Digest)
	if rerr := os.Rename(tmpDir, final); rerr != nil {
		if s.hasManifest(meta.Digest) {
			return nil // 并发：另一进程已经就位
		}
		return errs.Wrap(errs.CodeConfigInvalid, "publish the content manifest", "", rerr)
	}

	// 布局版本标记（幂等；供诊断与将来的兼容性判断）
	_ = os.WriteFile(filepath.Join(s.root, layoutFile), []byte(strconv.Itoa(layoutSchemaVersion)), 0o644)

	// v1 收敛：同一 digest 的内容已经存在于 v2（blob 池），旧的整棵树可以删掉。
	// 失败不必阻断——它只是没省下这次空间；下一轮 `usage` 会把它报出来。
	if legacy := s.PathForDigest(meta.Digest); legacy != final {
		_ = os.RemoveAll(legacy)
	}
	return nil
}
