// Package integrations 生成外部构建工具消费 ngm.mappings.json 所需的配置。
//
// 三条贯穿全包的设计约束（v0.3 计划 B 组）：
//
//  1. **不改用户已有配置**。用户自己写过的 `vite.config.ts` / `tsconfig.json`
//     永不被覆盖；内容不同时报冲突并给出 diff，让他自己决定怎么合。
//  2. **幂等**。重复运行结果一致；内容已经一致时报 up to date，不是错误。
//  3. **全有或全无**。任一处冲突就一个文件都不写——半套脚手架比没有更难收拾。
//
// 另外，所有产物都以 **mappings 为唯一事实源**：Vite / esbuild / Webpack 的配置
// 在构建时**读取** ngm.mappings.json（依赖变了不用重新生成），Deno 的 import map
// 因为要写成静态 JSON 只能内联（这一点在文档里明说）。
package integrations

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/mappings"
)

// Tool 是受支持的集成目标。
type Tool string

const (
	ToolVite    Tool = "vite"
	ToolEsbuild Tool = "esbuild"
	ToolDeno    Tool = "deno"
	ToolWebpack Tool = "webpack"
)

// Tools 返回全部受支持的工具（按名字排序，供 help 与错误提示使用）。
func Tools() []Tool {
	list := []Tool{ToolDeno, ToolEsbuild, ToolVite, ToolWebpack}
	sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
	return list
}

// ToolList 返回 `vite|esbuild|deno|webpack` 形式的清单文本。
func ToolList() string {
	names := make([]string, 0, len(Tools()))
	for _, t := range Tools() {
		names = append(names, string(t))
	}
	return strings.Join(names, "|")
}

// ParseTool 解析工具名，未知工具返回 exit 3 的错误。
func ParseTool(s string) (Tool, error) {
	for _, t := range Tools() {
		if string(t) == strings.ToLower(strings.TrimSpace(s)) {
			return t, nil
		}
	}
	return "", errs.New(errs.CodeConfigInvalid,
		"unknown integration "+quoted(s),
		"supported: "+ToolList())
}

// EntryFile 返回供**打包器**（Vite / esbuild / Webpack）使用的入口文件路径。
//
// 必须指向**文件**而不是目录：目录只有在恰好是 `index.*` 时才解析得到，
// 而 `main` 完全可能是 `./src/main.ts`——那种情况下别名的目录写法会直接失败。
// `main` 缺失时退回目录，并让调用方产生一条警告。
func EntryFile(m mappings.Mapping) string {
	if m.Main != "" {
		return JoinVendor(m.To, m.Main)
	}
	return strings.TrimSuffix(m.To, "/")
}

// TypeFile 返回供 **tsconfig `paths`** 使用的路径：类型声明优先，其次是源码入口。
//
// TS 语言服务拿到 `types` 就有完整类型；没有时指向源码同样能推断类型
// （实测：指向 `.ts` 源文件时，故意制造的类型不匹配会正确报错）。
func TypeFile(m mappings.Mapping) string {
	if m.Types != "" {
		return JoinVendor(m.To, m.Types)
	}
	return EntryFile(m)
}

// JoinVendor 把 `to` 与相对其中的入口拼起来（统一 `/`，去掉重复斜杠）。
func JoinVendor(to, rel string) string {
	base := strings.TrimSuffix(strings.TrimSpace(to), "/")
	r := strings.TrimPrefix(strings.TrimSpace(rel), "./")
	if r == "" {
		return base
	}
	return base + "/" + strings.TrimPrefix(r, "/")
}

// Artifact 是一个待写入的产物。
type Artifact struct {
	// Path 相对项目根（用 `/` 分隔，输出时再转成本地分隔符）。
	Path string
	// Content 是完整文件内容（含结尾换行）。
	Content []byte
	// Owned 表示这个文件名是 ngm 的（`ngm.*`）：可以覆盖重生成。
	//
	// 用户可能编辑过的配置文件（`vite.config.ts` / `tsconfig.json`）**不是** own 的：
	// 它们只会被新建，或在内容不同时报冲突。
	Owned bool
	// CreateOnly 表示这个文件只在**不存在**时生成；已存在时跳过并提示 Hint。
	//
	// 用于"只需加一行、但那行必须由用户加"的文件（`tsconfig.json`、`deno.json`）：
	// 它们已存在不是**冲突**——文件本身没有任何错，只是 ngm 无权改它。
	// 把它报成冲突会让用户以为出了问题，而实际上什么都没坏。
	CreateOnly bool
	// Hint 在 CreateOnly 命中已有文件时给出**一行**可操作的指引。
	Hint string
	// Purpose 说明这个文件的用途（用于输出，让用户知道自己在看什么）。
	Purpose string
}

// Status 是一次写入的结论。
type Status string

const (
	// StatusCreated 表示文件此前不存在，已创建。
	StatusCreated Status = "created"
	// StatusUpdated 表示文件已存在但内容有变，已更新（仅限 ngm 自己的文件）。
	StatusUpdated Status = "updated"
	// StatusUpToDate 表示内容已经一致，未改动。
	StatusUpToDate Status = "up to date"
	// StatusSkipped 表示文件已存在且只允许新建，因此未改动（非错误，附 Hint）。
	StatusSkipped Status = "skipped"
	// StatusConflict 表示文件已存在且内容不同、且不是 ngm 的文件——**未改动**。
	StatusConflict Status = "conflict"
)

// Outcome 是一个产物的处理结果。
type Outcome struct {
	Path   string
	Status Status
	// Diff 在冲突时给出"当前文件 → 应当包含的内容"的差异提示。
	Diff string
	// Hint 在 skipped 时给出用户需要手动做的那一件事。
	Hint string
}

// Conflict 报告是否存在冲突。
func HasConflict(outcomes []Outcome) bool {
	for _, o := range outcomes {
		if o.Status == StatusConflict {
			return true
		}
	}
	return false
}

// Plan 生成某个工具需要的全部产物（不写盘；是否落盘由 Apply 决定）。
//
// 返回 warnings 是"能生成、但用户需要知道"的事（例如某个依赖没有入口文件）。
func Plan(tool Tool, f *mappings.File) ([]Artifact, []string, error) {
	if f == nil {
		return nil, nil, errs.New(errs.CodeConfigInvalid,
			mappings.FileName+" is required to plan an integration",
			"run `ngm install` first")
	}

	var arts []Artifact
	var warns []string

	switch tool {
	case ToolVite:
		a, w := viteArtifacts(f)
		arts, warns = a, w
	case ToolEsbuild:
		a, w := esbuildArtifacts(f)
		arts, warns = a, w
	case ToolWebpack:
		a, w := webpackArtifacts(f)
		arts, warns = a, w
	case ToolDeno:
		a, w, err := denoArtifacts(f)
		if err != nil {
			return nil, nil, err
		}
		arts, warns = a, w
	default:
		return nil, nil, errs.New(errs.CodeConfigInvalid,
			"no generator for integration "+quoted(string(tool)), "")
	}

	// tsconfig 的 `paths` 与工具无关：`github:` 前缀在任何 TS 项目里都需要它，
	// 否则类型检查报"找不到模块"（见 guides/build.md）。因此一并生成。
	ts, tsWarns, err := tsconfigArtifacts(f)
	if err != nil {
		return nil, nil, err
	}
	arts = append(arts, ts...)
	warns = append(warns, tsWarns...)
	return arts, warns, nil
}

// Apply 按"全有或全无"写入产物。
//
// 先全部判定，再全部写入：只要有一个冲突，就**一个字节都不写**。
// 半套脚手架（写了 A、没写 B）会让项目处于既不能构建、又看不出缺什么的中间态。
func Apply(projectDir string, arts []Artifact, dryRun bool) ([]Outcome, error) {
	outcomes := make([]Outcome, 0, len(arts))

	for _, a := range arts {
		abs := filepath.Join(projectDir, filepath.FromSlash(a.Path))
		existing, err := os.ReadFile(abs)
		switch {
		case err == nil && string(existing) == string(a.Content):
			outcomes = append(outcomes, Outcome{Path: a.Path, Status: StatusUpToDate})
		case err == nil && a.CreateOnly:
			outcomes = append(outcomes, Outcome{Path: a.Path, Status: StatusSkipped, Hint: a.Hint})
		case err == nil && a.Owned:
			outcomes = append(outcomes, Outcome{Path: a.Path, Status: StatusUpdated})
		case err == nil:
			outcomes = append(outcomes, Outcome{
				Path:   a.Path,
				Status: StatusConflict,
				Diff:   diffLines(string(existing), string(a.Content)),
			})
		case os.IsNotExist(err):
			outcomes = append(outcomes, Outcome{Path: a.Path, Status: StatusCreated})
		default:
			return nil, errs.Wrap(errs.CodeConfigInvalid, "read "+a.Path, "", err)
		}
	}

	if HasConflict(outcomes) || dryRun {
		return outcomes, nil
	}

	for i, a := range arts {
		if outcomes[i].Status == StatusUpToDate || outcomes[i].Status == StatusSkipped {
			continue
		}
		abs := filepath.Join(projectDir, filepath.FromSlash(a.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, errs.Wrap(errs.CodeConfigInvalid, "create directory for "+a.Path, "", err)
		}
		if err := os.WriteFile(abs, a.Content, 0o644); err != nil {
			return nil, errs.Wrap(errs.CodeConfigInvalid, "write "+a.Path, "", err)
		}
	}
	return outcomes, nil
}

// FormatOutcomes 渲染写入结果，返回 (文本, 冲突数)。
func FormatOutcomes(outcomes []Outcome) (string, int) {
	var sb strings.Builder
	conflicts := 0
	for _, o := range outcomes {
		fmt.Fprintf(&sb, "  %-10s %s\n", string(o.Status), o.Path)
		if o.Hint != "" {
			fmt.Fprintf(&sb, "             %s\n", o.Hint)
		}
		if o.Status == StatusConflict {
			conflicts++
		}
	}
	return sb.String(), conflicts
}

// FormatConflicts 渲染冲突说明：告诉用户"文件是你的，我不动它，这是差异"。
func FormatConflicts(outcomes []Outcome) string {
	var sb strings.Builder
	for _, o := range outcomes {
		if o.Status != StatusConflict {
			continue
		}
		fmt.Fprintf(&sb, "%s already exists and ngm did not touch it.\n", o.Path)
		sb.WriteString("Difference between the current file (-) and what ngm would write (+):\n")
		sb.WriteString(o.Diff)
	}
	return sb.String()
}

// MarshalJSONFile 生成稳定格式的 JSON（两空格缩进 + 结尾换行）。
//
// 确定性来自 encoding/json 对 map 键的排序输出：Deno import map 与 tsconfig paths
// 的键顺序都**无语义**（两者都按"最具体匹配"选键），因此字典序即可。
// 真正对顺序敏感的只有打包器的别名表，而那部分在生成的 js/ts 配置里运行时自排。
func MarshalJSONFile(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "encode generated json", "", err)
	}
	return append(data, '\n'), nil
}

// quoted 给错误信息加引号，让用户输入在消息里边界清楚。
func quoted(s string) string { return fmt.Sprintf("%q", s) }

// ConflictError 构造"有文件被保留"时的统一错误（exit 3）。
//
// 它不是失败，而是**停下来让用户决定**：那些文件是用户写的，
// ngm 只能告诉他差在哪里，不能替他改。
func ConflictError(conflicts int) error {
	return errs.New(errs.CodeConfigInvalid,
		fmt.Sprintf("%d file(s) already exist and were left untouched", conflicts),
		"merge the difference shown above by hand, then re-run `ngm integrations add` "+
			"(see docs/guides/build.md for each tool)")
}

// NextStep 返回生成后的下一步提示，按工具给出具体可执行的命令。
func NextStep(tool Tool) string {
	switch tool {
	case ToolVite:
		return "next: vite reads the aliases at startup - no further setup needed"
	case ToolEsbuild:
		return "next: node " + EsbuildScriptPath
	case ToolDeno:
		return "next: deno run --import-map=ngm.importmap.json <entry>"
	case ToolWebpack:
		return "next: webpack --config ngm.webpack.cjs"
	}
	return "next: run your build tool"
}
