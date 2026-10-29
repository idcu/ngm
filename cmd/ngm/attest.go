package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/idcu/ngm/internal/attest"
	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/atomicfile"
	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/sbom"
	"github.com/idcu/ngm/internal/version"
)

const attestUsage = `ngm attest — emit an in-toto / SLSA v1 statement for ngm.lock

USAGE:
  ngm attest [--out=<file>] [--dir=<dir>]

FLAGS:
  --out   write the statement to this file instead of stdout
  --dir   project directory containing ngm.lock (default: .)

WHAT IT EMITS:
  One in-toto Statement v1 whose predicate is SLSA provenance v1, binding every
  pinned dependency to its archiveDigest and to the git commit it came from:
    subject    each git purl, sha256 = the canonical archive manifest (ADR-008)
    inputs     sha256 of ngm.json and of ngm.lock as committed
    materials  the pinned commit (gitCommit), plus the ref ngm.json declared
  It reads the lock only: no network, no re-resolution, no writes to the project.
  Same lock, same bytes - there is deliberately no timestamp and no random run id.

WHAT IT DOES NOT CLAIM:
  It is unsigned, and ngm does not invent a signing system (ADR-014): sign it with
  your own tooling (e.g. cosign attest-blob) so the claim becomes attributable.
  It also does not claim who built upstream, nor reproducibility, nor licences.
  It does NOT cover the dependency graph: v0.73 put the edges in ngm.lock, but
  ADR-028 §2.2 keeps them out of the statement - use 'ngm sbom' for the tree.
  Read the same lock, the two exports cover different dimensions.
  See docs/adr/adr-027-attestation.md.

EXIT CODES:
  0  the statement was written
  3  configuration error (no ngm.json, no ngm.lock, unwritable --out)
  6  internal failure (the statement could not be written to stdout)
`

// runAttest 处理 `ngm attest`（v0.70）。
//
// 定位（评估报告第 3 层第二条）：SBOM 是"清单"，attestation 是"可被签名的声明"。
// 两者读同一份输入（ngm.lock），产出同一批事实的两种标准形态——
// 本命令只多两样东西：in-toto 的信封，与"输入钉在字节上"的哈希。
func runAttest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("attest", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	outFlag := fs.String("out", "", "write to this file instead of stdout")
	fs.Usage = func() { fmt.Fprint(stderr, attestUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "out"},
	})); err != nil {
		return 3
	}
	if fs.NArg() > 0 {
		// 同 `sbom`：不接受 `--json`（stdout 就是 in-toto 语句本身），
		// 因此这里没有可给的信封，加 `failEarly` 会是死代码。
		fmt.Fprint(stderr, attestUsage)
		return 3
	}

	env, err := newProjectEnv(*dirFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	// ngm.json 是外部参数之一（它的哈希要写进语句），因此必须在场——
	// 与 sbom 同口径：空目录不是一个 ngm 项目，早失败比导出一份无主文档好。
	//
	// v0.81：保留读到的清单（此前丢弃返回值，只判断"在不在场"）。
	// 只有拿着它才能问"lock 与 ngm.json 是否自洽"——而这一问对一份
	// 要被签名的产物是必需的。
	// 输入的哈希 = **提交进 Git 的那份文件字节**（不是重新序列化后的规范形态）：
	// 外部参数要钉的是"你审过的那份文件"，重新序列化会掩盖手工编辑。
	//
	// v0.106：**只读一次**。此前这里先 `ReadManifest()`（open + decode）再
	// `os.ReadFile()`（又 open 一次），于是"语句描述的那份清单"与"被哈希进语句的
	// 那份字节"可能不是同一份文件——而这份语句是要被**签名**的，签完之后
	// 没有任何东西能发现这个窗口（v0.82 对 lock 做过同样的修，这里是它漏掉的另一半）。
	manifestBytes, rerr := os.ReadFile(env.ManifestPath())
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
				"ngm.json not found at "+env.ManifestPath(),
				"run `ngm init <name>` first to create a project"))
		}
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
			"read "+env.ManifestPath(), "check that the file is readable", rerr))
	}
	pf, err := config.ParseProjectFile(manifestBytes, env.ManifestPath())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	lockBytes, rerr := os.ReadFile(env.LockPath())
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return runErr(ctx, stdout, stderr, errs.New(
				errs.CodeConfigInvalid,
				"no ngm.lock found at "+env.LockPath(),
				"run `ngm install` to resolve dependencies and create the lock"))
		}
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
			"read "+env.LockPath(), "check that the file is readable", rerr))
	}

	// v0.82：解析与哈希用**同一份字节**（lock.Parse），不再读两次。
	//
	// 此前是 `lock.Read(path)` 一次、`os.ReadFile(path)` 再一次。两次之间
	// 文件若被改动，语句里的 subject（来自第一次）与 `lock.digest`
	// （来自第二次）会描述两份不同的文件。窗口极小，但对一份**要被签名的**
	// 证据，这属于不该存在的那一类：签名之后没有任何东西能发现它。
	lf, err := lock.Parse(lockBytes, env.LockPath())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if lf == nil {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			"no ngm.lock found at "+env.LockPath(),
			"run `ngm install` to resolve dependencies and create the lock"))
	}

	// v0.81：过期的 lock 必须**说出来**，而且这里比 sbom 更要紧——
	// 这份产物是要被**签名**的。签名之后最难被发现的正是这一类错误：
	// 结构合法、哈希正确，描述的却是一个已经不存在的状态
	// （ngm.json 改过、lock 没跟）。
	if stale := lockManifestMismatch(lf, toDepSpecs(pf.Dependencies)); len(stale) > 0 {
		fmt.Fprintf(stderr,
			"warning: ngm.lock is out of date with ngm.json (%s); "+
				"the statement describes the lock, not the manifest — "+
				"run `ngm install` before signing\n",
			strings.Join(stale, "; "))
	}

	entries, err := sbom.FromLock(lf)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	data, err := attest.Build(entries, attest.Options{
		ToolVersion:    version.Version,
		ManifestDigest: fileHexDigest(manifestBytes),
		LockDigest:     fileHexDigest(lockBytes),
	})
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// `--out=`（显式空值）与"没给 --out"等价：都走 stdout（v0.42 的不变量）。
	if out := strings.TrimSpace(*outFlag); out != "" {
		// v0.82：同 sbom —— 走 [atomicfile.Write] 而不是裸 os.WriteFile。
		// 这里的理由比 sbom 更强：这份文件是要**被签名**的，
		// 一个截断的 statement 会以正确的名字、最新的 mtime 躺在那里，
		// 而签名工具会照常签它。
		if werr := atomicfile.Write(out, data); werr != nil {
			return runErr(ctx, stdout, stderr, werr)
		}
		fmt.Fprintf(stdout, "wrote %s (%d subject(s), SLSA provenance v1)\n", out, len(entries))
		return 0
	}

	if _, werr := stdout.Write(data); werr != nil {
		fmt.Fprintf(stderr, "write report: %v\n", werr)
		return errs.CodeInternal.ExitCode()
	}
	return 0
}

// fileHexDigest 返回字节的裸 sha256 hex——
// digest.HashBytes 就是裸 hex（in-toto 的 digest 映射里算法名已经是键）。
func fileHexDigest(b []byte) string {
	return digest.HashBytes(b)
}
