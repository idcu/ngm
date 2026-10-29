package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/idcu/ngm/internal/adopt"
	"github.com/idcu/ngm/internal/core/atomicfile"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/version"
)

const adoptUsage = `ngm adopt — describe the dependencies already installed, as content evidence

USAGE:
  ngm adopt [--dir=<dir>] [--out=<file>] [--json]
  ngm adopt --check=<file> [--dir=<dir>] [--json]

FLAGS:
  --dir    project directory containing node_modules (default: .)
  --out    write the proof here instead of ./ngm.proof.json
  --json   write the proof to stdout and touch no file (mutually exclusive with
           an explicit --out: both name the same destination)
  --check  compare the current tree against a proof written earlier, and write
           nothing. Prints what changed (content / missing / extra) and exits 2
           if anything did. This is what turns the snapshot into a baseline:
           run it in CI to answer "is the tree still the one we audited?"
           With --json the *current* proof goes to stdout and the change list
           goes to stderr (stdout must stay a single document).

WHY --check EXISTS:
  A proof is a snapshot; on its own it only says what was true once. Comparing
  is the cheap half of verification and the only half available to a project
  that never runs ngm install - the digests are already in ngm's own manifest
  format, so a re-scan plus a diff needs no lock and no network.

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
  0  the proof was produced, or --check found nothing changed
  2  --check found a change: a package's bytes differ from the proof, or a
     package appeared / disappeared
  3  configuration error (no node_modules, empty node_modules, unwritable --out,
     --check pointed at something that is not a proof)
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
	checkFlag := fs.String("check", "", "compare against a stored proof and write nothing")
	fs.Usage = func() { fmt.Fprint(stderr, adoptUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "out"}, {Name: "check"}, {Name: "json", Bool: true},
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
	// `--check` 是**只读**模式：它的退出码是结论，而"顺便写了一份新基线"
	// 会让失败的那次运行留下副作用——下一次再跑就"没有变化"了，
	// 于是真正的问题被自己的产物抹掉。要刷新基线就单独跑一次不带 --check 的。
	checkSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "check" {
			checkSet = true
		}
	})
	if checkSet && strings.TrimSpace(*checkFlag) == "" {
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"`--check` was given an empty value",
			fmt.Sprintf("name the proof to compare against, e.g. --check=%s", adopt.FileName)))
	}
	if checkSet && outSet {
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"--check is read-only and cannot be combined with --out",
			"run `ngm adopt` once to refresh the baseline, then `ngm adopt --check=...` to compare"))
	}

	proof, aerr := adopt.Run(adopt.Options{Dir: *dirFlag, ToolVersion: version.Version})
	if aerr != nil {
		return runErr(ctx, stdout, stderr, aerr)
	}

	if checkSet {
		return runAdoptCheck(ctx, *dirFlag, *checkFlag, proof, *jsonOut, stdout, stderr)
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

// runAdoptCheck 比较当前树与一份存下来的证明（`ngm adopt --check=…`）。
//
// 它是"零改动接入"这条路的**后半段**：没有它，`ngm adopt` 写出的证明只能在
// 写下的那一刻有意义——一份没人再读的快照，和一个从不复核的审计结论是同一件事。
// 复核不需要 lock、不需要网络、也不需要 ngm 装过任何东西：摘要本来就在
// ngm 自己的清单格式里。
func runAdoptCheck(ctx context.Context, dir, proofPath string, cur *adopt.Proof,
	jsonOut bool, stdout, stderr io.Writer) int {
	target := proofPath
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	raw, rerr := os.ReadFile(target)
	if rerr != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
			"read the proof at "+target,
			"pass the file `ngm adopt` wrote (default: "+adopt.FileName+")", rerr))
	}
	old, perr := adopt.ParseProof(raw)
	if perr != nil {
		return runErr(ctx, stdout, stderr, perr)
	}
	changes := adopt.Compare(old, cur)

	// `--json` 时 stdout 必须是**单个文档**，因此差异走 stderr——
	// 与 `verify --json` 把沙箱/签名段落送去 stderr 是同一条规矩：
	// 那份输出标着"机器读这个"，混进人读文本就等于毁约。
	human := stdout
	if jsonOut {
		human = stderr
		data, merr := cur.Marshal()
		if merr != nil {
			return runErr(ctx, stdout, stderr, merr)
		}
		if _, werr := stdout.Write(data); werr != nil {
			fmt.Fprintf(stderr, "write proof: %v\n", werr)
			return errs.CodeInternal.ExitCode()
		}
	}

	if len(changes) == 0 {
		fmt.Fprintf(human, "unchanged: %d package(s) match the proof in %s\n",
			len(cur.Packages), proofPath)
		return 0
	}

	for _, c := range changes {
		switch c.Kind {
		case adopt.ChangeChanged:
			fmt.Fprintf(human, "✗ %s — content changed\n    was: %s\n    now: %s\n", c.Path, c.Want, c.Got)
		case adopt.ChangeMissing:
			fmt.Fprintf(human, "✗ %s — in the proof, gone now\n", c.Path)
		case adopt.ChangeExtra:
			fmt.Fprintf(human, "✗ %s — installed now, not in the proof\n", c.Path)
		}
	}
	fmt.Fprintf(human, "\n%d difference(s) against %s — this tree is not the one the proof describes\n",
		len(changes), proofPath)
	fmt.Fprintf(human, "  → if the change is intended, run `ngm adopt` again to record a new baseline\n")
	// 码 2 从**错误模型**取，而不是写字面量：`errs` 是退出码的唯一映射处
	// （ADR-026），而字面量 `return 2` 会让"这一处退的是哪个码"无法被机器核对
	// ——V57 当场就是这么抓到的。
	return errs.CodeDigestMismatch.ExitCode()
}

func sourceLabel(p *adopt.Proof) string {
	if p.Source.Lockfile == "" {
		return "no lock file"
	}
	return p.Source.Manager + " " + p.Source.Lockfile
}
