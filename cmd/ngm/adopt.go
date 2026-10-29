package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"

	"github.com/idcu/ngm/internal/adopt"
	"github.com/idcu/ngm/internal/core/atomicfile"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/version"
)

const adoptUsage = `ngm adopt — describe the dependencies already installed, as content evidence

USAGE:
  ngm adopt [--dir=<dir>] [--out=<file>] [--json]

FLAGS:
  --dir    project directory containing node_modules (default: .)
  --out    write the proof here instead of ./ngm.proof.json
  --json   write the proof to stdout and touch no file (mutually exclusive with
           an explicit --out: both name the same destination)

WHAT IT DOES:
  Walks node_modules, and for every installed package computes a content digest
  in **ngm's own manifest format** (ADR-008: NUL-separated records, sorted by
  UTF-8 byte order, sha256 over the listing). Those digests are byte-compatible
  with the archiveDigest that ngm install/verify maintain, so both sides can be
  compared directly.

  Provenance comes from package-lock.json when it exists (npm lockfile v2/v3):
  a git dependency resolved to a 40-char commit is recorded as commit, a git
  fragment that is a tag or branch is recorded as ref (not as a commit - a ref
  cannot be replayed), and everything else as registry integrity.

WHAT IT DOES NOT DO:
  It never installs, never writes node_modules, never rewrites a lock file, and
  never claims a package is safe: a digest answers "are these the same bytes",
  not "is this code benign".
  It does not parse pnpm-lock.yaml or yarn.lock (no YAML parser is vendored).
  When that is the case the proof says so in notes and coverage.unknown, rather
  than silently reporting every package as having no provenance.

EXIT CODES:
  0  the proof was produced
  3  configuration error (no node_modules, empty node_modules, unwritable --out)
  6  internal failure (the proof could not be written to stdout)
`

// runAdopt 处理 `ngm adopt`（v0.105）。
//
// 定位（本轮的结论）：ngm 的资产是"内容 ↔ commit"的可证明性，而它此前只对
// **自己装出来的树**说话。真实项目的依赖是 pnpm/npm 装的，于是采用率的硬门槛
// 变成了"先换安装器"。本命令把门槛降成一条只读命令：不接管安装，
// 只对**磁盘上已有的东西**出具证据。
func runAdopt(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("adopt", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	outFlag := fs.String("out", adopt.FileName, "write the proof to this file")
	jsonOut := fs.Bool("json", false, "write the proof to stdout instead of a file")
	fs.Usage = func() { fmt.Fprint(stderr, adoptUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "out"}, {Name: "json", Bool: true},
	})); err != nil {
		// v0.54 的规矩对**每一个**接受 --json 的命令成立：解析失败时若用户提了
		// `--json`，机器那侧要拿到一份信封，而不是 0 字节的 stdout。
		failEarly(args, stdout, err.Error(),
			"run `ngm adopt --help` for the flags it accepts")
		return 3
	}
	ctx = markJSONIfRequested(ctx, *jsonOut)
	if fs.NArg() > 0 {
		failEarly(args, stdout, "unexpected argument "+strconv.Quote(fs.Arg(0)),
			"`ngm adopt` takes no positional arguments; it describes the directory in --dir")
		fmt.Fprint(stderr, adoptUsage)
		return 3
	}

	// `--out` 与 `--json` 是**同一个目的地**的两种写法（文件 / stdout）。
	// 只在其一被显式给出时才谈互斥：`--out` 有默认值，"没给"与"给了默认值"
	// 必须可区分（v0.41 的教训）。
	outSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "out" {
			outSet = true
		}
	})
	if outSet && *jsonOut {
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"--out and --json are two spellings of the same destination",
			"keep one: --json for stdout, or --out=<file> for a file"))
	}
	if outSet && *outFlag == "" {
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"`--out` was given an empty value",
			fmt.Sprintf("omit it to use %s, or name a file", adopt.FileName)))
	}

	proof, aerr := adopt.Run(adopt.Options{Dir: *dirFlag, ToolVersion: version.Version})
	if aerr != nil {
		return runErr(ctx, stdout, stderr, aerr)
	}
	data, merr := proof.Marshal()
	if merr != nil {
		return runErr(ctx, stdout, stderr, merr)
	}

	if *jsonOut {
		if _, werr := stdout.Write(data); werr != nil {
			fmt.Fprintf(stderr, "write proof: %v\n", werr)
			return errs.CodeInternal.ExitCode()
		}
		return 0
	}

	target := *outFlag
	if !filepath.IsAbs(target) {
		target = filepath.Join(*dirFlag, target)
	}
	if werr := atomicfile.Write(target, data); werr != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
			"write the proof to "+target, "check free disk space and permissions", werr))
	}

	c := proof.Coverage
	fmt.Fprintf(stdout, "adopted %d package(s): %d with a git commit, %d with registry integrity, %d unknown\n",
		c.Packages, c.WithCommit, c.WithIntegrity, c.Unknown)
	fmt.Fprintf(stdout, "wrote %s (source: %s, coverage %d/%d traceable)\n",
		target, sourceLabel(proof), c.WithCommit, c.Packages)
	// "有未知来源"是这份证明最重要的一条读数，必须在人读通道里说出来——
	// 否则 42 个包里 3 个可追溯这件事，只有打开 JSON 才看得到。
	if c.Unknown > 0 {
		fmt.Fprintf(stdout, "note: %d package(s) have no traceable provenance; "+
			"see notes in the proof (content digests are still exact)\n", c.Unknown)
	}
	return 0
}

func sourceLabel(p *adopt.Proof) string {
	if p.Source.Lockfile == "" {
		return "no lock file"
	}
	return p.Source.Manager + " " + p.Source.Lockfile
}
