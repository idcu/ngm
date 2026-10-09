# ADR-027：attestation —— 接一层谓词，不自造签名

- **状态**：已定
- **日期**：2026-10-09
- **范围**：评估报告第 3 层第二条（"给 manifest 加 SLSA attestation，而不是自造签名体系"）；
  `ngm attest` 的输出契约；与 [ADR-014](./adr-014-self-report-signatures.md) 的关系

> **结论**：发布 `ngm attest`，把 `ngm.lock` 导出为一份**未签名**的
> [in-toto Statement v1](https://in-toto.io/Statement/v1)（谓词是
> [SLSA provenance v1](https://slsa.dev/provenance/v1)）。
> **签名不在这里做**——ngm 不发明签名格式、不分发密钥、不管理信任：
> 用户/CI 用自己的标准工具（cosign 等）签它。ngm 提供的是**被签的对象**。
>
> **代价与边界**：一份未签名的语句只是"一份可以被签的声明"，
> 它**不**证明"那次物化真的发生过"，也**不**证明上游是谁构建的。
> 想要前者，就在 CI 里现场生成并签名（本文给出那条路径）；
> 想要后者，那是上游 provenance 的事，与本 ADR 正交。

---

## 背景

评估报告第 3 层列了三件事，v0.69 做掉了第一件（SBOM 导出），本文处理第二件：

> **给 manifest 加 SLSA attestation**（不是自造签名，是接一层谓词）
> → 把 ngm 从"L2 的一半"推到"内容侧的 L2"，且与 npm 的 L3 **互补而非竞争**。

它同时点出了一个被混为一谈的地方：

> ADR-014 决定不做"自证文件签名"是对的，但那不等于不做 attestation——
> 这两件事被混为一谈了。

重新读一遍 ADR-014：它拒绝的是**给依赖自带的、ngm 刻意不采信的自述（`verify.js`）加签名**，
以及**发明一套自己的密钥分发/轮换**。它没有（也不该）拒绝
"用**跨生态的既定格式**把 ngm 自己算出的事实写下来，交给用户的标准工具签名"。
本 ADR 是后一件事，因此不构成对 ADR-014 的翻案——它落在 ADR-014 明确留下的那条缝里：
"出现一个跨生态、有既定格式的标准时，ngm 应当**实现该标准**，而不是发明自己的。"

竞品对比 §6 已经把这条路的形状写好了：

> 如果要往 SLSA 这条轴上走，ngm 的天然位置是 **L2 的一半**：
> `archiveDigest` 已经有"内容 ↔ commit"的绑定（比 L1 强），但**没有签名**……
> 最省事的形状是给 manifest 加一层 SLSA 谓词，而不是自造一套。

---

## 三个必须先回答的问题

### 1. 谓词声称什么 —— 逐字段写死，且写清边界

| 字段 | 值 | 它证明什么 |
|------|----|-----------|
| `_type` | `https://in-toto.io/Statement/v1` | in-toto v1 信封（ITE-6 已稳定） |
| `subject[]` | 每个依赖的 **purl**（`pkg:git/<host>/<path>@<commit>[#<subPath>]`） | 这是什么包、哪个 commit |
| `subject[].digest.sha256` | `archiveDigest` 的裸 hex | **规范化内容清单**的哈希（ADR-008） |
| `subject[].annotations["ngm/digest-scope"]` | 一句固定文本 | **这个哈希不是 tarball 的哈希**——整份文档最容易被误读的地方，写在数据里而不是只写在文档里 |
| `predicate.buildDefinition.buildType` | 指向本 ADR 的 URI | 字段语义的规范出处 |
| `predicate.buildDefinition.externalParameters` | `ngm.json` 与 `ngm.lock` 的**字节** sha256 | 输入是"你提交进 Git 的那两份文件"（不是重新序列化后的形态） |
| `predicate.buildDefinition.resolvedDependencies[]` | `git+https://…@<commit>`，digest 用 `gitCommit` | 上游的**不可变锚点**；注解 `ngm/declared-ref` 记下声明的 `refType:ref` |
| `predicate.runDetails.builder` | `https://github.com/idcu/ngm` + ngm 版本 | 谁产出了这份语句 |
| `predicate.runDetails.metadata.invocationId` | **lock 的 sha256**，不是随机 UUID | 见下节"为什么确定性优先" |

**明确不声称**（写进命令的用法文本，不只写在这里）：

- 不声称上游构建完整性（那是上游 provenance 的轴）、不声称可复现构建；
- 不声称许可证（SBOM 那边同样写 `NOASSERTION`）；
- 不声称覆盖传递依赖的**图形**（lock 是扁平清单，语句与它同构）。

### 2. 谁签 —— 用户/CI，ngm 只给被签的对象

未签名是**有意的**，理由与 ADR-014 第 2/3 问同构：

- 签名要可用就要回答"公钥怎么分发/轮换/吊销、用户怎么表达信任"——
  那是用户的信任配置，不是工具替用户发明的东西；
- 更根本的是"验过了之后呢"：**签名证明"谁说的"，不证明"说的是真的"**。
  一把保管良好的钥匙照样能签出一份错误的语句。签名能让"不可归属"变成"可归属",
  它不能让假的变真。

所以路径是：ngm 现场产出语句 → 用户/CI 用标准工具签名（一条命令）：

```bash
ngm attest --out=ngm.attestation.json
cosign attest-blob --yes --type slsaprovenance \
  --predicate ngm.attestation.json ngm.lock
```

消费侧同样用标准工具：

```bash
cosign verify-blob-attestation --type slsaprovenance \
  --certificate-identity <CI 身份> --certificate-oidc-issuer <issuer> \
  --predicate ngm.json ngm.lock
```

**在 CI 里现场生成并签**（而不是提交后再签）时，签名者的时间戳与身份正好补上
语句刻意不写的两样东西：何时、谁。

一条可跑的 CI 路径（GitHub Actions 节选；具体 flag 以你所用 cosign 版本的文档为准）：

```yaml
- name: vendor + attest + sign
  run: |
    ngm install
    ngm attest --out=ngm.attestation.json
    cosign attest-blob --yes --type slsaprovenance \
      --predicate ngm.attestation.json ngm.lock
```

两个值得说破的点：

- `ngm attest` 是**确定性**的：同一份 lock 在本地与 CI 里导出的语句**逐字节相同**——
  所以"我先在本地看一遍要签什么"与"CI 里实际签了什么"可以 diff，签名前没有惊喜；
- 签名之后，这份 blob attestation 的**归属与时间**由签名者（及其透明日志）提供，
  消费方用 `cosign verify-blob-attestation` 加上**自己的**身份策略来验——
  ngm 在这条链里的责任到"把要签的事实算准、写全"为止，到此为止是刻意的。

### 3. 验什么 —— 与 `ngm verify` 正交，互不替代

| | `ngm verify` | 本 ADR 的语句 + 签名 |
|---|---|---|
| 回答 | 本地 vendor 内容与 lock 是否一致（三级检查 + 漂移分类） | 这份 lock/内容绑定**由谁**背书（签名者），且**可被机器消费** |
| 不回答 | 谁背的书 | 内容此刻是否还在（那要 verify） |

所以两条线都必须在：**语句替代不了 verify，verify 也替代不了签名。**
这也正是"内容侧的 L2"的含义：L2 要求的那一半（可验证的来源）由签名补上，
而 ngm 负责把要签的事实算准、写对、写全。

---

## 决策

1. **新增 `ngm attest`**，输出**单个** in-toto Statement v1（多 subject：全部依赖），
   谓词为 SLSA provenance v1。不提供"逐依赖一个文件"的形态——
   一次物化的所有产物共享同一份 provenance，拆开只会让签名次数无谓地翻倍。
2. **确定性优先**：同一份 lock 两次导出**字节相同**。语句里**没有时间戳**、
   `invocationId` 是输入的哈希而不是随机运行 id。"何时/谁"由签名者提供。
   这条与 lock 的 `resolvedAt` 同一处理方式：可复现是审计复核的前提。
3. **不实现签名**、不引入任何加密依赖、不定义自定义字段（除两个 `ngm/` 前缀的注解键）。
4. **只读**：与 `ngm sbom` 同一口径——不触网、不重解析、不写项目（除 `--out`）。
5. **边界写进用法文本**：命令的 `--help` 与 [cli.md](../guides/cli.md) 都要写明
   "未签名、不是构建溯源、许可证一律不声称"。

---

## 什么条件下可以重新考虑

1. **SLSA / in-toto 规范演进**（例如 v2 改字段或新增必填项）——那时**跟随规范**，
   并评估 `buildType` 需要新版本号（`…/v2`）而不是悄悄改语义。
2. **出现"内容侧 attestation"的既定消费格式**（例如某个策略引擎定义了
   "依赖清单 + 内容哈希"的标准谓词）——那时应当实现该标准，而不是加自定义字段。
3. **要覆盖传递依赖图形**——前提是 lock 先携带图形信息（同 SBOM 的依赖关系段，
   §未达成项）；在那之前语句与 lock 同构，不假装更多。

任一条成立时，本 ADR 应被修订而非悄悄绕过。

---

## 后果

- **v0.70 交付 `ngm attest`**：评估第 3 层三条中，第一条（SBOM，v0.69）与第二条
  （attestation，本版）已闭环；第三条（`verify --json` 成为"证据"）仍缺**消费方**——
  在没有具体消费场景之前设计它是猜（评估报告自己也是这么说的）。
- **没有新增密钥管理代码**：没有实现，就没有"半实现的密钥分发"可被误用（同 ADR-014）。
- **诚实记录一条未验证项**：语句的形态按 in-toto v1 / SLSA v1 的公开规范手写，
  离线环境里**没有跑过**官方校验器（`slsa-verifier` / `cosign` 的 schema 检查）。
  字段集与形态由本仓库的测试守住（`internal/attest` 的 6 组断言 + `cmd/ngm` 的 4 条端到端）。
- **`verify --signatures` 与本命令互补**：前者查上游 Git 签名的**状态**（身份问题交回 Git），
  后者承载"内容 ↔ commit ↔ 输入"的绑定。两条都指向同一个纪律：
  **能证明"谁说的"不等于能证明"说的是真的"**。

---

## 相关文档

- [ADR-014 自述文件签名](./adr-014-self-report-signatures.md)（为什么不做自造签名体系；本 ADR 与它的边界）
- [ADR-008 archiveDigest 的定义](./adr-008-archive-digest.md)（`subject.digest` 到底是什么的哈希）
- [ADR-004 声明用 refType、锁定用 commit](./adr-004-reftype.md)（`ngm/declared-ref` 注解的来源）
- [竞品对比 §6 与 SLSA 的关系](../COMPETITIVE-ANALYSIS.md)（L2 的一半、互补而非竞争）
- [评估报告 2026-10-09 · 第 3 层](../internals/assessment-2026-10-09.md)（本 ADR 的事实源）
- [CLI 参考 · ngm attest](../guides/cli.md)（用户侧）
