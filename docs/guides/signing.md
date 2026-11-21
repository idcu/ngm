# 给 `ngm attest` 的语句签名（CI 配方）

> **ngm 不签名、不管密钥、不管理信任**（[ADR-014](../adr/adr-014-self-report-signatures.md)、
> [ADR-027](../adr/adr-027-attestation.md)）。ngm 只产出**可被签名的声明**；
> 签名和验证都用你自己的标准工具（cosign / Sigstore）。本页把那条路固化成可复现配方。

## 为什么有这一页

ADR-027 已经给出"ngm attest → 现场 cosign 签名"的节选，但**没有一条能照着跑完的配方**——
"怎么签、怎么验、CI 里怎么拒绝"散在几段说明里。评估 2026-11-21 把它列为 C1：
把既有决定落成证据，而不是又一次"文档里说可以、没人跑过"。

## 三个角色，别混为一谈

| 问题 | 谁回答 | ngm 的角色 |
|---|---|---|
| "这份内容是否就是锁定的那个 commit" | `ngm verify --content` / `archiveDigest` | **全权负责** |
| "这份声明是谁、在哪次 CI 里签的" | cosign / Sigstore 签名 + 透明日志 | **只提供被签字节** |
| "上游发布物是谁构建的" | 上游的 SLSA provenance | **正交，不声称** |

一句诚实的话：**签过名的语句 ≠ 内容一定无害**。它证明"某个身份为这份内容↔commit 的绑定签了名"，
恶意 commit 也会被如实签名。签名补的是"谁说的"，不补"说的是不是真的"。

## 配方（GitHub Actions，keyless / OIDC）

前置：`ngm install` 已完成、`ngm.lock` 与 `ngm.json` 自洽（`ngm attest` 在过期时默认 exit 3）。

```yaml
permissions:
  id-token: write        # keyless 签名必需（短时证书）
  contents: read

steps:
  - uses: actions/checkout@v6

  - name: Install ngm (from source or pinned release)
    run: go build -o ngm ./cmd/ngm   # 或下载带 SHA256SUMS 的发布二进制并校验

  - name: Resolve + content-verify (CI 默认已是 content，见 ADR-051)
    run: |
      ./ngm install --frozen-lockfile
      ./ngm verify --content

  - name: Emit the signable statement
    run: ./ngm attest --out=ngm.attestation.json --with-timestamps
    # --with-timestamps：让被签语句带上产出时刻（放弃字节确定性）。
    # 想要逐字节可复现的语句就去掉它——见 ADR-027 补记。

  - name: Sign with cosign (keyless, no key material in ngm)
    run: |
      cosign attest-blob --yes --type slsaprovenance \
        --predicate ngm.attestation.json ngm.lock

  - name: Verify at consume time (the actual gate)
    run: |
      cosign verify-blob-attestation --type slsaprovenance \
        --certificate-identity 'https://github.com/<org>/<repo>/.github/workflows/<file>.yml@refs/heads/main' \
        --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
        --predicate ngm.attestation.json ngm.lock
```

## 三条必须说破的点

1. **验证才是门槛，签名不是。** 只签不验等于没设门；`verify-blob-attestation` 的
   `--certificate-identity` / `--certificate-oidc-issuer` 必须钉死到你的工作流，
   否则同组织里别的工作流也能造出可接受的签名。
2. **不要给 ngm 塞密钥。** 任何"让 ngm 自己签"的改动都违反 ADR-014/027；
   ngm 进程里不应出现私钥、token 或密钥路径。
3. **确定性 vs 时间戳二选一。** 默认 `ngm attest` 同 lock 同字节，便于 diff 与黄金比对；
   `--with-timestamps` 让签名带上产出时刻，但那份语句不再逐字节稳定。
   两者都是合法用法，选一个并写进你的 CI 配置。

## 本地 smoke（需自行装 cosign）

本仓库**不提供**签名 smoke 脚本——签名工具在 ngm 之外，随版本变化的 flag 应由 cosign 自己的文档
决定，固化一个"第二个实现"只会漂移（同 `build-release.ps1` 转调 `.sh` 的教训）。
本地要验证时，按上面 CI 配方的三步在临时目录跑一遍 `attest → attest-blob → verify-blob-attestation`；
CI 里的判据（`TestV141SigningRecipeIsReproducible`）守的是**配方文档本身**完整，不冒充跑过签名。

## 相关文档

- [ADR-027 attestation](../adr/adr-027-attestation.md)（被签对象的契约 + 补记）
- [ADR-014 自述签名](../adr/adr-014-self-report-signatures.md)（为什么 ngm 不签名）
- [ADR-051 verify 默认级别](../adr/adr-051-verify-level-defaults.md)
- [真实消费方端到端案例](../practice/ngm-consumer-case-study-2026-11-21.md)（含篡改注入与签名验证步骤）
- [CLI 参考 · ngm attest](./cli.md)
- [竞品对比 §6 SLSA](../COMPETITIVE-ANALYSIS.md#6-生态已经换了一套语言slsa-l1l42026-10-补充)
