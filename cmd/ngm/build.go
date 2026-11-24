package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/core/atomicfile"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/mappings"
	"github.com/idcu/ngm/internal/receipt"
)

const buildUsage = `ngm build — bundle a project with an external engine

USAGE:
  ngm build [<entry>] [--engine=<name>] [--outfile=<path>] [--production]
             [--dry-run] [--dir=<dir>] [--provenance] [--cache]

FLAGS:
  --engine=<name>   engine to use (default: engines.bundle in ngm.json, else esbuild)
  --outfile=<path>  write the bundle there (default: stdout, exactly like running esbuild)
  --production      minify, and define process.env.NODE_ENV="production"
  --dry-run         print the resolved command and exit without spawning anything
                    (no engine is executed, and no version is probed)
  --dir=<dir>       project directory (default: .)
  --provenance      write a deterministic side-car ngm.build.json next to ngm.lock.
                    The receipt records input/engine/argv/mappings/output digests;
                    it is NOT part of ngm.lock, has no timestamps, and is not a
                    reproducible-build certification (ADR-057). Off by default.
  --cache           reuse the local ~/.ngm/build-cache for identical inputs.
                    Implies --provenance; a hit is reported as "source: cache" in
                    the receipt, a rebuild as "source: built". A tampered cache
                    entry is invalidated and rebuilt once; if the rebuilt bytes
                    still disagree, the command exits 2.

WHAT NGM ADDS:
  Every entry in ngm.mappings.json becomes --alias:<from>=<to>, so
  ` + "`import x from \"github:org/repo\"`" + ` resolves into ngm.vendor — the bytes
  that ngm proved. Other than that this is a thin wrapper around the engine.

EXIT CODES:
  0  built (also for --dry-run)
  1  the engine ran and failed (its own stderr is preserved)
  2  integrity failure: a polluted cache could not be repaired by one rebuild
  3  configuration error (no entry, unknown engine name)
  5  no usable engine (not installed, or a dry-run stub)
	6  internal failure: the conclusion was reached but could not be delivered (writing stdout, encoding JSON, reading stdin), or ngm panicked
`

// runBuild 处理 `ngm build`。
func runBuild(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("build", stderr)
	engineFlag := fs.String("engine", "", "engine name")
	outfile := fs.String("outfile", "", "output file")
	production := fs.Bool("production", false, "production build")
	dryRun := fs.Bool("dry-run", false, "print the resolved command and exit")
	dirFlag := fs.String("dir", ".", "project directory")
	provenance := fs.Bool("provenance", false, "write a deterministic ngm.build.json side-car")
	useCache := fs.Bool("cache", false, "reuse the local build cache (implies --provenance)")

	c := newCmdContextNoJSON(ctx, args, stdout, stderr, fs)
	c.usage(buildUsage)
	if !c.parse([]flagSpec{
		{Name: "engine"},
		{Name: "outfile"},
		{Name: "production", Bool: true},
		{Name: "dry-run", Bool: true},
		{Name: "dir"},
		{Name: "provenance", Bool: true},
		{Name: "cache", Bool: true},
	}, nil) {
		return 3
	}
	if fs.NArg() > 1 {
		fmt.Fprint(stderr, buildUsage)
		return 3
	}

	ec, err := newEngineContext(*dirFlag, stderr)
	if err != nil {
		return c.fail(err)
	}

	entry := fs.Arg(0)
	if entry == "" {
		entry = ec.pf.Main
	}
	if strings.TrimSpace(entry) == "" {
		return c.fail(errs.New(
			errs.CodeConfigInvalid,
			"no entry file",
			"pass one (`ngm build src/index.ts`) or set `main` in ngm.json"))
	}

	sel, err := ec.selectionFor(adapter.KindBundle, *engineFlag)
	if err != nil {
		return c.fail(err)
	}

	alias, err := ec.mappingsAlias()
	if err != nil {
		return c.fail(err)
	}

	opts := adapter.BundleOptions{
		Outfile:    *outfile,
		Alias:      alias,
		Production: *production,
	}

	if *dryRun {
		plans, perr := ec.runner.Plans(adapter.KindBundle, sel, opts, entry)
		if perr != nil {
			return c.fail(perr)
		}
		fmt.Fprintf(stdout, "would bundle %s\n", entry)
		printPlans(stdout, plans)
		return 0
	}

	wantReceipt := *provenance || *useCache
	var rec *receipt.Receipt
	var cacheKey string
	if wantReceipt {
		rec, cacheKey, err = buildReceiptBase(ec, sel, opts, entry)
		if err != nil {
			return c.fail(err)
		}
		rec.Source = receipt.SourceBuilt
	}

	var berr error
	var res *adapter.BundleResult
	var staleExpected string
	if *useCache {
		cacheDir := buildCacheDir()
		cached, rerr := receipt.ReadCache(cacheDir, cacheKey)
		if rerr != nil {
			return c.fail(errs.Wrap(errs.CodeInternal, "read build cache",
				"delete ~/.ngm/build-cache and retry", rerr))
		}
		if cached != nil && cached.ExpectedDigest != "" && cached.ActualDigest == cached.ExpectedDigest {
			// 命中：不启动引擎，直接把缓存字节当作产物。
			res = &adapter.BundleResult{Code: cached.Data}
			if opts.Outfile != "" {
				res.Outfile = opts.Outfile
				if werr := writeCachedArtifact(ec.env.ProjectDir, opts.Outfile, cached.Data); werr != nil {
					return c.fail(werr)
				}
			}
			rec.Source = receipt.SourceCache
		} else if cached != nil {
			// 污染：记下写入时的摘要，重建一次后仍不符就 exit 2。
			staleExpected = cached.ExpectedDigest
			if rerr := receipt.RemoveCache(cacheDir, cacheKey); rerr != nil {
				return c.fail(errs.Wrap(errs.CodeInternal, "invalidate polluted build cache",
					"delete ~/.ngm/build-cache and retry", rerr))
			}
		}
	}
	if res == nil {
		res, berr = ec.runner.Bundle(ctx, sel, entry, opts)
		if berr != nil {
			return c.fail(berr)
		}
	}

	// 引擎的诊断原样转发到 stderr（不转述、不吞掉）
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, w)
	}
	// ngm 自己的说明另起一行并加前缀：用户要能分清"引擎在报警"与"ngm 在解释自己做的事"。
	// 这条通道此前是**断的**：`deno bundle` 的实验性提示写好了却没人读——
	// 而那句提示恰恰是"我们没说过它稳定"的全部依据。
	for _, n := range res.Notes {
		fmt.Fprintf(stderr, "note: %s\n", n)
	}
	if res.Outfile != "" {
		fmt.Fprintf(stdout, "bundled %s → %s\n", entry, res.Outfile)
	} else {
		if _, werr := stdout.Write(res.Code); werr != nil {
			fmt.Fprintf(stderr, "write bundle: %v\n", werr)
			return errs.CodeInternal.ExitCode()
		}
	}

	if wantReceipt {
		outData, outPath, derr := artifactBytes(ec.env.ProjectDir, res, opts.Outfile)
		if derr != nil {
			return c.fail(derr)
		}
		rec.Output = receipt.Output{Path: outPath, Digest: receipt.Digest(outData)}
		if staleExpected != "" && rec.Output.Digest != staleExpected {
			return c.fail(errs.New(errs.CodeDigestMismatch,
				"rebuilt artifact does not match the cached digest",
				"the engine produced different bytes after cache invalidation; "+
					"delete ~/.ngm/build-cache and rerun `ngm build --provenance`"))
		}
		if *useCache && rec.Source == receipt.SourceBuilt {
			if werr := receipt.WriteCache(buildCacheDir(), cacheKey,
				outData, rec.Output.Digest); werr != nil {
				return c.fail(errs.Wrap(errs.CodeInternal, "write build cache",
					"delete ~/.ngm/build-cache and retry", werr))
			}
		}
		if werr := receipt.Write(filepath.Join(ec.env.ProjectDir, receipt.FileName), rec); werr != nil {
			return c.fail(errs.Wrap(errs.CodeInternal, "write "+receipt.FileName,
				"check that the project directory is writable", werr))
		}
		fmt.Fprintf(stderr, "wrote %s (source: %s)\n", receipt.FileName, rec.Source)
	}
	return 0
}

// buildReceiptBase 收集回执里除 output 之外的字段，并返回缓存键。
//
// 所有输入都来自只读探测：没有 spawn 引擎、没有改写 ngm.lock、没有时间戳。
func buildReceiptBase(ec *engineContext, sel adapter.Selection, opts adapter.BundleOptions, entry string) (*receipt.Receipt, string, error) {
	chain, err := ec.runner.Resolve(adapter.KindBundle, sel)
	if err != nil {
		return nil, "", err
	}
	var chosen, firstUnavailable *adapter.Entry
	for i := range chain {
		eng, eerr := adapter.NewEngine(chain[i], ec.env.ProjectDir)
		if eerr == nil && eng.Available() {
			chosen = &chain[i]
			break
		}
		if firstUnavailable == nil {
			firstUnavailable = &chain[i]
		}
	}
	if chosen == nil {
		// 用同一套 ToolDigest 错误给出"缺哪个工具"的精确 hint，
		// 而不是只说"引擎不可用"让用户自己猜命令里写的是什么。
		if firstUnavailable != nil && firstUnavailable.Adapter == adapter.AdapterSubprocess {
			if _, err := adapter.ToolDigest(firstUnavailable.Program); err != nil {
				return nil, "", err
			}
		}
		return nil, "", errs.New(errs.CodeEngineNotFound,
			"no usable bundle engine: "+strings.Join(sel.Chain(), ", "),
			"install the engine tool, or point `command` in ngm.engines.json at an installed executable "+
				"(run `ngm build --dry-run` to see the resolved command)")
	}

	engineDigest, derr := engineDigestFor(ec.env.ProjectDir, *chosen)
	if derr != nil {
		return nil, "", derr
	}

	inputDigest, derr := receipt.DigestFile(resolveProjectPath(ec.env.ProjectDir, entry))
	if derr != nil {
		return nil, "", errs.Wrap(errs.CodeConfigInvalid, "read build input "+entry,
			"the path is resolved relative to the project directory", derr)
	}

	plans, perr := ec.runner.Plans(adapter.KindBundle, sel, opts, entry)
	if perr != nil {
		return nil, "", perr
	}
	var argv []string
	for _, p := range plans {
		if p.Engine == chosen.Name {
			argv = append([]string(nil), p.Args...)
			break
		}
	}
	sort.Strings(argv) // 沿用 subprocess adapter 的按键排序约定（spec §3.1）

	mappingsDigest, derr := mappingsDigestFor(ec.env.ProjectDir)
	if derr != nil {
		return nil, "", derr
	}

	rec := &receipt.Receipt{
		ReceiptVersion: receipt.Version,
		Inputs:         []receipt.Input{{Path: receiptInputPath(ec.env.ProjectDir, entry), Digest: inputDigest}},
		Engine: receipt.Engine{
			Name:    chosen.Name,
			Kind:    string(chosen.Kind),
			Version: chosen.Version,
			Digest:  engineDigest,
		},
		Argv:           argv,
		MappingsDigest: mappingsDigest,
	}
	return rec, rec.CacheKey(buildPlatformTriple()), nil
}

// engineDigestFor 计算"工具字节"摘要：subprocess 走 LookPath，wasm/embed 读实际文件。
func engineDigestFor(projectDir string, e adapter.Entry) (string, error) {
	switch e.Adapter {
	case adapter.AdapterSubprocess:
		return adapter.ToolDigest(e.Program)
	case adapter.AdapterWasm:
		return adapter.FileDigest(projectDir, e.Program)
	case adapter.AdapterEmbed:
		self, err := os.Executable()
		if err != nil {
			return "", errs.Wrap(errs.CodeInternal, "locate running executable",
				"this is an internal error; report it", err)
		}
		return adapter.FileDigest("", self)
	default:
		return "", errs.New(errs.CodeEngineNotFound,
			fmt.Sprintf("cannot digest engine %q with adapter %q", e.Name, e.Adapter),
			"declare a subprocess or wasm engine with a local tool file")
	}
}

// mappingsDigestFor 对 mappings 文件字节取摘要；没有文件时用固定空摘要，
// 这样"有/无 mappings"也是键的一个明确取值，而不是被省略。
func mappingsDigestFor(projectDir string) (string, error) {
	path := mappings.Find(projectDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return receipt.Digest(nil), nil
	}
	if err != nil {
		return "", errs.Wrap(errs.CodeConfigInvalid, "read "+mappings.FileName,
			"the mappings file is unreadable; run `ngm mappings validate`", err)
	}
	return receipt.Digest(data), nil
}

// artifactBytes 从落盘产物或 stdout 字节中取出摘要对象，并给出相对/稳定路径。
func artifactBytes(projectDir string, res *adapter.BundleResult, requestedOutfile string) ([]byte, string, error) {
	if requestedOutfile != "" {
		path := res.Outfile
		if path == "" {
			path = requestedOutfile
		}
		data, err := os.ReadFile(resolveProjectPath(projectDir, path))
		if err != nil {
			return nil, "", errs.Wrap(errs.CodeInternal, "read built artifact "+path,
				"the engine exited 0 but the output file is unreadable", err)
		}
		return data, filepath.ToSlash(path), nil
	}
	return res.Code, receipt.StdoutPath, nil
}

func resolveProjectPath(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

// receiptInputPath 不把绝对路径写进回执：优先记相对项目目录的路径，
// 逃逸出项目时退化成 basename（摘要仍指向真实读取的字节）。
func receiptInputPath(projectDir, entry string) string {
	if !filepath.IsAbs(entry) {
		return filepath.ToSlash(entry)
	}
	if rel, err := filepath.Rel(projectDir, entry); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(entry)
}

func writeCachedArtifact(projectDir, outfile string, data []byte) error {
	path := resolveProjectPath(projectDir, outfile)
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return errs.Wrap(errs.CodeInternal, "create output directory "+dir,
				"check that the project directory is writable", err)
		}
	}
	if err := atomicfile.Write(path, data); err != nil {
		return errs.Wrap(errs.CodeInternal, "write cached artifact "+outfile,
			"check that the project directory is writable", err)
	}
	return nil
}

func buildPlatformTriple() string {
	return runtime.GOOS + "-" + runtime.GOARCH + "-" + runtime.Compiler
}
