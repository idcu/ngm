// atomicfile — **原子写文件**的唯一实现（v0.82）。
//
// 为什么需要一个包：此前 `writeFileAtomic` 有**三份**实现，分别在
// internal/lock/io.go、internal/config/io.go、internal/mappings/schema.go。
// 它们逐行相同，差别只在临时文件前缀与错误措辞——而正是这种"只差一点"的复制，
// 让其中一份修好的东西不会传到另外两份。
//
// 现成的证据：v0.66 修了 lock 的那份（`os.CreateTemp` 建的是 0600，
// rename 之后 ngm.lock 就变成 0600，而它是要提交进 Git、供团队共享的文件）。
// **另外两份至今没有那行 Chmod** ——也就是说 `ngm.json` 与
// `ngm.mappings.json` 每次原子写之后权限都是 0600。
// 这不是"少了一处优化"，是一条**只在另外两个文件上还活着的缺陷**。
//
// 合并之后：规则一处，三个文件一起修好；`ngm sbom --out` / `ngm attest --out`
// 也走同一条路径（它们此前是直接 `os.WriteFile`，中断会留下截断文件）。
package atomicfile

import (
	"os"
	"path/filepath"

	"github.com/idcu/ngm/internal/core/errs"
)

// Perm 是原子写产出的文件权限。
//
// 为什么必须是 0644 而不是沿用 `os.CreateTemp` 的 0600：
// ngm 写的文件（ngm.json / ngm.lock / ngm.mappings.json / 导出的产物）
// 都是**要提交进 Git、供团队共享**的。0600 会让同机的其他用户与部分
// CI 缓存读不到它，而且 Git 会记录一次 mode change。
const Perm = 0o644

// Write 把 data 原子地写到 path：同目录临时文件 → 落盘 → rename。
//
// 顺序是有讲究的：
//
//  1. Chmod 必须在 rename **之前**——rename 之后改权限，会让文件
//     短暂以错误权限存在（而它已经以目标名字可见了）；
//  2. Sync 在 Close 之前——把数据推到磁盘，避免"rename 成功但内容没落盘"；
//  3. rename 是同目录内的原子替换，要么旧文件在、要么新文件在。
//
// 失败路径一定会清掉临时文件（defer os.Remove），不会留下 `.ngm-*.tmp` 残骸。
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ngm-*.tmp")
	if err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "create temp file in "+dir,
			"check directory permissions", err)
	}
	tmpName := tmp.Name()
	// 成功路径已 rename（此时 Remove 对已不存在的路径无害）；失败路径清理残骸。
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return errs.Wrap(errs.CodeConfigInvalid, "write temp file", "", err)
	}
	if err := tmp.Chmod(Perm); err != nil {
		tmp.Close()
		return errs.Wrap(errs.CodeConfigInvalid, "set temp file permissions",
			"check permissions on the target directory", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return errs.Wrap(errs.CodeConfigInvalid, "sync temp file", "", err)
	}
	if err := tmp.Close(); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "close temp file", "", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "replace "+path,
			"close any editor holding the file", err)
	}
	return nil
}
