// Package mappings 实现 ngm.mappings.json 的 schema、生成与校验。
//
// 规范唯一事实源：modules/p4-ecosystem.md §mappings 协议。
//
// 作用：构建工具（Vite / esbuild / Deno / Webpack）不认识 `github:` 裸导入，
// 需要一层映射把它们指向 vendor 里的真实路径。mappings 就是这层间接性——
// 它不参与可证明性（那由 lock + digest 承担），只负责"让工具找得到代码"。
package mappings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

const (
	// SchemaVersion 是 mappings 协议版本（p4-ecosystem.md：当前 1）。
	SchemaVersion = 1

	// FileName 是 mappings 文件名（项目根目录）。
	FileName = "ngm.mappings.json"
)

// File 是 ngm.mappings.json 的顶层结构。
//
// 字段顺序即序列化顺序（encoding/json 按 struct 声明顺序输出）。
type File struct {
	Version  int       `json:"version"`
	Mappings []Mapping `json:"mappings"`
}

// Mapping 是单个依赖的映射。
type Mapping struct {
	// From 是依赖标识（slug 形式，如 `github:my-org/utils`）。
	// 与 ngm.json / ngm.lock 的 name 口径一致。
	From string `json:"from"`

	// To 是 vendor 中的实际路径，形如 `./ngm.vendor/github.com/my-org/utils`。
	//
	// 带 `./` 前缀是协议的一部分（构建工具的 alias 值直接使用它）。
	// monorepo 子路径依赖时指向子目录本身，而不是仓库根。
	To string `json:"to"`

	// Main 是入口文件（相对依赖根，如 `./index.js`）。可缺失。
	Main string `json:"main,omitempty"`

	// Types 是类型声明入口（相对依赖根，如 `./index.d.ts`）。可缺失。
	Types string `json:"types,omitempty"`
}

// NewFile 创建一个空的 mappings 文件。
func NewFile() *File {
	return &File{Version: SchemaVersion, Mappings: []Mapping{}}
}

// VendorRelRoot 是 vendor 目录在项目中的默认相对路径前缀（含 `./`）。
//
// 协议示例（p4-ecosystem.md）固定使用它；`vendor.mode: global` 时调用方可传入
// 其他根（例如全局 vendor 目录），此时 `to` 指向真实位置。
const VendorRelRoot = "./ngm.vendor"

// ToFromVendorPath 用默认根把 vendor 相对路径转为 `to` 字段值。
func ToFromVendorPath(vendorRelPath string) string {
	return ToJoin(VendorRelRoot, vendorRelPath)
}

// ToJoin 把 vendor 根与相对路径拼成 `to` 字段值。
//
// 根为绝对路径（global 模式）时原样保留，相对根则确保 `./` 前缀。
func ToJoin(vendorRelRoot, vendorRelPath string) string {
	root := strings.TrimRight(strings.TrimSpace(filepath.ToSlash(vendorRelRoot)), "/")
	rel := strings.Trim(filepath.ToSlash(vendorRelPath), "/")
	if root == "" {
		root = VendorRelRoot
	}
	if !strings.HasPrefix(root, "./") && !strings.HasPrefix(root, "/") && !looksLikeWindowsAbs(root) {
		root = "./" + root
	}
	return root + "/" + rel
}

// looksLikeWindowsAbs 识别 `C:/...` 形式的绝对路径。
func looksLikeWindowsAbs(p string) bool {
	return len(p) >= 2 && p[1] == ':' &&
		((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z'))
}

// Sort 按 from（字节序）稳定排序，保证输出确定性。
//
// 排序键为 from + to：同一 slug 的 monorepo 子路径是不同条目。
func (f *File) Sort() {
	sort.SliceStable(f.Mappings, func(i, j int) bool {
		if f.Mappings[i].From != f.Mappings[j].From {
			return f.Mappings[i].From < f.Mappings[j].From
		}
		return f.Mappings[i].To < f.Mappings[j].To
	})
}

// Marshal 序列化为规范字节流（2 空格缩进、LF、末尾换行）。
func (f *File) Marshal() ([]byte, error) {
	if f == nil {
		return nil, errs.New(errs.CodeConfigInvalid, "cannot marshal nil mappings", "")
	}
	out := *f
	if out.Version == 0 {
		out.Version = SchemaVersion
	}
	if out.Mappings == nil {
		out.Mappings = []Mapping{}
	}
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "marshal "+FileName, "", err)
	}
	return append(data, '\n'), nil
}

// Validate 校验结构与版本。
func (f *File) Validate() error {
	if f == nil {
		return errs.New(errs.CodeConfigInvalid, "mappings file is nil", "")
	}
	if f.Version != SchemaVersion {
		return errs.New(
			errs.CodeConfigInvalid,
			"unsupported mappings `version`",
			"this build supports version 1; regenerate with `ngm install`")
	}
	for i, m := range f.Mappings {
		if m.From == "" {
			return errs.New(errs.CodeConfigInvalid,
				"mappings["+itoa(i)+"] is missing `from`", "")
		}
		if m.To == "" {
			return errs.New(errs.CodeConfigInvalid,
				"mappings["+itoa(i)+"] is missing `to`", "")
		}
	}
	return nil
}

// Read 读取并校验 ngm.mappings.json；文件不存在时返回 (nil, nil)。
func Read(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read "+path, "", err)
	}
	var f File
	// 与 lock 一致：容忍未知字段（同 MAJOR 内的向前兼容）
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"parse "+path, "delete it and re-run `ngm install`", err)
	}
	if err := f.Validate(); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "invalid "+path, "", err)
	}
	return &f, nil
}

// Write 以确定性格式写回（原子写）。
func Write(path string, f *File) error {
	data, err := f.Marshal()
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// Find 返回项目目录下的 mappings 路径。
func Find(projectDir string) string {
	return filepath.Join(projectDir, FileName)
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ngm-mappings-*.tmp")
	if err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "create temp file in "+dir,
			"check directory permissions", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return errs.Wrap(errs.CodeConfigInvalid, "write temp mappings", "", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return errs.Wrap(errs.CodeConfigInvalid, "sync temp mappings", "", err)
	}
	if err := tmp.Close(); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "close temp mappings", "", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "replace "+path,
			"close any editor holding the file", err)
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
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
