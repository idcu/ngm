# 给 `ngm attest` 的语句签名（CI 配方）

ngm **不签名、不管密钥、不管理信任**（[ADR-014](../adr/README.md)、[ADR-027](../adr/README.md)）。它只产出**可被签名的声明**；签名和验证都用你自己的标准工具（cosign / Sigstore）。本文把那条路固化成可复现配方。

## 三个角色，别混为一谈

| 问题 | 谁回答 | ngm 的角色 |
|---|---|---|
| "这份内容是否就是锁定的那个 commit" | `ngm verify --content` / `archiveDigest` | 全权负责 |
| "这份声明是谁、在哪次 CI 里签的" | cosign / Sigstore 签名 + 透明日志 | 只提供被签字节 |
| "上游发布物是谁构建的" | 上游的 SLSA provenance | 正交，不声称 |

一句诚实的话：**签过名的语句 ≠ 内容一定无害**。它证明"某个身份为这份内容↔commit 的绑定签了名"，恶意 commit 也会被如实签名。签名只补"谁说的"，不补"说的是不是真的"。OIDC / keyless 证明"这次发布走了哪条流程、由谁触发"，因此签名要与 `minimumReleaseAge`、`no-downgrade`、内容 digest 并用，不能替代它们。

## 配方（GitHub Actions，keyless / OIDC）

前置：`ngm install` 已完成、`ngm.lock` 与 `ngm.json` 自洽（`ngm attest` 在过期时默认 exit 3）。

```yaml
permissions:
  id-token: write        # keyless 签名必需（短时证书）
  contents: read

steps:
  - uses: actions/checkout@v6

  - name: Install ngm
    run: go build -o ngm ./cmd/ngm   # 或下载带 SHA256SUMS 的发布二进制并校验

  - name: Resolve + content-verify
    run: |
      ./ngm install --frozen-lockfile
      ./ngm verify --content

  - name: Emit the signable statement
    run: ./ngm attest --out=ngm.attestation.json --with-timestamps
    # --with-timestamps：让被签语句带上产出时刻（放弃字节确定性）。
    # 想要逐字节可复现的语句就去掉它。

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

执行顺序必须是：`ngm attest` 产出被签语句 → `cosign attest-blob` 签名 → `cosign verify-blob-attestation` 验证。

## 三条必须说破的点

1. **验证才是门槛，签名不是。** 只签不验等于没设门；`verify-blob-attestation` 的 `--certificate-identity` / `--certificate-oidc-issuer` 必须钉死到你的工作流，否则同组织里别的工作流也能造出可接受的签名。
2. **不要给 ngm 塞密钥。** 任何"让 ngm 自己签"的改动都违反 [ADR-014](../adr/README.md) 与 [ADR-027](../adr/README.md)；ngm 进程里不应出现私钥、token 或密钥路径。
3. **确定性 vs 时间戳二选一。** 默认 `ngm attest` 同 lock 同字节，便于 diff 与黄金比对；`--with-timestamps` 让签名带上产出时刻，但那份语句不再逐字节稳定。两者都是合法用法，选一个并写进 CI 配置。

## 本地 smoke

本仓库不提供签名 smoke 脚本——签名工具在 ngm 之外，随版本变化的 flag 应由 cosign 自己的文档决定。本地要验证时，按上面配方的三步在临时目录跑一遍 `attest → attest-blob → verify-blob-attestation`；ngm 侧只需要确认 `ngm attest` 产出的语句与 lock 对应。

## 相关文档

- [CLI 参考 · ngm attest](./cli.md) · [快速上手](./quickstart.md)
- [ADR 索引](../adr/README.md) · [生态定位](./positioning.md)
