package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/atomicfile"
	"github.com/idcu/ngm/internal/core/errs"
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

EXIT CODES:
  0  the project files were created
  3  configuration error: --runtime is not node|deno, the target already holds an
     ngm.json, or a file could not be written
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
	fs := newFlagSet("init", stderr)
	runtime := fs.String("runtime", "node", "project runtime (node|deno)")
	dir := fs.String("dir", ".", "target directory")
	force := fs.Bool("force", false, "overwrite existing ngm.json")

	c := newCmdContextNoJSON(ctx, args, stdout, stderr, fs)
	c.usage(initUsage)
	if !c.parse([]flagSpec{
		{Name: "runtime"}, {Name: "dir"}, {Name: "force", Bool: true},
	}, nil) {
		return 3
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, initUsage)
		return 3
	}
	name := strings.TrimSpace(fs.Arg(0))
	if name == "" {
		// 一个空名字会写出一份**自己拒绝读取**的 ngm.json：init 成功，
		// 随后 `ngm doctor` / `ngm install` 报 "name is required"。
		// 这个错误必须在写盘之前以 init 的身份说出来。
		return c.fail(errs.New(errs.CodeConfigInvalid,
			"a project name is required",
			"pass a name like `github.com:my-org/app` as the first argument"))
	}

	if !validRuntimeChoice(*runtime) {
		return c.fail(errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("invalid --runtime=%q", *runtime),
			"choose one of: node, deno",
		))
	}

	target, err := filepath.Abs(*dir)
	if err != nil {
		return c.fail(errs.Wrap(errs.CodeConfigInvalid, "resolve target dir",
			"pass an absolute --dir, or run ngm from the directory you mean", err))
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return c.fail(errs.Wrap(errs.CodeConfigInvalid, "create target dir",
			"check that the parent directory exists and is writable (a read-only filesystem "+
				"or a missing drive letter looks like this)", err))
	}

	cfgPath := filepath.Join(target, "ngm.json")
	if _, err := os.Stat(cfgPath); err == nil && !*force {
		return c.fail(errs.New(
			errs.CodeConfigInvalid,
			cfgPath+" already exists",
			"pass --force to overwrite, or remove the file manually",
		))
	}

	// 入口文件：**只在不存在时**写。用户已经写好的源码不该被脚手架覆盖，
	// 这一点与 --force 无关（--force 的语义是"覆盖 ngm.json"，不是"覆盖源码"）。
	entryPath := filepath.Join(target, filepath.FromSlash(initEntryRel))
	// 在写 ngm.json **之前**预检入口形状：一个同名的目录会让 main 指向目录，
	// init 却打印 "kept ... (already exists)" 并退 0——下一条 `ngm build` 才失败。
	// 也避免"失败后仍留下一份新建的 ngm.json"。
	if st, err := os.Stat(entryPath); err == nil && !st.Mode().IsRegular() {
		return c.fail(errs.New(
			errs.CodeConfigInvalid,
			entryPath+" exists but is not a regular file",
			"remove it or choose another --dir; init will not write an ngm.json whose `main` points at a directory"))
	}

	// 与 add / remove 走**同一个**写回入口：格式化、LF、原子写、写前校验
	// 都只有一份实现。此前 init 自己 MarshalIndent 再 atomicfile.Write，
	// 于是"模板是否合法"没有任何判据——`ngm init ""` 会写出一份
	// 后续所有命令都拒绝读取的 ngm.json。
	if werr := config.WriteProjectFile(cfgPath, initProjectFile(name, config.Runtime(*runtime))); werr != nil {
		return c.fail(werr)
	}
	fmt.Fprintf(stdout, "created %s\n", cfgPath)

	if _, err := os.Stat(entryPath); err == nil {
		fmt.Fprintf(stdout, "kept    %s (already exists)\n", entryPath)
		return 0
	}
	if err := os.MkdirAll(filepath.Dir(entryPath), 0o755); err != nil {
		return c.fail(errs.Wrap(errs.CodeConfigInvalid, "create src dir",
			"check that the project directory is writable", err))
	}
	if err := atomicfile.Write(entryPath, []byte(initEntryStub)); err != nil {
		return c.fail(errs.Wrap(errs.CodeConfigInvalid, "write entry file",
			"check that the src directory is writable; ngm.json was written, the entry file was not", err))
	}
	fmt.Fprintf(stdout, "created %s\n", entryPath)
	return 0
}

func validRuntimeChoice(s string) bool {
	return s == "node" || s == "deno"
}

// initProjectFile 生成最小 ngm.json 模板：仅填写必要字段，依赖数组留空。
//
// 关键点：
//   - 不再自己序列化：交给 [config.WriteProjectFile]（确定格式、LF、原子写、
//     写前 Validate 都只有一份实现）
//   - 不在模板中预填 dependencies —— 用户应通过 `ngm add` 添加
//   - `main` 指向 init 生成的入口文件，使 `ngm build` 无需参数即可工作
func initProjectFile(name string, runtime config.Runtime) *config.ProjectFile {
	return &config.ProjectFile{
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
}
