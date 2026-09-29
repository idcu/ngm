// 本文件提供 fixture Git 仓库的句柄式 API（GitRepo）与"远端"模拟，
// 用于覆盖 M1 验收要求的场景：annotated tag、force push、LFS 指针、gitlink、monorepo 子路径。
//
// 设计取舍：保留 testutils.go 中的过程式 helper（GitInit/GitWriteFile/...）以兼容既有测试，
// 同时提供 GitRepo 让场景编排更接近真实操作序列。
package testutils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GitRepo 是一个 fixture 工作树仓库的句柄。
type GitRepo struct {
	// Dir 是工作树根目录。
	Dir string
	t   *testing.T
}

// NewGitRepo 创建并初始化一个仓库（分支 main + 首个空 commit）。
func NewGitRepo(t *testing.T) *GitRepo {
	t.Helper()
	MustHaveGit(t)
	dir := t.TempDir()
	GitInit(t, dir)
	return &GitRepo{Dir: dir, t: t}
}

// NewGitRepoAt 在指定目录创建仓库（用于需要固定路径的场景，如"远端"裸仓库）。
func NewGitRepoAt(t *testing.T, dir string) *GitRepo {
	t.Helper()
	MustHaveGit(t)
	GitInit(t, dir)
	return &GitRepo{Dir: dir, t: t}
}

// Exec 执行任意 git 命令并返回 stdout。
func (r *GitRepo) Exec(args ...string) string {
	r.t.Helper()
	return GitOutput(r.t, r.Dir, args...)
}

// WriteFile 写入文件（LF 规范化）并 git add。返回自身以便链式调用。
func (r *GitRepo) WriteFile(rel, content string) *GitRepo {
	r.t.Helper()
	GitWriteFile(r.t, r.Dir, rel, content)
	return r
}

// Commit 提交当前索引，返回 commit hash。
func (r *GitRepo) Commit(msg string) string {
	r.t.Helper()
	return GitCommit(r.t, r.Dir, msg)
}

// Tag 创建 tag，返回 tag 解析出的 commit。
func (r *GitRepo) Tag(name string, annotate bool) string {
	r.t.Helper()
	return GitTag(r.t, r.Dir, name, annotate)
}

// Head 返回当前 HEAD commit hash。
func (r *GitRepo) Head() string {
	r.t.Helper()
	return strings.TrimSpace(r.Exec("rev-parse", "HEAD"))
}

// RevParse 解析任意 revision。
func (r *GitRepo) RevParse(rev string) string {
	r.t.Helper()
	return strings.TrimSpace(r.Exec("rev-parse", rev))
}

// WriteLFSPointer 写入一个符合 Git LFS 规范的指针文件并 add。
//
// 指针格式（https://git-lfs.github.com/spec/v1）：
//
//	version https://git-lfs.github.com/spec/v1
//	oid sha256:<64 hex>
//	size <bytes>
//
// M2 的 digest 实现必须检测到这类指针并报错（不得静默哈希）。
func (r *GitRepo) WriteLFSPointer(rel, sha256hex string, size int) *GitRepo {
	r.t.Helper()
	body := "version https://git-lfs.github.com/spec/v1\n" +
		"oid sha256:" + sha256hex + "\n" +
		"size " + itoa(size) + "\n"
	GitWriteFile(r.t, r.Dir, rel, body)
	return r
}

// AddGitlink 在索引中插入一个 gitlink 条目（模拟 submodule，mode 160000）。
//
// 用法：先确保 subCommit 是一个真实存在的 commit（可用另一个 fixture 仓库的 HEAD）。
// 方法把条目写入索引但不提交——调用方随后调用 Commit。
func (r *GitRepo) AddGitlink(relPath, subCommit string) *GitRepo {
	r.t.Helper()
	r.Exec("update-index", "--add", "--cacheinfo", "160000,"+subCommit+","+relPath)
	return r
}

// AddExecutable 写入可执行文件（mode 100755）并 add。
func (r *GitRepo) AddExecutable(rel, content string) *GitRepo {
	r.t.Helper()
	full := filepath.Join(r.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o755); err != nil {
		r.t.Fatal(err)
	}
	r.Exec("add", "--", rel)
	// Windows 上 core.filemode 可能为 false；显式设置索引 mode 以保证跨平台一致
	r.Exec("update-index", "--chmod=+x", "--", rel)
	return r
}

// AddSymlink 写入一个符号链接条目并 add。
//
// Windows 需要开发者模式或管理员权限；失败时 t.Skip（由非 NTFS/无权限环境降级）。
func (r *GitRepo) AddSymlink(rel, target string) *GitRepo {
	r.t.Helper()
	full := filepath.Join(r.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Symlink(target, full); err != nil {
		r.t.Skipf("symlink not supported in this environment: %v", err)
	}
	r.Exec("add", "--", rel)
	return r
}

// ---------------------------------------------------------------------------
// 远端模拟：一个本地的裸仓库，充当 `origin`。
// ---------------------------------------------------------------------------

// NewBareRemote 创建一个空裸仓库作为远端（push 目标）。
//
// `--template=` 让裸仓库不继承开发者的 init.templatedir 模板（避免全局 hooks 混入）。
func NewBareRemote(t *testing.T) string {
	t.Helper()
	MustHaveGit(t)
	dir := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, filepath.Dir(dir), "init", "--bare", "--quiet", "-b", "main", "--template=", dir)
	return dir
}

// SetRemote 把 remoteURL 配置为名为 name 的 remote（默认 origin）。
func (r *GitRepo) SetRemote(name, remoteURL string) *GitRepo {
	r.t.Helper()
	r.Exec("remote", "add", name, remoteURL)
	return r
}

// Push 推送指定 refspec 到 remote。
func (r *GitRepo) Push(remote, refspec string) *GitRepo {
	r.t.Helper()
	args := []string{"push", remote}
	if refspec != "" {
		args = append(args, refspec)
	}
	r.Exec(args...)
	return r
}

// ForcePush 强制推送当前 HEAD 到 remote 的指定分支（默认 main）。
//
// 用途：模拟上游改写历史（tag 重打 / commit 改写）的漂移场景。
func (r *GitRepo) ForcePush(remote, branch string) *GitRepo {
	r.t.Helper()
	if branch == "" {
		branch = "main"
	}
	r.Exec("push", "--force", remote, branch)
	return r
}

// ForceRetag 删除既有 tag 并重新指向 HEAD，然后强制推送 tag。
//
// 这是 verify 的"tag 重打（unexpected drift）"场景的最小复现。
func (r *GitRepo) ForceRetag(remote, tag string, annotate bool) *GitRepo {
	r.t.Helper()
	_, _ = r.Exec2("tag", "-d", tag)
	r.Tag(tag, annotate)
	if remote != "" {
		r.Exec("push", "--force", remote, "refs/tags/"+tag)
	}
	return r
}

// Exec2 与 Exec 相同，但容忍失败（返回 stdout 与 error），用于清理类命令。
func (r *GitRepo) Exec2(args ...string) (string, error) {
	r.t.Helper()
	return GitOutputErr(r.Dir, args...)
}

// WriteFileRaw 写入不做换行转换的字节内容并 add。
func (r *GitRepo) WriteFileRaw(rel string, content []byte) *GitRepo {
	r.t.Helper()
	GitWriteFileRaw(r.t, r.Dir, rel, content)
	return r
}

// AddSymlinkEntry 插入 symlink 条目（mode 120000），不依赖文件系统符号链接能力。
func (r *GitRepo) AddSymlinkEntry(relPath, target string) *GitRepo {
	r.t.Helper()
	GitAddSymlinkEntry(r.t, r.Dir, relPath, target)
	return r
}

// ---------------------------------------------------------------------------
// M2：archiveDigest 的"全能" fixture
// ---------------------------------------------------------------------------

// kitchenSinkBigSize 是 big.bin 的字节数（8 KiB——足够跨过常见的缓冲区边界，
// 又不至于拖慢测试）。
const kitchenSinkBigSize = 8192

// kitchenSinkBigContent 生成确定性的大文件内容（按字节递增，不含随机性）。
func kitchenSinkBigContent() []byte {
	b := make([]byte, kitchenSinkBigSize)
	for i := range b {
		b[i] = byte(i % 256)
	}
	return b
}

// BuildKitchenSinkRepo 创建一个覆盖 ADR-008 全部条目类型的 fixture 仓库。
//
// 覆盖：普通文件 / 空文件 / 可执行 / symlink / 含 CRLF 文件 / 含空格路径 /
// Unicode 路径 / 多级嵌套目录 / 大文件。
//
// 关键性质：**内容完全确定**（无时间戳、无随机数），因此同一 commit 的
// archiveDigest 是稳定值——这是 M2 冻结测试向量的前提。
// commit hash 本身会随提交时间变化，但不进入 digest。
//
// 注意：不含 LFS 与 gitlink——它们是**必须报错**的异常路径，
// 由独立的 fixture（BuildLFSRepo / BuildGitlinkRepo）覆盖。
func BuildKitchenSinkRepo(t *testing.T) *GitRepo {
	t.Helper()
	r := NewGitRepo(t)

	// 普通文件
	r.WriteFile("plain.txt", "hello ngm\n")
	// 空文件（blob 为空）
	r.WriteFile("empty.txt", "")
	// 含 CRLF 的文件：必须原样参与哈希（ADR-008 §关键说明 3）
	r.WriteFileRaw("crlf.txt", []byte("line1\r\nline2\r\n"))
	// 路径含空格
	r.WriteFile("with space.txt", "spaced content\n")
	// Unicode 路径
	r.WriteFile("中文目录/文件.txt", "unicode content\n")
	// 多级嵌套
	r.WriteFile("nested/deep/dir/mod.ts", "export const x = 1\n")
	// 大文件
	r.WriteFileRaw("big.bin", kitchenSinkBigContent())
	// 可执行文件
	r.AddExecutable("scripts/run.sh", "#!/bin/sh\necho ngm\n")

	r.Commit("test: kitchen sink fixture")

	// symlink 通过索引构造（跨平台），必须在 commit 之前加入
	r.AddSymlinkEntry("link-to-plain.txt", "plain.txt")
	r.AddSymlinkEntry("dir/link-up.txt", "../plain.txt")
	r.Commit("test: symlink entries")

	return r
}

// BuildLFSRepo 创建一个包含 Git LFS 指针的仓库（用于验证"必须报错"）。
func BuildLFSRepo(t *testing.T) *GitRepo {
	t.Helper()
	r := NewGitRepo(t)
	r.WriteFile("README.md", "uses lfs\n")
	r.WriteLFSPointer("assets/logo.bin",
		"4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393", 12345)
	r.Commit("test: lfs pointer")
	return r
}

// BuildGitlinkRepo 创建一个包含 gitlink（submodule 引用）的仓库。
//
// gitlink 需要一个真实存在的"子模块 commit"作为目标，这里用同一仓库的
// HEAD 充当（git 只校验它是 40 位 sha）。
func BuildGitlinkRepo(t *testing.T) *GitRepo {
	t.Helper()
	r := NewGitRepo(t)
	r.WriteFile("index.ts", "export const root = 1\n")
	head := r.Commit("test: before gitlink")
	r.AddGitlink("vendor/sub", head)
	r.Commit("test: gitlink entry")
	return r
}

// RewriteHEAD 改写当前 HEAD（amend）后返回新 hash，用于模拟 commit 改写。
//
// 用法：rewrite 之后 ForcePush 到远端，即得"commit 被改写"的漂移场景。
func (r *GitRepo) RewriteHEAD(msg string) string {
	r.t.Helper()
	r.Exec("commit", "--amend", "--no-edit", "-m", msg)
	return r.Head()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
