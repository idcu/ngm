package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
)

// initUsage 是 `ngm init --help` 的简短说明。
const initUsage = `ngm init — initialize a new project

USAGE:
  ngm init <name> [--runtime=node|deno] [--dir=<path>]

FLAGS:
  --runtime   "node" (default) or "deno"
  --dir       target directory; defaults to current directory
  --force     overwrite an existing ngm.json

WRITES:
  <dir>/ngm.json       project config (LF, 2-space indent, fixed field order)
  <dir>/src/index.ts   a minimal entry file, only when it does not exist yet

  The generated ngm.json points "main" at that entry, so the quickstart flow
  (init -> add -> install -> verify -> build) works without extra arguments.
  An existing src/index.ts is never overwritten, not even with --force.
`

// initEntryRel 是 `ngm init` 生成的入口文件（相对项目根）。
const initEntryRel = "src/index.ts"

// initEntryStub 是入口文件的内容：既能被 esbuild 打包，也能被 node 直接运行。
//
// 写成"有类型标注 + 会打印"的两用形态是刻意的：`ngm build && node dist/x.js`
// 是用户验证整条链路最短的路径，而类型标注让 esbuild 的处理结果可观测。
const initEntryStub = `export const hello = (who: string): string => "hello " + who;

console.log(hello("ngm"));
`

// runInit 处理 `ngm init <name>` 命令。
//
// 行为：
//  1. 解析 --runtime / --dir / --force
//  2. 若目标已有 ngm.json 且无 --force → exit 3
//  3. 生成模板 ngm.json（schemaVersion=1，最小依赖列表，main 指向入口文件）
//  4. 生成 src/index.ts（**仅当不存在**，任何情况下都不覆盖用户代码）
//  5. 在 stdout 报告已生成 / 已保留的文件
func runInit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("init")
	runtime := fs.String("runtime", "node", "project runtime (node|deno)")
	dir := fs.String("dir", ".", "target directory")
	force := fs.Bool("force", false, "overwrite existing ngm.json")
	fs.Usage = func() { fmt.Fprint(stderr, initUsage) }
	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "runtime"}, {Name: "dir"}, {Name: "force", Bool: true},
	})); err != nil {
		return 3
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, initUsage)
		return 3
	}
	name := fs.Arg(0)

	if !validRuntimeChoice(*runtime) {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("invalid --runtime=%q", *runtime),
			"choose one of: node, deno",
		))
	}

	target, err := filepath.Abs(*dir)
	if err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "resolve target dir", "", err))
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "create target dir", "", err))
	}

	cfgPath := filepath.Join(target, "ngm.json")
	if _, err := os.Stat(cfgPath); err == nil && !*force {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			cfgPath+" already exists",
			"pass --force to overwrite, or remove the file manually",
		))
	}

	tmpl := renderInitTemplate(name, config.Runtime(*runtime))
	if err := os.WriteFile(cfgPath, tmpl, 0o644); err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "write ngm.json", "", err))
	}
	fmt.Fprintf(stdout, "created %s\n", cfgPath)

	// 入口文件：**只在不存在时**写。用户已经写好的源码不该被脚手架覆盖，
	// 这一点与 --force 无关（--force 的语义是"覆盖 ngm.json"，不是"覆盖源码"）。
	entryPath := filepath.Join(target, filepath.FromSlash(initEntryRel))
	if _, err := os.Stat(entryPath); err == nil {
		fmt.Fprintf(stdout, "kept    %s (already exists)\n", entryPath)
		return 0
	}
	if err := os.MkdirAll(filepath.Dir(entryPath), 0o755); err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "create src dir", "", err))
	}
	if err := os.WriteFile(entryPath, []byte(initEntryStub), 0o644); err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "write entry file", "", err))
	}
	fmt.Fprintf(stdout, "created %s\n", entryPath)
	return 0
}

func validRuntimeChoice(s string) bool {
	return s == "node" || s == "deno"
}

// renderInitTemplate 生成最小 ngm.json 模板：仅填写必要字段，依赖数组留空。
//
// 关键点：
//   - 字段顺序固定（与 v0.1 lock 序列化纪律一致）；输出末尾带 LF
//   - 缩进 2 空格（与 guides/configuration.md 示例一致）
//   - 不在模板中预填 dependencies —— 用户应通过 `ngm add` 添加
//   - `main` 指向 init 生成的入口文件，使 `ngm build` 无需参数即可工作
func renderInitTemplate(name string, runtime config.Runtime) []byte {
	p := config.ProjectFile{
		SchemaVersion: config.SchemaVersion,
		Name:          name,
		Version:       "0.1.0",
		Runtime:       runtime,
		Main:          "./" + initEntryRel,
		Dependencies:  []config.Dependency{}, // 显式空数组而非 nil，确保输出稳定
		Engines: &config.EnginesConfig{
			Transform: "esbuild",
			Bundle:    "esbuild",
		},
		Vendor: &config.VendorConfig{
			Mode:     "local",
			LinkMode: "auto",
		},
	}
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		// marshal 一个静态 struct 不会失败；保留为保险
		panic(err)
	}
	// 确保 LF 行尾，与"全部锁文件与清单统一 LF"对齐
	out = append(out, '\n')
	return out
}
