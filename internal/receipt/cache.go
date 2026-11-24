package receipt

import (
	"os"
	"path/filepath"
	"strings"
)

// CacheEntryName 把摘要键转成可跨平台当目录名的形态：去掉 "sha256:" 前缀。
// Windows 不允许目录名里出现冒号，直接拿 Digest() 的返回值建目录会失败。
func CacheEntryName(key string) string {
	return strings.TrimPrefix(key, "sha256:")
}

// CacheDir 返回本地构建缓存根目录：<ngm-home>/build-cache（默认即 ~/.ngm/build-cache）。
//
// 它只做本地：不上传、不共享、不查远端。home 由调用方从 vendor.DefaultLayout().Home 取得，
// 于是 NGM_HOME 的隔离语义与 ngm 其它用户态数据一致。
func CacheDir(home string) string {
	return filepath.Join(home, "build-cache")
}

// Cached 是一次缓存命中读到的内容。
//
// ExpectedDigest 是写入时记下的产物摘要；ActualDigest 是本次读出的字节现算的摘要。
// 两者不等就是**污染**，调用方必须失效并重建，不能静默返回错误产物。
type Cached struct {
	Data           []byte
	ExpectedDigest string
	ActualDigest   string
}

// ReadCache 读取一个缓存条目。miss 返回 (nil, nil)：没读到不等于读坏了。
func ReadCache(dir, key string) (*Cached, error) {
	entry := filepath.Join(dir, CacheEntryName(key))
	artifact := filepath.Join(entry, "artifact")
	meta := filepath.Join(entry, "digest")
	data, err := os.ReadFile(artifact)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	expected, err := os.ReadFile(meta)
	if err != nil {
		if os.IsNotExist(err) {
			// 有产物没摘要：按污染处理，交给调用方重建。
			return &Cached{Data: data, ActualDigest: Digest(data)}, nil
		}
		return nil, err
	}
	return &Cached{
		Data:           data,
		ExpectedDigest: string(expected),
		ActualDigest:   Digest(data),
	}, nil
}

// WriteCache 写入一个缓存条目。摘要与字节一起落盘，才能发现之后的污染。
func WriteCache(dir, key string, data []byte, digest string) error {
	entryDir := filepath.Join(dir, CacheEntryName(key))
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(entryDir, "artifact"), data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(entryDir, "digest"), []byte(digest), 0o644)
}

// RemoveCache 失效（删除）一个缓存条目；目录不存在不是错误。
func RemoveCache(dir, key string) error {
	return os.RemoveAll(filepath.Join(dir, CacheEntryName(key)))
}
