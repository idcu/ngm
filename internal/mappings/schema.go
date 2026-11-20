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
	"strconv"
	"strings"

	"github.com/idcu/ngm/internal/core/atomicfile"
	"github.com/idcu/ngm/internal/core/errs"
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

	// Path 是 monorepo 子路径依赖的子目录（如 `packages/core`）。可缺失。
	//
	// 为什么需要它（v0.3 E 组）：同一个依赖可以有**多个**子路径条目，
	// 而只靠 `to`（vendor 里的路径）无法**确定性地**还原"该用哪个导入标识符"——
	// 集成脚手架（Vite / esbuild 的 alias、Deno 的 import map）必须知道那个标识符。
	// 从 vendor 路径反推是猜测；`path` 让它成为一个事实。
	//
	// 它是**可选字段**：缺省即"这个依赖没有子路径"。按 schema 版本策略
	// （configuration.md §schema 版本策略："同一大版本内只新增可选字段"），
	// 因此 `version` **不递增**——计划里写的"v2"指的是协议扩展，不是版本号。
	Path string `json:"path,omitempty"`

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

// Specifier 返回该映射的**导入标识符**：`from`，或 `from + "/" + path`。
//
// 它是消费方实际书写的那个字符串，因此也是别名 / import map / tsconfig paths 的键。
//
// 为什么不直接用 `from`：同一个仓库的多个子路径条目**共用** `from`，
// 用它当键只能留下一行，另一行会被指错。实测（esbuild）：只给短键时，
// `github:demo/lib/packages/core` 被改写成 `.../packages/core/packages/core` 而失败；
// 若留存的那一行恰好指向一个存在的目录，则会**静默解析到错误的代码**——
// 那是比报错更糟的结果。
//
// 放在协议层而不是各消费方：`ngm build` 与 `ngm integrations` 必须给出同一个键，
// 两处各写一遍迟早漂移。
func (m Mapping) Specifier() string {
	if m.Path == "" {
		return m.From
	}
	return m.From + "/" + strings.Trim(m.Path, "/")
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
	// 导入标识符（from 或 from/path）是 build / integrations 的**键**：
	// 两条同键、不同 to 的映射不会报错，只会让其中一个静默覆盖另一个——
	// 最终解析到哪个包取决于遍历顺序。这种"看起来有两条、实际只有一条"
	// 必须在协议层拒绝，而不是留给每个消费方各自去重。
	//
	// **只查声明了 `path` 的条目**：v1 文件（v0.2 生成）里同一个 monorepo 的
	// 多个子路径都没有 `path`，于是共用 `from` 作为键——那是历史格式，
	// `mappings validate` 对它的契约是"仍可读、给一条再生成警告"，不是拒绝。
	// 生成器产出重复（两个条目声明同一个 path）仍然会被这里拦住。
	seenSpecifier := make(map[string]int, len(f.Mappings))
	for i, m := range f.Mappings {
		if m.From == "" {
			return errs.New(errs.CodeConfigInvalid,
				"mappings["+itoa(i)+"] is missing `from`", "")
		}
		if m.To == "" {
			return errs.New(errs.CodeConfigInvalid,
				"mappings["+itoa(i)+"] is missing `to`", "")
		}
		if m.Path != "" {
			spec := m.Specifier()
			if prev, dup := seenSpecifier[spec]; dup {
				return errs.New(errs.CodeConfigInvalid,
					"mappings["+itoa(i)+"] duplicates import specifier "+strconv.Quote(spec)+
						" (also mappings["+itoa(prev)+"])",
					"each (from, path) pair may appear only once; remove the duplicate entry")
			}
			seenSpecifier[spec] = i
		}
		if m.Path != "" {
			p := strings.TrimSpace(m.Path)
			// 形状校验：`path` 必须是依赖内部的一个相对子目录。
			// 拒绝绝对路径与 `..` 是因为它最终会参与拼出文件系统路径——
			// 协议层拦掉，比让每个消费方各自小心更可靠。
			if p != m.Path || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") ||
				strings.Contains(p, "..") {
				return errs.New(errs.CodeConfigInvalid,
					"mappings["+itoa(i)+"] has an invalid `path`",
					"`path` must be a relative subdirectory inside the dependency (e.g. packages/core)")
			}
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
//
// 先 Validate 再落盘，与 lock.Write 同一条纪律：mappings 是 `ngm build`
// 解析导入时的键表，写进一份内部不一致的文件，失败会推迟到下一次构建，
// 且表现成"找不到模块"而不是"生成 mappings 的那一步有问题"。
func Write(path string, f *File) error {
	if err := f.Validate(); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid,
			"refusing to write an invalid "+FileName,
			"this means the mappings file was built incorrectly; re-run `ngm install` "+
				"and report the output above if it persists", err)
	}
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

// writeFileAtomic 转调 [atomicfile.Write]（v0.82：唯一实现处）。
//
// 与 lock / config 那两份同形；本包此前没有 Chmod，
// 于是 `ngm.mappings.json` 写出后是 **0600**。
func writeFileAtomic(path string, data []byte) error {
	return atomicfile.Write(path, data)
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
