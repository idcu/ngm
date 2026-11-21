# 真实消费方端到端案例：把 ngm 的闭环跑给别人看

依据：[next-step spec](../specs/assessment-2026-11-21-next-step/spec.md) N3 ·
[功能现状梳理](../analysis/ngm-feature-inventory-2026-11-21.md) ·
[签名配方](../guides/signing.md) · [信任模型](../architecture/trust-model.md)。

> 本案例证明的是**内容身份能否被独立复核**，不是"npm 的哪一格还空着"（ADR-044）。
> 每一步都写明**证明什么 / 不证明什么**。签名步骤引用既有[签名配方](../guides/signing.md)，
> 不引入 ngm 自签命令，也不固化第二份 cosign smoke 脚本（延续 C1 决定）。

## 0. 前提与隔离

- 一个真实消费方仓库（本文记作 `<consumer>`），依赖来自自建 Git host 或 GitHub/Gitee。
- 在本机预热过 mirror 与 content store；CI 里 `--offline` 要求两者都已就绪。
- **注入演示必须在一次性副本上做**，并设 `vendor.linkMode=copy`：
  `ngm.vendor/` 默认是 hardlink 到 content store，就地改字节会同时改写 blob 池（[README §149](../../README.md#四层磁盘布局)）。
  单独给这个副本一个临时 `NGM_HOME`，避免污染共享 store。

## 1. 主路径：从声明到可签名证据

```bash
cd <consumer>

# ① 初始化：显式 runtime，生成 ngm.json + src/index.ts
ngm init github:my-org/consumer --runtime=node --dir=.

# ② 声明依赖：refType 显式；完整 40 位 commit 可省略 refType
ngm add github:my-org/utils@v1.2.3 --ref-type=tag --dir=.

# ③ 解析/落地：先在联网环境生成 lock 并预热内容树
ngm install --dir=.

# ④ CI 复现：lock 已提交，禁止改 lock、禁止联网络
ngm install --frozen-lockfile --offline --dir=.

# ⑤ 内容级验证：重放 digest 并逐文件比字节
ngm verify --content --dir=.

# ⑥ 只读导出证据（不触网、不重解析）
ngm sbom --format=cyclonedx --out=ngm.cdx.json --dir=.
ngm attest --with-timestamps --out=ngm.intoto.jsonl --dir=.
```

| 步骤 | 证明什么 | 不证明什么 |
|---|---|---|
| ① `init` | 项目有明确 runtime 与 schemaVersion 1 | 不证明依赖安全 |
| ② `add` | `refType` 显式声明意图，不会把 tag 当 branch | 不证明 tag 未被重打（那是 ⑤ 的事） |
| ③ `install` | ref → commit → 内容摘要写入 `ngm.lock` | 不证明上游代码无害 |
| ④ `frozen` + `offline` | lock 未被改写；离线也能从 mirror/content store 重放 | 不证明冷缓存下还能装上 |
| ⑤ `verify --content` | vendor 字节与 lock digest 一致；tag 重打/分支改写会报漂移 | 不证明签名、不证明构建产物、不证明上游不是恶意 commit |
| ⑥ `sbom` / `attest` | 从 lock 导出带 commit + 内容摘要的确定性证据 | 不声称许可证清单、不签名、不覆盖依赖图以外的构建过程 |

`verify --json` 的 `summary.exitCode` 与进程退出码一致；失败时 stdout 是**完整报告**或**错误信封**，
不是半份 JSON。CI 判断应读它，不要 grep 人读文本。

## 2. 签名与验证（沿用既有配方）

`ngm attest` 只产被签字节。签名、透明日志与验证由 cosign / Sigstore 完成：

```bash
# 以下三步照 docs/guides/signing.md 的 GitHub Actions 配方执行：
#   ngm attest --with-timestamps --out=...
#   cosign attest-blob --type slsaprovenance ...
#   cosign verify-blob-attestation --type slsaprovenance \
#     --certificate-identity ... --certificate-oidc-issuer ...
```

**验证才是门槛，签名不是。** 只签不验等于没设门；`--certificate-identity` /
`--certificate-oidc-issuer` 必须钉死，否则任何人造的签名都能通过。
ngm 不持有密钥、不执行签名（ADR-014/027）。

## 3. 注入演示：篡改必须被抓到

这是本案例的核心。没有这一步，上面全是"通过路径的自证"。

```bash
# 在一次性副本上，且 vendor.linkMode=copy、独立 NGM_HOME
printf '\n// tampered\n' >> ngm.vendor/github.com/my-org/utils/index.js

ngm verify --content --dir=.        # → exit 2（critical：内容不符）
# --json 时 summary.exitCode 也为 2，dependencies[].content.mismatches 给出路径与期望/实际哈希

# 恢复：删掉副本的 vendor 后重跑离线安装；若 blob 池被污染，先重建临时 NGM_HOME
rm -rf ngm.vendor
ngm install --frozen-lockfile --offline --dir=.
ngm verify --content --dir=.        # → exit 0
```

判据（由 `TestV158ConsumerCaseStudyDemonstratesTheWholeLoop` 机械守住）：

1. 命令链按 `install → verify --content → attest` 的顺序出现；
2. 注入段明确写出篡改对象在 `ngm.vendor/`，且期望退出码是 **2**；
3. 引用 `signing.md`，且**不出现任何让 ngm 自己签名的命令**（ADR-014/027）。

变异验证：删掉注入段 → 红；把 `verify --content` 换成默认 `verify` → 红；还原 → 绿。
为什么第二条也算红：默认 structure 只比结构与大小，**等长字节篡改检不出来**，
用它做注入演示会给出假阴性。

## 4. 这个案例回答与不回答的问题

| 问题 | 回答者 |
|---|---|
| 这份 vendor 是不是 lock 钉住的那批字节？ | ngm `verify --content`（全权负责） |
| tag 是否被重打、分支是否被改写？ | ngm 漂移三态（全权负责） |
| 这份声明是谁、在哪次 CI 里签的？ | cosign / Sigstore + 透明日志（ngm 只提供被签字节） |
| 上游 commit 里的代码是否恶意？ | **没人能靠 digest 回答**；需要代码审查、策略与外部审计 |
| 引擎构建产物是否与源码一致？ | 不在 ngm 主轴内；交给构建系统/独立复核 |

## 5. 相关文档

- [签名配方](../guides/signing.md)
- [CLI 参考：verify / sbom / attest](../guides/cli.md)
- [信任模型：不能证明什么](../architecture/trust-model.md)
- [外部审计立项备忘](../analysis/external-audit-brief-2026-11-21.md)
