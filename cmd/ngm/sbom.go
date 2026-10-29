package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/core/atomicfile"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/sbom"
	"github.com/idcu/ngm/internal/version"
)

const sbomUsage = `ngm sbom — export a software bill of materials (CycloneDX / SPDX)

USAGE:
  ngm sbom [--format=cyclonedx|spdx] [--out=<file>] [--dir=<dir>]

FLAGS:
  --format   output format: cyclonedx (default) or spdx
  --out      write the document to this file instead of stdout
  --dir      project directory containing ngm.lock (default: .)

WHAT IT EXPORTS:
  Every dependency pinned in ngm.lock, each with its commit, its archiveDigest
  (SHA-256 over the canonical file listing, ADR-008) and a git purl.
  It reads the lock only: no network, no re-resolution, no writes to the project.

WHAT IT DOES NOT CLAIM:
  Not build provenance ("who built what" is SLSA's axis, orthogonal to ngm) and
  not a licence inventory - upstream licences are unknown to ngm, so SPDX output
  says NOASSERTION rather than guessing.

EXIT CODES:
  0  the document was written
  3  configuration error (no ngm.json, no ngm.lock, unknown --format, unwritable --out)
  6  internal failure (the document could not be written to stdout)
`

// runSbom 处理 `ngm sbom`（v0.69）。
//
// 定位（评估报告第 3 层）：合规/审计场景是这类工具唯一的付费场景，
// 而它的入场券是一份**别人能读**的清单——lock 里已经有 commit + digest +
// 来源链，导出几乎是免费的。本命令因此只有一处输入（ngm.lock）：
// 不触网、不重解析、不写项目。
func runSbom(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("sbom", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	formatFlag := fs.String("format", string(sbom.FormatCycloneDX), "output format (cyclonedx|spdx)")
	outFlag := fs.String("out", "", "write to this file instead of stdout")
	fs.Usage = func() { fmt.Fprint(stderr, sbomUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "format"}, {Name: "out"},
	})); err != nil {
		return 3
	}
	if fs.NArg() > 0 {
		// 这里**不加** `failEarly`：`sbom` 不接受 `--json`——它的 stdout 本身就是
		// 机器可读的文档（CycloneDX / SPDX）。`ngm sbom --json` 会在 `fs.Parse`
		// 那一支失败，而按 v0.16 规则④，"命令不接受 --json 时 stdout 仍为空"
		// 是**正确**的行为。加一行永远走不到的调用只会是死代码。
		fmt.Fprint(stderr, sbomUsage)
		return 3
	}

	format, ferr := sbom.ParseFormat(*formatFlag)
	if ferr != nil {
		return runErr(ctx, stdout, stderr, ferr)
	}

	env, err := newProjectEnv(*dirFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	pf, err := env.ReadManifest()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	lf, err := lock.Read(env.LockPath())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if lf == nil {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			"no ngm.lock found at "+env.LockPath(),
			"run `ngm install` to resolve dependencies and create the lock"))
	}

	entries, err := sbom.FromLock(lf)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// v0.81：过期的 lock 必须**说出来**。
	//
	// 此前 sbom / attest 都只读 lock，不做 install 那道 `lockManifestMismatch`，
	// 于是 ngm.json 改过之后照样能导出一份"看起来正常"的 SBOM——
	// 而 attest 那份是要被**签名**的。签名之后最难被发现的正是这一类错误：
	// 结构合法、哈希正确，描述的却是一个已经不存在的状态。
	if stale := lockManifestMismatch(lf, toDepSpecs(pf.Dependencies)); len(stale) > 0 {
		fmt.Fprintf(stderr,
			"warning: ngm.lock is out of date with ngm.json (%s); "+
				"this export describes the lock, not the manifest — run `ngm install` first\n",
			strings.Join(stale, "; "))
	}

	// v0.81：图不完整必须**说出来**，并且不导出半张图。
	// 旧 lock、或被手改过的 lock 会缺少某些条目的 `requiredBy`——
	// 那时"导出一份看起来完整的依赖树"比"不导出"更糟：前者是错的事实。
	graphComplete := lf.HasGraph()
	if !graphComplete {
		fmt.Fprintln(stderr,
			"note: this lock does not carry a complete dependency graph; "+
				"the dependency section is omitted — run `ngm install` to record it")
	}

	data, err := sbom.Build(
		sbom.Project{Name: pf.Name, Version: pf.Version},
		entries,
		sbom.Options{
			Format:        format,
			Now:           time.Now(),
			ToolVersion:   version.Version,
			GraphComplete: graphComplete,
		})
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// `--out=`（显式空值）与"没给 --out"等价：都走 stdout——
	// v0.42 的不变量是"两者要么等价、要么失败"，不允许"都成功而结果不同"。
	if out := strings.TrimSpace(*outFlag); out != "" {
		// v0.82：走与 lock / ngm.json / mappings 同一条原子写
		// （[atomicfile.Write]）。此前是裸 `os.WriteFile`：写到一半被中断
		// 会留下一个**截断的 JSON**，而它名字是对的、mtime 是新的，
		// 看文件的人没有理由怀疑它。
		if werr := atomicfile.Write(out, data); werr != nil {
			return runErr(ctx, stdout, stderr, werr)
		}
		fmt.Fprintf(stdout, "wrote %s (%d component(s), %s)\n", out, len(entries), format)
		return 0
	}

	if _, werr := stdout.Write(data); werr != nil {
		fmt.Fprintf(stderr, "write report: %v\n", werr)
		return errs.CodeInternal.ExitCode()
	}
	return 0
}
