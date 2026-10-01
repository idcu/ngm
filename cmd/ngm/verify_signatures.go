package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/vendor"
)

// verifyWithSignatures 在常规判定之后追加"这份代码有没有被签名"这一层。
//
// 与 `--sandbox` 同一形状：**只追加**，常规 verify 的判定对象与结论完全不变。
//
// 三条纪律（[ADR-014](../../docs/adr/adr-014-self-report-signatures.md) 决策 3 的实现）：
//
//  1. **未签名默认不是失败。** 绝大多数依赖没有签名，把它当错误会让 verify 对所有人变红——
//     那是假警报，而假警报会让真的警报失效。想要门槛的人自己开 `--require-signed`。
//  2. 判定用**用户自己的**密钥配置（GPG keyring / `gpg.ssh.allowedSignersFile`）。
//     ngm 不管理密钥、不分发密钥、不发明签名格式——它只报告事实。
//  3. 默认不查，是因为每个依赖要跑一到两次 git 子进程，而 verify 的主要成本就是 spawn
//     （见 verify.go 里"100 依赖 100 次 spawn"的注释）。静默多跑 N 次与"成本必须可预期"
//     冲突——`ngm tree` 的 `--osv` 是同一个先例。
//
// 严格档位失败返回 **2**（完整性/信任类），与 `--sandbox` 的脚本失败同类，`--allow-drift` 无效。
func verifyWithSignatures(
	ctx context.Context,
	env *projectEnv,
	lf *lock.File,
	baseCode int,
	require, jsonMode bool,
	stdout, stderr io.Writer,
) int {
	// `--json` 时写到 stderr：stdout 必须是**纯 JSON**（它的文档写着 "CI should use this"）。
	out := stdout
	if jsonMode {
		out = stderr
	}

	fmt.Fprintln(out, "\n— git signatures (--signatures) —")
	fmt.Fprintln(out,
		"         verdicts come from your own Git key configuration; ngm does not manage keys")

	mirror := vendor.NewMirror(env.Layout.MirrorRoot(), env.GitOpts)
	var notSigned []string
	checked := 0

	for i := range lf.Dependencies {
		d := &lf.Dependencies[i]

		repo, perr := resolve.ParseSlug(d.Name)
		if perr != nil {
			fmt.Fprintf(out, "  ? %s — cannot read its address: %v\n", d.Name, perr)
			notSigned = append(notSigned, d.Name)
			continue
		}
		repoPath := mirror.PathFor(repo)

		sig, serr := git.CommitSignature(ctx, env.GitOpts, repoPath, d.Commit)
		if serr != nil {
			fmt.Fprintf(out, "  ? %s — cannot check its commit: %v\n", d.Name, serr)
			notSigned = append(notSigned, d.Name)
			continue
		}

		// commit 没有好签名时看 tag：**多数项目只签 tag，不签每个 commit**。
		// 只看 commit 会把它们一律误报成"没签名"。
		if sig.Status != git.SigGood && d.RefType == string(resolve.RefTypeTag) && d.Ref != "" {
			if tsig, terr := git.TagSignature(ctx, env.GitOpts, repoPath, d.Ref); terr == nil &&
				signatureRank(tsig.Status) > signatureRank(sig.Status) {
				sig = tsig
			}
		}

		checked++
		renderSignature(out, d, sig)
		if sig.Status != git.SigGood {
			notSigned = append(notSigned, d.Name)
		}
	}

	if require && checked == 0 {
		// 空转守卫：一个都没查成时**不能算通过**。否则一个坏掉的 mirror 会让
		// `--require-signed` 变成一句空话（"全都通过了"，因为什么都没查）。
		fmt.Fprintln(stderr,
			"error: --require-signed was given but no dependency could be checked at all")
		fmt.Fprintln(stderr,
			"  hint: that is a failure, not a pass — check that the mirror is present (ngm install)")
		return 2
	}

	if !require {
		fmt.Fprintf(out, "  (%d checked; this is a report, not a gate — see --require-signed)\n", checked)
		return baseCode
	}

	if len(notSigned) > 0 {
		fmt.Fprintf(stderr,
			"error: %d dependency(ies) are not signed by a key you trust: %s\n",
			len(notSigned), strings.Join(notSigned, ", "))
		fmt.Fprintln(stderr,
			"  hint: this gate uses your own Git key configuration (GPG keyring / gpg.ssh.allowedSignersFile); "+
				"ngm does not manage keys — add the signer's public key there, or run without --require-signed to see it as a report")
		return 2
	}
	return baseCode
}

// renderSignature 渲染单个依赖的签名结论。
func renderSignature(w io.Writer, d *lock.Dependency, sig git.Signature) {
	switch sig.Status {
	case git.SigGood:
		who := sig.Signer
		if who == "" {
			who = "key " + sig.Key
		}
		fmt.Fprintf(w, "  ✓ %s@%s → signed by %s (%s)\n", d.Name, d.Ref, who, sig.From)
	case git.SigNone:
		// "·" 而不是"！"：没有签名**不是问题**，只是事实。符号会决定用户怎么读它。
		fmt.Fprintf(w, "  · %s@%s → no signature\n", d.Name, d.Ref)
	case git.SigUntrusted:
		fmt.Fprintf(w, "  ! %s@%s → signed, but not verifiable with your keys (%s)\n", d.Name, d.Ref, sig.From)
		if sig.Raw != "" {
			fmt.Fprintf(w, "      %s\n", sig.Raw)
		}
	default:
		fmt.Fprintf(w, "  ✗ %s@%s → the signature is **invalid** (%s)\n", d.Name, d.Ref, sig.From)
		if sig.Raw != "" {
			fmt.Fprintf(w, "      %s\n", sig.Raw)
		}
	}
}

// signatureRank 给结论排序，用于"commit 与 tag 谁更值得报告"。
//
// 偏好更**可操作**的信息：有签名（哪怕验证不通过）比"没签名"更值得说；
// 而"签名坏了"比"用你的钥匙无法确认"更具体、更该被看到。
func signatureRank(s git.SignatureStatus) int {
	switch s {
	case git.SigGood:
		return 3
	case git.SigBad:
		return 2
	case git.SigUntrusted:
		return 1
	default:
		return 0
	}
}
