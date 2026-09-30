package integrations

import (
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/mappings"
)

// ResolveEntry 是一个"标识符 → 目标"的解析项。
type ResolveEntry struct {
	// Key 是导入标识符（Specifier 的结果）。
	Key string
	// Target 是 vendor 中的目标路径。
	Target string
}

// ResolveEntries 计算**打包器**（Vite / esbuild / Webpack）的解析项，按键长降序排列。
//
// 两个刻意的取舍：
//
//   - **只用精确键**（不带结尾 `/` 的前缀项）。同时给出精确键与前缀键时，
//     `github:org/repo/packages/core` 会同时匹配两条，谁生效取决于工具内部规则——
//     那是我**没有逐一实测**的行为，不该当成约定写进生成器。代价是"深入依赖内部的
//     裸导入"（`github:org/repo/src/util`）不被支持：依赖自己的相对导入由它内部解析，
//     外部按声明标识符导入即可。
//   - **目标是入口文件**而不是目录：目录只有在恰好是 `index.*` 时才解析得到，
//     而 `main` 完全可能是 `./src/main.ts`。
//
// 长键优先是**防御性**的，不是必需的：实测 esbuild 在多个键都能匹配时会选最具体的
// 那个，与 `--alias:` 的先后无关。保留这个顺序是因为它对未逐一实测的工具（Vite /
// Webpack）无害且更符合直觉——而不是因为我们证明过它必要。
func ResolveEntries(f *mappings.File) []ResolveEntry {
	entries := make([]ResolveEntry, 0, len(f.Mappings))
	for _, m := range f.Mappings {
		entries = append(entries, ResolveEntry{Key: m.Specifier(), Target: EntryFile(m)})
	}
	sort.SliceStable(entries, func(i, j int) bool { return len(entries[i].Key) > len(entries[j].Key) })
	return entries
}

// TsconfigPaths 计算 `compilerOptions.paths` 条目。
//
// 与打包器的解析项**不共用**函数：这里的目标是类型声明（`types` 优先），
// 而打包器要的是运行时入口（`main`）。两者常常不是同一个文件。
func TsconfigPaths(f *mappings.File) map[string][]string {
	out := make(map[string][]string, len(f.Mappings)*2)
	for _, m := range f.Mappings {
		out[m.Specifier()] = []string{TypeFile(m)}
		if m.Path == "" {
			out[m.From+"/*"] = []string{strings.TrimSuffix(m.To, "/") + "/*"}
		}
	}
	return out
}

// DenoImports 计算 Deno import map 的 `imports` 条目（返回有序键，便于确定性输出）。
//
// 与打包器不同，Deno 的映射目标必须是**文件**：目录形式的键在 Deno 里解析不了。
// 因此 `main` 缺失的依赖无法生成精确键——这时只保留前缀项并发一条警告，
// 而不是写一条看起来能用、实际解析失败的映射。
func DenoImports(f *mappings.File) (keys []string, imports map[string]string, warns []string) {
	imports = make(map[string]string, len(f.Mappings)*2)
	for _, m := range f.Mappings {
		if m.Main != "" || m.Types != "" {
			imports[m.Specifier()] = EntryFile(m)
		} else {
			warns = append(warns, m.From+
				" has no `main`/`types` in its mapping, so no exact Deno specifier could be generated for it "+
				"(Deno cannot resolve a directory); only the prefix entry is available")
		}
		if m.Path == "" {
			imports[m.From+"/"] = strings.TrimSuffix(m.To, "/") + "/"
		}
	}

	keys = make([]string, 0, len(imports))
	for k := range imports {
		keys = append(keys, k)
	}
	// 长键优先，理由同 ResolveEntries
	sort.SliceStable(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	return keys, imports, warns
}
