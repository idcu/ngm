package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// AdapterKind 是 adapter 的实现方式（architecture/engine-adapter.md §Adapter 类型）。
type AdapterKind string

const (
	// AdapterEmbed 在 ngm 进程内实现。v0.1 仅 self stub。
	AdapterEmbed AdapterKind = "embed"
	// AdapterSubprocess spawn 外部 CLI —— **默认方式**。
	AdapterSubprocess AdapterKind = "subprocess"
	// AdapterWasm 在 wasm 运行时内执行（v0.3+，本 build 未实现）。
	AdapterWasm AdapterKind = "wasm"
	// AdapterRemote 调用远端构建服务（v0.3+，本 build 未实现）。
	AdapterRemote AdapterKind = "remote"
)

// AllAdapters 按固定顺序返回所有 adapter 类型。
func AllAdapters() []AdapterKind {
	return []AdapterKind{AdapterEmbed, AdapterSubprocess, AdapterWasm, AdapterRemote}
}

// IsValid 报告 a 是否为已知的 adapter 类型。
func (a AdapterKind) IsValid() bool {
	for _, v := range AllAdapters() {
		if a == v {
			return true
		}
	}
	return false
}

const (
	// CatalogVersion 是 ngm.engines.json 的 schema 版本（guides/configuration.md §schema 版本策略）。
	CatalogVersion = 1
	// FileName 是引擎清单文件名。
	FileName = "ngm.engines.json"
	// SelfEngineName 是兜底 stub 引擎的名字。
	SelfEngineName = "self"
)

// Entry 是引擎清单中的一条：**一个引擎 + 一个能力类别**。
//
// 同一引擎可以有多条（esbuild 同时提供 transform 与 bundle），
// 这与 architecture/engine-adapter.md §引擎清单 的示例形态一致。
type Entry struct {
	// Name 是引擎名（如 esbuild）。
	Name string `json:"name"`
	// Kind 是本条目提供的能力类别。
	Kind EngineKind `json:"kind"`
	// Adapter 是实现方式。
	Adapter AdapterKind `json:"adapter"`
	// Command 是"程序名 + 固定前缀参数"，按空白切分（见 splitCommand）。
	Command string `json:"command"`
	// Version 是清单声明的版本（**声明**，未必等于本机实际版本）。
	Version string `json:"version,omitempty"`
	// SupportedInput 是支持的输入扩展名（含点）。
	SupportedInput []string `json:"supportedInput,omitempty"`
	// DefaultOptions 是默认选项，可被 ngm.json 的 engines.<kind>.options 覆盖。
	DefaultOptions map[string]any `json:"defaultOptions,omitempty"`
	// Optional 为 true 表示该条目**不要求本机安装**。
	//
	// 背景：v0.2 E 组把 tsc / postcss 纳入内置清单（它们已适配，理应可被发现），
	// 但并非每个项目都需要类型检查或用 postcss 编译 CSS。若把它们当作"必须有"，
	// `ngm engines validate` 会因为"你没装 tsc"而对**所有**用户报 exit 5——
	// 那是一条假警报：不装 tsc 的人根本没打算做类型检查。
	//
	// 真正用到却没装时，命令本身会在那一刻失败并给出安装提示（exit 5 + hint），
	// 那才是该报错的时机。版本探测不受影响：装了就探测并比对声明版本。
	Optional bool `json:"optional,omitempty"`

	// Stub 为 true 表示该条目不产出生产产物（仅 dry-run / 兜底）。
	//
	// 这是 ngm 侧的扩展字段：ADR-005 要求 self 引擎"只做兜底、dry-run、
	// 离线 stub"，把这条约束写进数据而不是靠名字硬编码判断，
	// 使 `ngm build --engine=self`（非 dry-run）能明确拒绝而不是静默产出空产物。
	Stub bool `json:"stub,omitempty"`

	// Builtin 标记条目来自内置清单（用户清单里不写这个字段）。
	Builtin bool `json:"-"`
	// Program 与 Args 由 Command 派生，见 splitCommand。
	Program string   `json:"-"`
	Args    []string `json:"-"`
}

// Key 是条目在清单中的唯一标识（name + kind）。
func (e Entry) Key() string { return e.Name + "/" + string(e.Kind) }

// splitCommand 把清单里的 command 拆成程序名与固定前缀参数。
//
//	"esbuild"                           → ("esbuild", nil)
//	"deno check"                        → ("deno", ["check"])
//	"tsc --emitDeclarationOnly"         → ("tsc", ["--emitDeclarationOnly"])
//	`"C:\Program Files\tool.exe" --x`   → (`C:\Program Files\tool.exe`, ["--x"])
//
// **不经过 shell**：切分后直接 execve，因此不支持管道、重定向、变量展开、
// 通配符与命令替换。这是安全选择而非省事——清单来自仓库（外部输入），
// 若走 `sh -c` 就等于把任意命令执行权交给任何一个 PR。
//
// 唯一支持的"语法"是**双引号包裹含空白的片段**：Windows 的默认安装路径
// 几乎都带空格（`C:\Program Files\...`），不给这个能力会让合法路径无法表达。
// 引号只影响切分、不进入参数值；不做任何转义处理。
//
// 需要复杂行为的用户应写一个包装脚本，在脚本内部处理——那样 shell 语义由
// 用户显式承担，而不是由 ngm 隐式开启。
func splitCommand(command string) (program string, args []string) {
	var (
		out     []string
		cur     strings.Builder
		inQuote bool
		started bool
	)
	flush := func() {
		if started {
			out = append(out, cur.String())
			cur.Reset()
			started = false
		}
	}

	for i := 0; i < len(command); i++ {
		switch c := command[i]; {
		case c == '"':
			// 引号本身不进参数值；`""` 仍产出一个空片段
			inQuote = !inQuote
			started = true
		case (c == ' ' || c == '\t' || c == '\r' || c == '\n') && !inQuote:
			flush()
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	flush()

	if len(out) == 0 {
		return "", nil
	}
	return out[0], out[1:]
}

// Catalog 是引擎清单。
type Catalog struct {
	Version int     `json:"version"`
	Engines []Entry `json:"engines"`
}

// BuiltinCatalog 返回内置引擎清单。
//
// v0.1 只适配 esbuild（transform + bundle）与 self（dry-run stub）：
// development/v0.1-plan.md M6 明确"v0.1 只需 esbuild"。
//
// tsc / deno / postcss **刻意不预置**：architecture/engine-adapter.md 要求
// "清单里出现的引擎必须已经适配"，预置未适配的条目会让 `ngm engines list`
// 对用户说谎（显示"可用"却跑不了）。已装好这些工具的用户可以用
// ngm.engines.json 自行声明——subprocess 协议的驱动是通用的。
func BuiltinCatalog() *Catalog {
	c := &Catalog{Version: CatalogVersion}

	// esbuild：bundle + transform（同一程序，两条清单条目）
	for _, e := range []Entry{
		{
			Name: "esbuild", Kind: KindBundle, Adapter: AdapterSubprocess,
			Command: "esbuild", Version: "0.24.0",
			SupportedInput: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".css"},
			DefaultOptions: map[string]any{"format": "esm", "target": "es2020"},
		},
		{
			Name: "esbuild", Kind: KindTransform, Adapter: AdapterSubprocess,
			Command: "esbuild", Version: "0.24.0",
			SupportedInput: []string{".ts", ".tsx", ".js", ".jsx"},
			DefaultOptions: map[string]any{"target": "es2020"},
		},
	} {
		e.Builtin = true
		c.Engines = append(c.Engines, e)
	}

	// v0.2 E 组新增：tsc 与 postcss 已适配，因此进入内置清单
	// （engine-adapter.md 的口径是"清单里出现的引擎必须已经适配"）。
	//
	// 一律 Optional：见 Entry.Optional——未安装不算 issue。
	// **deno 刻意不进内置清单**：它的 bundle 是 Deno >= 2.4 的实验特性，
	// 且一旦内置就会在 typeCheck 的默认选择里排在 tsc 前面（按名字排序），
	// 让多数 TS 项目意外用上 deno。需要 deno 的用户在 ngm.engines.json 里
	// 显式声明——argv 翻译已经支持它。
	for _, e := range []Entry{
		{
			Name: "typescript", Kind: KindTypeCheck, Adapter: AdapterSubprocess,
			Command: "tsc", Version: "5.6.0", Optional: true,
			SupportedInput: []string{".ts", ".tsx"},
		},
		{
			Name: "typescript", Kind: KindTypeDecl, Adapter: AdapterSubprocess,
			Command: "tsc --emitDeclarationOnly", Version: "5.6.0", Optional: true,
			SupportedInput: []string{".ts", ".tsx"},
		},
		{
			Name: "postcss", Kind: KindCSS, Adapter: AdapterSubprocess,
			Command: "postcss", Version: "8.4.0", Optional: true,
			SupportedInput: []string{".css"},
		},
	} {
		e.Builtin = true
		c.Engines = append(c.Engines, e)
	}

	// self：所有能力类别都注册，使任何 kind 都能被"规划"（--dry-run）或
	// 在配置里显式标注"此处没有真实引擎"。它永不产出产物（Stub）。
	for _, k := range AllKinds() {
		c.Engines = append(c.Engines, Entry{
			Name: SelfEngineName, Kind: k, Adapter: AdapterEmbed,
			Version: "0", Stub: true, Builtin: true,
		})
	}

	c.normalize()
	return c
}

// LoadCatalog 读取并合并生效的引擎清单。
//
// 优先级（高的覆盖低的）：项目 ngm.engines.json > 全局 <ngmHome>/ngm.engines.json
// > 内置清单。ngmHome 通常是 `~/.ngm`（由调用方从 vendor.Layout 传入，
// 本包不读环境变量）。
//
// 覆盖粒度是**整条**（name + kind 相同即整条替换），不做字段级合并：
// 字段级合并会产生"命令来自用户、默认选项来自内置"的混合体，
// 出问题时无法解释某一项到底从哪来。
func LoadCatalog(projectDir, ngmHome string) (*Catalog, error) {
	cat := BuiltinCatalog()

	sources := []string{}
	if ngmHome != "" {
		sources = append(sources, filepath.Join(ngmHome, FileName))
	}
	if projectDir != "" {
		sources = append(sources, filepath.Join(projectDir, FileName))
	}

	for _, path := range sources {
		user, err := readCatalog(path)
		if err != nil {
			return nil, err
		}
		if user != nil {
			cat.overlay(user)
		}
	}
	cat.normalize()
	return cat, nil
}

// readCatalog 读取一份用户清单；文件不存在返回 (nil, nil)。
func readCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read "+path, "", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var c Catalog
	if err := dec.Decode(&c); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"parse "+path,
			"the engine catalog must match the schema in architecture/engine-adapter.md", err)
	}
	if c.Version != CatalogVersion {
		return nil, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("%s: unsupported `version` %d", path, c.Version),
			fmt.Sprintf("this build supports version %d", CatalogVersion))
	}
	if c.Engines == nil {
		c.Engines = []Entry{}
	}
	return &c, nil
}

// overlay 用 user 里的条目替换/追加到 cat。
func (c *Catalog) overlay(user *Catalog) {
	index := map[string]int{}
	for i, e := range c.Engines {
		index[e.Key()] = i
	}
	for _, e := range user.Engines {
		e.Builtin = false
		if i, ok := index[e.Key()]; ok {
			c.Engines[i] = e
			continue
		}
		c.Engines = append(c.Engines, e)
		index[e.Key()] = len(c.Engines) - 1
	}
}

// normalize 派生 Program/Args 并按 (kind 顺序, name) 稳定排序。
//
// 排序固定使 `ngm engines list` 与 --json 输出可 diff、可快照，
// 不受清单书写顺序或合并路径影响。
func (c *Catalog) normalize() {
	if c.Version == 0 {
		c.Version = CatalogVersion
	}
	for i := range c.Engines {
		c.Engines[i].Program, c.Engines[i].Args = splitCommand(c.Engines[i].Command)
	}
	order := map[EngineKind]int{}
	for i, k := range AllKinds() {
		order[k] = i
	}
	sort.SliceStable(c.Engines, func(i, j int) bool {
		a, b := c.Engines[i], c.Engines[j]
		if order[a.Kind] != order[b.Kind] {
			return order[a.Kind] < order[b.Kind]
		}
		return a.Name < b.Name
	})
}

// Find 按 (kind, name) 查找条目。
func (c *Catalog) Find(kind EngineKind, name string) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	for _, e := range c.Engines {
		if e.Kind == kind && e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// NamesFor 返回某能力类别下所有引擎名（按清单顺序，即排序后的顺序）。
func (c *Catalog) NamesFor(kind EngineKind) []string {
	var out []string
	if c == nil {
		return out
	}
	for _, e := range c.Engines {
		if e.Kind == kind {
			out = append(out, e.Name)
		}
	}
	return out
}

// EntriesFor 返回某能力类别下所有条目。
func (c *Catalog) EntriesFor(kind EngineKind) []Entry {
	var out []Entry
	if c == nil {
		return out
	}
	for _, e := range c.Engines {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 校验
// ---------------------------------------------------------------------------

// IssueKind 区分"清单写错了"与"引擎当前不可用"，并直接对应退出码。
type IssueKind string

const (
	// IssueSchema 清单结构错误 → exit 3。
	IssueSchema IssueKind = "schema"
	// IssueUnavailable 引擎在本机找不到 → exit 5。
	IssueUnavailable IssueKind = "unavailable"
	// IssueUnimplemented adapter 类型本 build 未实现 → exit 5。
	IssueUnimplemented IssueKind = "unimplemented"
	// IssueVersion 清单声明的版本与本机实际版本不一致 → **信息**，退出码 0。
	//
	// 刻意不是错误：旧版本的引擎通常照样能跑，把"声明 5.6.0、实际 5.3.0"
	// 判为失败会让 validate 变成版本管理器而非校验器。它存在是为了让
	// `--json` 与 human 输出把这件事**说出来**，而不是藏着。
	IssueVersion IssueKind = "version"
)

// ExitCode 返回该问题类别对应的 ngm 退出码。
func (k IssueKind) ExitCode() int {
	switch k {
	case IssueSchema:
		return errs.CodeConfigInvalid.ExitCode()
	case IssueVersion:
		return 0
	default:
		return errs.CodeEngineNotFound.ExitCode()
	}
}

// probeVersion 探测条目对应程序的实际版本。
//
// 复用 subprocess 驱动而不是另写一份：探测方式必须与引擎被调用的方式一致
// （同一个 program + 同一段前缀参数），否则"声明 vs 实际"的比较没有意义。
func probeVersion(e Entry) (string, error) {
	return newSubprocessEngine(e, "").Version()
}

// Issue 是清单校验发现的一个问题。
type Issue struct {
	// Entry 是涉及的条目（"name/kind"）；空表示整份清单的问题。
	Entry string `json:"entry,omitempty"`
	// Kind 决定退出码（见 IssueKind）。
	Kind IssueKind `json:"kind"`
	// Message 是可读描述。
	Message string `json:"message"`
}

// ExitCode 依据问题集合计算 `ngm engines validate` 的退出码。
//
// 规则：**schema 错误优先于可用性问题**。
//
// 清单结构坏掉时，"某个引擎不可用"这类结论是建立在无意义数据上的
// （例如 command 字段被写成对象、kind 拼错），先让用户修结构（3），
// 修好后再看可用性（5）。规则的唯一实现在这里，CLI 与测试都调它——
// 避免"命令行按一种规则、测试按另一种规则"的漂移。
func ExitCode(issues []Issue) int {
	code := 0
	for _, is := range issues {
		switch is.Kind {
		case IssueSchema:
			return errs.CodeConfigInvalid.ExitCode()
		case IssueUnavailable, IssueUnimplemented:
			if code == 0 {
				code = errs.CodeEngineNotFound.ExitCode()
			}
		}
	}
	return code
}

// Validate 校验清单的结构与本机可用性。
//
// 返回的问题按 (清单顺序) 排列，便于稳定输出与快照。
// adapter 未实现（wasm / remote）也算问题——清单声明了本 build 做不到的事。
func (c *Catalog) Validate() []Issue {
	var issues []Issue

	if c == nil {
		return []Issue{{Kind: IssueSchema, Message: "engine catalog is nil"}}
	}
	if c.Version != CatalogVersion {
		issues = append(issues, Issue{
			Kind:    IssueSchema,
			Message: fmt.Sprintf("unsupported `version` %d (this build supports %d)", c.Version, CatalogVersion),
		})
	}

	seen := map[string]bool{}
	for _, e := range c.Engines {
		key := e.Key()
		label := e.Key()

		if strings.TrimSpace(e.Name) == "" {
			issues = append(issues, Issue{Kind: IssueSchema, Message: "engine entry has an empty `name`"})
			continue
		}
		if !e.Kind.IsValid() {
			issues = append(issues, Issue{Entry: label, Kind: IssueSchema,
				Message: fmt.Sprintf("unknown `kind` %q; valid kinds: %s", e.Kind, kindList())})
		}
		if !e.Adapter.IsValid() {
			issues = append(issues, Issue{Entry: label, Kind: IssueSchema,
				Message: fmt.Sprintf("unknown `adapter` %q; valid adapters: %s", e.Adapter, adapterList())})
		} else {
			switch e.Adapter {
			case AdapterWasm, AdapterRemote:
				issues = append(issues, Issue{Entry: label, Kind: IssueUnimplemented,
					Message: fmt.Sprintf("adapter %q is not implemented in this build (planned for v0.3)", e.Adapter)})
			case AdapterEmbed:
				if e.Name != SelfEngineName {
					issues = append(issues, Issue{Entry: label, Kind: IssueUnimplemented,
						Message: fmt.Sprintf("no embedded engine named %q in this build (only %q)", e.Name, SelfEngineName)})
				}
			case AdapterSubprocess:
				if e.Program == "" {
					issues = append(issues, Issue{Entry: label, Kind: IssueSchema,
						Message: "subprocess entries need a non-empty `command`"})
					continue
				}
				// 注意：可用性检查之后**不能**提前 continue——后面的 stub / 重名
				// 校验仍要执行。一个条目可以同时"没装"且"写法错误"，
				// 只报前者会让用户修完安装再撞上第二个问题。
				if _, err := exec.LookPath(e.Program); err != nil {
					if !e.Optional {
						// 可选引擎未安装不是问题：用到它的那一刻自然会失败并给出提示，
						// 在这里报会让"没装 tsc"变成一条与用户无关的红。
						issues = append(issues, Issue{Entry: label, Kind: IssueUnavailable,
							Message: fmt.Sprintf("`%s` was not found on PATH", e.Program)})
					}
				} else if v, verr := probeVersion(e); verr == nil && v != "" && e.Version != "" &&
					!strings.HasPrefix(v, e.Version) {
					// 已安装：探测实际版本并与清单声明比对（v0.2 计划"validate 覆盖版本探测"）。
					// 版本不一致是**信息**而非失败（旧版本常常照样能跑），走 IssueVersion。
					issues = append(issues, Issue{Entry: label, Kind: IssueVersion,
						Message: fmt.Sprintf("catalog declares version %s, `%s` reports %q",
							e.Version, e.Program, v)})
				}
			}
		}

		if e.Stub && e.Adapter != AdapterEmbed {
			issues = append(issues, Issue{Entry: label, Kind: IssueSchema,
				Message: "`stub: true` is only allowed for embed adapters"})
		}
		if seen[key] {
			issues = append(issues, Issue{Entry: label, Kind: IssueSchema,
				Message: "duplicate entry for the same (name, kind)"})
		}
		seen[key] = true
	}

	return issues
}

// kindList 与 adapterList 生成稳定的枚举列表（用于错误消息）。
func kindList() string {
	parts := make([]string, 0, len(AllKinds()))
	for _, k := range AllKinds() {
		parts = append(parts, string(k))
	}
	return strings.Join(parts, ", ")
}

func adapterList() string {
	parts := make([]string, 0, len(AllAdapters()))
	for _, a := range AllAdapters() {
		parts = append(parts, string(a))
	}
	return strings.Join(parts, ", ")
}
