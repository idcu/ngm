package git

import (
	"context"
	"strings"
)

// SignatureStatus 是一次签名检查的结论。
//
// 语义**照搬 Git 自己的判定**（`git log --format=%G?`，见 git-log(1)），不重新定义
// "什么算签过"：是否信任一把钥匙，完全由**用户自己的**密钥配置决定
// （GPG keyring / `gpg.ssh.allowedSignersFile`）。ngm 不管理密钥、不分发密钥、
// 也不发明签名格式（[ADR-014](../../docs/adr/adr-014-self-report-signatures.md) 决策 2）。
type SignatureStatus string

const (
	// SigGood 签名有效，且签名者受你的配置信任（`G`）。
	SigGood SignatureStatus = "good"
	// SigUntrusted 签名存在，但用你现有的钥匙串**无法确认**（`U` 密钥未知 / `X` `Y` 过期 /
	// `R` 已吊销 / `E` 无法校验）。
	//
	// 它**不等于伪造**——这个区分很重要：把"无法确认"说成"签名无效"会让用户
	// 去查一个根本不存在的问题。
	SigUntrusted SignatureStatus = "untrusted"
	// SigBad 签名存在但**无效**（`B`）：内容与签名不符。这才是"签名被破坏"。
	SigBad SignatureStatus = "bad"
	// SigNone 没有签名（`N`）。
	SigNone SignatureStatus = "none"
)

// Signature 是一次签名检查的结果。
type Signature struct {
	// Status 是结论。
	Status SignatureStatus
	// Signer 是签名者（`%GS`）；未签名时为空。
	Signer string
	// Key 是签名密钥标识（`%GK`）；未签名时为空。
	Key string
	// From 说明结论来自哪一层：`commit` 或 `tag <name>`。
	//
	// **必须写出来**：同一个依赖，commit 未签名而 tag 已签名是最常见的形态
	// （多数项目只签 tag，不签每个 commit）。不说明来源，用户就无法理解
	// "为什么它说签名了，我明明没签 commit"。
	From string
	// Raw 保留 git 给出的原始线索（`%G?` 的字符，或 tag 验证的输出首行），供诊断。
	Raw string
}

// CommitSignature 检查某个 commit 的签名。
//
// 用 `%G?` 而不是解析 `git verify-commit` 的输出：前者是**机器可读**的单一字符，
// 后者是人类措辞，随 git 版本与语言环境变化。
func CommitSignature(ctx context.Context, opts Options, repoPath, commit string) (Signature, error) {
	o := opts
	o.Dir = repoPath

	res, err := Run(ctx, o, "log", "-1", "--format=%G?%x00%GS%x00%GK", commit)
	if err != nil {
		return Signature{}, err
	}
	fields := strings.SplitN(strings.TrimRight(string(res.Stdout), "\r\n"), "\x00", 3)
	raw := ""
	if len(fields) > 0 {
		raw = strings.TrimSpace(fields[0])
	}
	sig := Signature{Status: statusFromRaw(raw), From: "commit", Raw: raw}
	if len(fields) > 1 {
		sig.Signer = strings.TrimSpace(fields[1])
	}
	if len(fields) > 2 {
		sig.Key = strings.TrimSpace(fields[2])
	}
	return sig, nil
}

// TagSignature 检查某个 tag 的签名。
//
// 判定用 `git verify-tag` 的**退出码**（0 = 受你信任的密钥所签）；是否"存在签名"
// 用 `%(contents:signature)` 判断（它能区分"轻量 tag / 未签名"与"有签名"）。
//
// 这里有一处**不可避免的脆弱**：git 没有为 tag 验证提供机器可读的状态码（commit 有 `%G?`，
// tag 没有）。因此"无效"与"无法确认"的分界要靠 git 自己的措辞（`BAD signature`）。
// 它只影响**分类的粒度**，不影响"通过 / 不通过"——后者由退出码决定，与措辞无关。
func TagSignature(ctx context.Context, opts Options, repoPath, tag string) (Signature, error) {
	o := opts
	o.Dir = repoPath

	pres, perr := Run(ctx, o, "tag", "--format=%(contents:signature)", tag)
	if perr != nil {
		return Signature{}, perr
	}
	if strings.TrimSpace(string(pres.Stdout)) == "" {
		return Signature{Status: SigNone, From: "tag " + tag}, nil
	}

	// 有签名块：用退出码定"通过/不通过"。非零退出时 Run 返回 error，
	// 但 Result 仍然可用（exec.go 的返回值约定），因此这里看的是 ExitCode。
	vres, _ := Run(ctx, o, "verify-tag", tag)
	out := string(vres.Stdout) + string(vres.Stderr)
	if vres.ExitCode == 0 {
		return Signature{Status: SigGood, From: "tag " + tag, Raw: "verify-tag exit 0"}, nil
	}
	if strings.Contains(out, "BAD signature") {
		return Signature{Status: SigBad, From: "tag " + tag, Raw: firstNonEmptyLine(out)}, nil
	}
	return Signature{Status: SigUntrusted, From: "tag " + tag, Raw: firstNonEmptyLine(out)}, nil
}

// statusFromRaw 把 `%G?` 的字符映射为结论。
//
// G 是唯一表示"受你信任"的值；B 表示签名被破坏；N 表示没有签名；
// 其余（U/X/Y/R/E）与**任何未知字符**一律落到"无法确认"——
// 保守的方向是明确的：不确定时绝不报"好"。
func statusFromRaw(raw string) SignatureStatus {
	switch strings.TrimSpace(raw) {
	case "G":
		return SigGood
	case "B":
		return SigBad
	case "N":
		return SigNone
	default:
		return SigUntrusted
	}
}

// firstNonEmptyLine 取第一行非空内容（用于把 git 的措辞放进 Raw 供诊断）。
func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
