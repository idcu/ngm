package lock

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/idcu/ngm/internal/core/atomicfile"
	"github.com/idcu/ngm/internal/core/errs"
)

// Read 读取并校验 ngm.lock。
//
// 文件不存在时返回 (nil, nil)——"无 lock" 是合法状态（`ngm install` 会生成它），
// 调用方据此区分"首次安装"与"尊重既有 lock"。
func Read(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read "+path, "", err)
	}
	return Parse(data, path)
}

// Parse 从**已读到的字节**解析并校验 lock（v0.82）。
//
// 为什么需要它：`ngm attest` 要把 lock 的**原始字节哈希**写进语句
// （外部参数钉的是"你审过的那份文件"），同时又需要解析后的结构。
// 此前它是两次独立的 `os.ReadFile`——两次之间文件若被改动，
// 语句里的 subject（来自第一次）与 `lock.digest`（来自第二次）
// 会描述**两份不同的文件**。对一份要被签名的证据，这是不能接受的。
//
// 读一次、两用，同一份字节既被解析也被哈希，那个窗口就不存在了。
func Parse(data []byte, path string) (*File, error) {
	var f File
	dec := json.NewDecoder(bytes.NewReader(data))
	// 与 ngm.json 不同，lock 的未知字段**不报错**：按 locking.md
	// "同 MAJOR 内只允许新增可选字段，旧版本工具应忽略未知字段继续工作"，
	// 向前兼容是格式契约的一部分。
	if err := dec.Decode(&f); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"parse "+path+" (is it valid JSON?)",
			"delete ngm.lock and re-run `ngm install` to regenerate it", err)
	}
	if err := f.Validate(); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "invalid "+path, "", err)
	}
	return &f, nil
}

// Write 以确定性格式写回 ngm.lock（原子写）。
//
// 调用方应先 Sort（见 File.Marshal 的说明）。
//
// v0.66：写前必须 Validate。此前非法条目（例如 7 位短 commit）能被写进 lock，
// 而 lock 是"依赖状态的唯一事实源"——写进一份自己读不回来的文件，
// 会让下一次 install/verify 直接 exit 3，且用户无法通过正常路径修复
// （评估报告 P2#6/#8 的贯穿路径）。拒绝在这里，说清在错误里。
func Write(path string, f *File) error {
	if err := f.Validate(); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid,
			"refusing to write an invalid "+filepath.Base(path),
			"this means the lock was built incorrectly; re-run the command that produced it, "+
				"and report the output above if it persists", err)
	}
	data, err := f.Marshal()
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// writeFileAtomic 转调 [atomicfile.Write]（v0.82：唯一实现处）。
//
// 此前本包自己有一份，与 config / mappings 那两份逐行相同、只差临时文件前缀——
// 于是 v0.66 在这里修的 0644 权限没有传到另外两份（那里 `ngm.json`
// 与 `ngm.mappings.json` 至今写出 0600）。规则不写在第二个地方。
func writeFileAtomic(path string, data []byte) error {
	return atomicfile.Write(path, data)
}

// LooksLikeLock 是 Read 的轻量前置检查（不做完整解析），
// 供 CLI 在决定"是否需要读取"时避免无谓 IO。
func LooksLikeLock(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// Find 返回项目目录下的 lock 路径。
func Find(projectDir string) string {
	return filepath.Join(projectDir, FileName)
}
