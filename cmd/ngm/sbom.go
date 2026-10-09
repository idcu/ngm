package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/lock"
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

	data, err := sbom.Build(
		sbom.Project{Name: pf.Name, Version: pf.Version},
		entries,
		sbom.Options{
			Format:      format,
			Now:         time.Now(),
			ToolVersion: version.Version,
		})
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// `--out=`（显式空值）与"没给 --out"等价：都走 stdout——
	// v0.42 的不变量是"两者要么等价、要么失败"，不允许"都成功而结果不同"。
	if out := strings.TrimSpace(*outFlag); out != "" {
		if werr := os.WriteFile(out, data, 0o644); werr != nil {
			return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
				"write the SBOM to "+out,
				"check that the target directory exists and is writable", werr))
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
