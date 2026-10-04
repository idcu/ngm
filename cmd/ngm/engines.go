package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/errs"
)

const enginesUsage = `ngm engines — inspect and validate the engine catalog

USAGE:
  ngm engines list [--json] [--dir=<dir>]
  ngm engines info <name> [--json] [--dir=<dir>]
  ngm engines validate [--json] [--dir=<dir>]

SUBCOMMANDS:
  list       show every catalog entry with its availability and probed version
  info       show one engine in detail (a name may appear under several kinds)
  validate   check the catalog schema and whether each engine is usable

FLAGS:
  --json       machine-readable output
  --dir=<dir>  project directory (default: .)

CATALOG SOURCES (highest wins):
  <project>/ngm.engines.json  →  <ngm home>/ngm.engines.json  →  built-in

EXIT CODES:
  0  ok
  3  the catalog is malformed
  5  an engine in the catalog is not usable (missing, or an unimplemented adapter)
`

// engineRow 是 `ngm engines list` 的一行（也是 --json 的元素）。
type engineRow struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Adapter 是实现方式（subprocess / embed / wasm / remote）。
	Adapter string `json:"adapter"`
	// Command 是清单里声明的命令（含固定前缀参数）。
	Command string `json:"command,omitempty"`
	// DeclaredVersion 是清单声明的版本，Version 是本机探测到的实际版本。
	//
	// 两者都给是有意的：声明与实际不一致本身就是需要看见的信息
	// （比如清单写 0.24.0 而机器上是 0.18.0，某些选项可能不存在）。
	DeclaredVersion string         `json:"declaredVersion,omitempty"`
	Version         string         `json:"version,omitempty"`
	Available       bool           `json:"available"`
	Stub            bool           `json:"stub,omitempty"`
	Builtin         bool           `json:"builtin"`
	SupportedInput  []string       `json:"supportedInput,omitempty"`
	DefaultOptions  map[string]any `json:"defaultOptions,omitempty"`
}

// runEngines 处理 `ngm engines <subcommand>`。
func runEngines(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("engines", stderr)
	jsonOut := fs.Bool("json", false, "machine-readable output")
	dirFlag := fs.String("dir", ".", "project directory")
	fs.Usage = func() { fmt.Fprint(stderr, enginesUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "json", Bool: true}, {Name: "dir"},
	})); err != nil {
		return 3
	}
	if fs.NArg() < 1 {
		fmt.Fprint(stderr, enginesUsage)
		return 3
	}

	sub := fs.Arg(0)
	rest := fs.Args()[1:]

	ec, err := newEngineContext(*dirFlag, stderr)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	switch sub {
	case "list":
		if len(rest) > 0 {
			fmt.Fprint(stderr, enginesUsage)
			return 3
		}
		return runEnginesList(ec, *jsonOut, stdout, stderr)
	case "info":
		if len(rest) != 1 {
			fmt.Fprint(stderr, enginesUsage)
			return 3
		}
		return runEnginesInfo(ctx, ec, rest[0], *jsonOut, stdout, stderr)
	case "validate":
		if len(rest) > 0 {
			fmt.Fprint(stderr, enginesUsage)
			return 3
		}
		return runEnginesValidate(ec, *jsonOut, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand: %s\n\n%s\n", sub, enginesUsage)
		return 3
	}
}

// runEnginesList 列出全部清单条目。
func runEnginesList(ec *engineContext, jsonOut bool, stdout, stderr io.Writer) int {
	rows := engineRows(ec.catalog, ec.env.ProjectDir, "")

	if jsonOut {
		return writeJSON(stdout, stderr, rows)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tADAPTER\tCOMMAND\tVERSION\tSTATUS\tSOURCE")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Name, r.Kind, r.Adapter,
			orDash(r.Command),
			orDash(versionCell(r)),
			statusCell(r),
			sourceCell(r))
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "write listing: %v\n", err)
		return 1
	}

	if len(rows) == 0 {
		fmt.Fprintln(stdout, "the catalog is empty")
	}
	return 0
}

// runEnginesInfo 展示单个引擎的详情（同名可能横跨多个 kind）。
func runEnginesInfo(ctx context.Context, ec *engineContext, name string, jsonOut bool, stdout, stderr io.Writer) int {
	rows := engineRows(ec.catalog, ec.env.ProjectDir, name)
	if len(rows) == 0 {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeEngineNotFound,
			fmt.Sprintf("no engine named %q in the catalog", name),
			"run `ngm engines list` to see what is available"))
	}

	if jsonOut {
		return writeJSON(stdout, stderr, rows)
	}

	for i, r := range rows {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "%s (%s)\n", r.Name, r.Kind)
		fmt.Fprintf(stdout, "  adapter:    %s\n", r.Adapter)
		fmt.Fprintf(stdout, "  command:    %s\n", orDash(r.Command))
		fmt.Fprintf(stdout, "  declared:   %s\n", orDash(r.DeclaredVersion))
		fmt.Fprintf(stdout, "  detected:   %s\n", orDash(r.Version))
		fmt.Fprintf(stdout, "  status:     %s\n", statusCell(r))
		fmt.Fprintf(stdout, "  source:     %s\n", sourceCell(r))
		if len(r.SupportedInput) > 0 {
			fmt.Fprintf(stdout, "  input:      %s\n", strings.Join(r.SupportedInput, " "))
		}
		if len(r.DefaultOptions) > 0 {
			fmt.Fprintf(stdout, "  defaults:   %s\n", renderOptions(r.DefaultOptions))
		}
	}
	return 0
}

// runEnginesValidate 校验清单结构与本机可用性。
//
// 退出码规则单源在 adapter.ExitCode（schema 错误优先于可用性问题）。
func runEnginesValidate(ec *engineContext, jsonOut bool, stdout, stderr io.Writer) int {
	issues := ec.catalog.Validate()

	if jsonOut {
		payload := struct {
			Version int             `json:"version"`
			OK      bool            `json:"ok"`
			Issues  []adapter.Issue `json:"issues"`
		}{Version: ec.catalog.Version, OK: len(issues) == 0, Issues: issues}
		if code := writeJSON(stdout, stderr, payload); code != 0 {
			return code
		}
		return adapter.ExitCode(issues)
	}

	if len(issues) == 0 {
		fmt.Fprintf(stdout, "catalog is valid; all %d entr(ies) are usable\n", len(ec.catalog.Engines))
		return 0
	}
	for _, is := range issues {
		label := is.Entry
		if label == "" {
			label = "(catalog)"
		}
		fmt.Fprintf(stdout, "✗ %s: %s [%s]\n", label, is.Message, is.Kind)
	}
	fmt.Fprintf(stdout, "\n%d issue(s) found\n", len(issues))
	return adapter.ExitCode(issues)
}

// engineRows 把清单转成输出行，并探测每个条目的可用性与实际版本。
//
// 探测结果按"程序 + 固定参数"缓存：同一程序的多条清单条目（esbuild 同时提供
// bundle 与 transform）只 spawn 一次版本探测。
func engineRows(cat *adapter.Catalog, dir, only string) []engineRow {
	cache := map[string]probeResult{}
	rows := make([]engineRow, 0, len(cat.Engines))

	for _, e := range cat.Engines {
		if only != "" && e.Name != only {
			continue
		}
		key := e.Program + "\x00" + strings.Join(e.Args, " ")
		pr, ok := cache[key]
		if !ok {
			pr = probeEngine(e, dir)
			cache[key] = pr
		}
		rows = append(rows, engineRow{
			Name:            e.Name,
			Kind:            string(e.Kind),
			Adapter:         string(e.Adapter),
			Command:         e.Command,
			DeclaredVersion: e.Version,
			Version:         pr.version,
			Available:       pr.available,
			Stub:            e.Stub,
			Builtin:         e.Builtin,
			SupportedInput:  e.SupportedInput,
			DefaultOptions:  e.DefaultOptions,
		})
	}
	return rows
}

// probeResult 是一次可用性/版本探测的结果。
type probeResult struct {
	available bool
	version   string
}

// probeEngine 探测单个条目的可用性与版本。
//
// 探测失败不报错：`ngm engines list` 的职责是**如实呈现**，不是中断。
// "哪个引擎缺了"正是它要回答的问题。
func probeEngine(e adapter.Entry, dir string) probeResult {
	eng, err := adapter.NewEngine(e, dir)
	if err != nil {
		return probeResult{}
	}
	pr := probeResult{available: eng.Available()}
	if pr.available && !e.Stub {
		if v, verr := eng.Version(); verr == nil {
			pr.version = v
		}
	}
	return pr
}

// statusCell 渲染一行状态。
func statusCell(r engineRow) string {
	switch {
	case r.Stub:
		return "stub"
	case r.Available:
		return "ok"
	default:
		return "not found"
	}
}

// versionCell 渲染版本列：优先实际版本，没有则退到声明版本。
func versionCell(r engineRow) string {
	if r.Version != "" {
		return r.Version
	}
	if r.DeclaredVersion != "" {
		return r.DeclaredVersion + " (declared)"
	}
	return ""
}

// sourceCell 渲染条目来源。
func sourceCell(r engineRow) string {
	if r.Builtin {
		return "built-in"
	}
	return adapter.FileName
}

// renderOptions 把默认选项渲染成稳定的 k=v 列表（按键排序）。
func renderOptions(opts map[string]any) string {
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, opts[k]))
	}
	return strings.Join(parts, " ")
}

// orDash 把空串渲染为 `-`（表格里比空白更容易看出"没有"）。
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// writeJSON 输出确定格式的 JSON（2 空格缩进、LF、末尾换行）。
func writeJSON(stdout, stderr io.Writer, v any) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "marshal json: %v\n", err)
		return 1
	}
	if _, werr := stdout.Write(append(data, '\n')); werr != nil {
		fmt.Fprintf(stderr, "write json: %v\n", werr)
		return 1
	}
	return 0
}
