package mappings

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// FileReader 从**依赖根**读取文件，relPath 用 `/` 分隔。
//
// 返回 (content, exists, error)。调用方通常从 content store 的对应子树读取，
// 这样映射生成与实际 vendor 内容严格一致。
type FileReader func(relPath string) ([]byte, bool, error)

// Entry 是推断出的入口信息。
type Entry struct {
	// Main 是入口文件（相对依赖根，带 `./` 前缀）。
	Main string
	// Types 是类型声明入口（同上）。
	Types string
	// Source 记录推断依据，用于诊断与测试。
	Source string
}

// GenerateInput 描述一个待生成映射的依赖。
type GenerateInput struct {
	// From 是依赖标识（slug）。
	From string
	// VendorRelRoot 是 vendor 根的 `to` 前缀；空时使用默认的 `./ngm.vendor`。
	//
	// `vendor.mode: global` 时调用方传入全局 vendor 目录，`to` 因此指向真实位置。
	VendorRelRoot string
	// VendorRelPath 是该依赖在 vendor 中的相对路径（`github.com/org/repo[/sub]`）。
	VendorRelPath string
	// SubPath 是 monorepo 子路径（`packages/core`），空表示依赖根。
	//
	// 它写进 `path` 字段：消费方据此拼出导入标识符
	// （`github:org/repo` vs `github:org/repo/packages/core`）。
	SubPath string
	// Read 读取该依赖根下的文件（monorepo 时根即子路径）。
	Read FileReader
}

// Generate 生成 mappings 文件与警告列表。
//
// 入口推断（guides/configuration.md §入口推断）按**三层顺序**：
//
//  1. 依赖自身 `ngm.json` 的 `main` / `types`
//  2. `package.json` 的 `exports["."]`（其次 `main` / `types`）
//  3. `./index.ts` / `./index.js`（类型：`./index.d.ts`）
//
// 全部缺失时输出警告，映射**仍然生成但不带入口字段**
// （规范要求："全部缺失时 ngm install 输出警告，映射仍生成但不带入口字段"）。
func Generate(inputs []GenerateInput) (*File, []string) {
	f := NewFile()
	var warnings []string

	for _, in := range inputs {
		root := in.VendorRelRoot
		if root == "" {
			root = VendorRelRoot
		}
		m := Mapping{
			From: in.From,
			To:   ToJoin(root, in.VendorRelPath),
			Path: strings.Trim(strings.TrimSpace(in.SubPath), "/"),
		}

		if in.Read != nil {
			entry, warns := InferEntry(in.Read)
			m.Main = entry.Main
			m.Types = entry.Types
			for _, w := range warns {
				warnings = append(warnings, fmt.Sprintf("%s: %s", in.From, w))
			}
		}

		f.Mappings = append(f.Mappings, m)
	}

	f.Sort()
	return f, warnings
}

// InferEntry 按三层规则推断入口，返回推断结果与警告。
func InferEntry(read FileReader) (Entry, []string) {
	// ---- 第 1 层：依赖自身 ngm.json ----
	if e, ok := entryFromNgmJSON(read); ok {
		return e, nil
	}

	// ---- 第 2 层：package.json ----
	if e, ok := entryFromPackageJSON(read); ok {
		return e, nil
	}

	// ---- 第 3 层：index 约定 ----
	if e, ok := entryFromIndexConvention(read); ok {
		return e, nil
	}

	return Entry{}, []string{
		"no entry point found (checked ngm.json `main`/`types`, package.json `exports`/`main`, and ./index.ts|js); " +
			"the mapping has no `main`/`types` — upstream can add them to ngm.json",
	}
}

func entryFromNgmJSON(read FileReader) (Entry, bool) {
	data, ok, err := read("ngm.json")
	if err != nil || !ok {
		return Entry{}, false
	}
	var m struct {
		Main  string `json:"main"`
		Types string `json:"types"`
	}
	if json.Unmarshal(data, &m) != nil {
		return Entry{}, false
	}
	if strings.TrimSpace(m.Main) == "" && strings.TrimSpace(m.Types) == "" {
		return Entry{}, false
	}
	return Entry{Main: relPath(m.Main), Types: relPath(m.Types), Source: "ngm.json"}, true
}

func entryFromPackageJSON(read FileReader) (Entry, bool) {
	data, ok, err := read("package.json")
	if err != nil || !ok {
		return Entry{}, false
	}
	var pj struct {
		Main    string          `json:"main"`
		Types   string          `json:"types"`
		Exports json.RawMessage `json:"exports"`
	}
	if json.Unmarshal(data, &pj) != nil {
		return Entry{}, false
	}

	var main, types string
	if len(pj.Exports) > 0 {
		main, types = parseExports(pj.Exports)
	}
	if main == "" {
		main = pj.Main
	}
	if types == "" {
		types = pj.Types
	}
	if strings.TrimSpace(main) == "" && strings.TrimSpace(types) == "" {
		return Entry{}, false
	}
	return Entry{Main: relPath(main), Types: relPath(types), Source: "package.json"}, true
}

// parseExports 从 `exports` 字段中提取入口与类型。
//
// 支持三种实际写法：
//
//	"exports": "./index.js"
//	"exports": { ".": "./index.js" }
//	"exports": { ".": { "types": "./index.d.ts", "import": "./index.mjs", "default": "./index.js" } }
func parseExports(raw json.RawMessage) (main, types string) {
	// 字符串形式
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, ""
	}

	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return "", ""
	}

	target := raw
	if dot, ok := obj["."]; ok {
		target = dot
	}

	// `"."` 直接是字符串
	var ts string
	if json.Unmarshal(target, &ts) == nil {
		return ts, ""
	}

	// `"."` 是条件映射
	var cond map[string]json.RawMessage
	if json.Unmarshal(target, &cond) != nil {
		return "", ""
	}
	if v, ok := cond["types"]; ok {
		var tv string
		if json.Unmarshal(v, &tv) == nil {
			types = tv
		}
	}
	// 运行时的优先顺序：import → module → default → require
	for _, key := range []string{"import", "module", "default", "require"} {
		if v, ok := cond[key]; ok {
			var mv string
			if json.Unmarshal(v, &mv) == nil && mv != "" {
				main = mv
				break
			}
		}
	}
	return main, types
}

func entryFromIndexConvention(read FileReader) (Entry, bool) {
	for _, cand := range []string{"index.ts", "index.js", "index.mjs", "index.cjs"} {
		if _, ok, err := read(cand); err != nil || !ok {
			continue
		}
		e := Entry{Main: "./" + cand, Source: "index convention"}
		for _, t := range []string{"index.d.ts", "index.d.mts", "index.d.cts"} {
			if _, tok, terr := read(t); terr == nil && tok {
				e.Types = "./" + t
				break
			}
		}
		return e, true
	}
	return Entry{}, false
}

// relPath 规范化入口路径：统一 `/` 分隔并确保带 `./` 前缀。
//
// 协议要求 `to` 使用 `./ngm.vendor/...` 形式；`main`/`types` 同理，
// 以便构建工具的 alias 直接使用。
func relPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = filepath.ToSlash(p)
	if strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") {
		return p
	}
	return "./" + strings.TrimPrefix(p, "/")
}
