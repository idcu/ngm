package lock

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/idcu/ngm/internal/errs"
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
func Write(path string, f *File) error {
	data, err := f.Marshal()
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// writeFileAtomic 原子写文件（同目录临时文件 + rename）。
//
// 为何重要：lock 是"依赖状态的唯一事实源"，被中断截断的 lock 会让
// 下一次 install 完全无法判断状态。
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ngm-lock-*.tmp")
	if err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "create temp file in "+dir,
			"check directory permissions", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return errs.Wrap(errs.CodeConfigInvalid, "write temp lock", "", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return errs.Wrap(errs.CodeConfigInvalid, "sync temp lock", "", err)
	}
	if err := tmp.Close(); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "close temp lock", "", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "replace "+path,
			"close any editor holding the file", err)
	}
	return nil
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
