# 架构决策记录（ADR）

> ADR 记录"已定的重大决策与理由"。状态流转：`提议` → `已定` → `已废弃` / `被取代`。

---

## 索引

| 编号 | 决策 | 状态 |
|------|------|------|
| [ADR-001](./adr-001-go.md) | 为什么用 Go | 已定（v2 修订） |
| [ADR-002](./adr-002-git-direct.md) | 为什么直接拉 Git 仓库 | 已定 |
| [ADR-003](./adr-003-vendor.md) | 为什么纯 vendor/ 目录 | 已定（v2 修订） |
| [ADR-004](./adr-004-reftype.md) | 为什么声明用 refType 锁定用 commit | 已定 |
| [ADR-005](./adr-005-engine-interface.md) | 为什么引擎统一接口动态选择 | 已定（v2 修订） |
| [ADR-006](./adr-006-lytd-merge.md) | 为什么 lytd 并入 ngm | 已定（v2 修订） |
| [ADR-007](./adr-007-runtime-node-deno.md) | Node 还是 Deno？ | 已定 |
| [ADR-008](./adr-008-archive-digest.md) | archiveDigest 的定义 | 已定 |
| [ADR-009](./adr-009-supply-chain-policy.md) | 供应链策略的执行时机与失败语义 | 已定（v0.2 首个决策） |
| [ADR-010](./adr-010-online-verify-fetch-policy.md) | 在线 verify 的抓取策略 | 已定 |
| [ADR-011](./adr-011-wasm-runtime.md) | wasm adapter 的运行时与 ABI | 已定（v0.3 A 组） |
| [ADR-012](./adr-012-sandbox.md) | 沙箱与执行边界 | 已定（v0.3 D 组之后） |
| [ADR-013](./adr-013-remote-adapter.md) | remote adapter 的信任边界 | 已定（v0.3 A 组，**结论：不发布**） |
| [ADR-014](./adr-014-self-report-signatures.md) | 依赖自述的签名检查 | 已定（v0.4 复核，**结论：不做**；身份交回 Git 的签名机制） |
| [ADR-015](./adr-015-commit-ref-resolution.md) | commit 型依赖不做 ref 解析 | 已定（v0.5 A 组）：commit 型 **0 次 git 子进程**；tag/branch 不变 |
| [ADR-016](./adr-016-mirror-url-local-read.md) | mirror 远端地址改为读 config 文件 | 已定（v0.5 A4）：形状不认识即回退子进程；tag 型 4.00 → **3.00 次/依赖** |
| [ADR-017](./adr-017-remote-adapter-release-decision.md) | remote adapter 的**发布决策** | 已定（v0.6）：**仍不发布**；剩下的问题写成可判定的门槛（修订 [ADR-013](./adr-013-remote-adapter.md)） |
| [ADR-018](./adr-018-store-reclaim.md) | 内容寻址 store 的**回收与去重** | 已定（v0.6）：**不做**按可达性自动删除；`store usage` / `store prune` 排期；**写入侧去重**是长期解法 |
| [ADR-019](./adr-019-content-addressed-blobs.md) | 层 2 改为**按文件内容寻址**（blob 池 + 树清单） | 已定（v0.7）**并已实施（v0.8）**：digest 定义不变、blob 里含 mode（§修订）、`symlink` 落地模式退化；实测 20.0× → 0.6× |
| [ADR-020](./adr-020-remote-adapter-shelved.md) | `remote` adapter **正式搁置** | 已定（2026-10-03）：该 adapter **在代码里不存在**（"发布"= 从零写协议）；三条触发条件（真实需求 / 本地算力证据 / 跨生态标准） |
| [ADR-021](./adr-021-symlink-link-mode.md) | `symlink` 落地模式**保留降级** | 已定（2026-10-03）：不实现"按需物化一棵 hardlink 树"；触发条件是"具体的工具/流程要求 + 实测数字" |
| [ADR-022](./adr-022-verify-performance-target.md) | verify 的性能目标改为**次数门禁 + 秒数观测** | 已定（2026-10-03）：3s 不再是验收目标（v0.5 实测它在噪声里不可判别）；门禁是 spawn 预算 |
| [ADR-023](./adr-023-orphan-reclaim.md) | 回收**无人引用的 blob**（`store prune --orphans`） | 已定**并已实施**（2026-10-03）：只删"没有任何清单引用 + 比门槛更旧"的字节；**有清单读不出来时拒绝删除**；仍不做按可达性删除 |
| [ADR-024](./adr-024-bounded-explanations.md) | 解释输出**必须有界**（`why` 的路径枚举 / `tree` 的展开） | 已定**并已实施**（2026-10-04）：`why` 默认最多 **64** 条路径、`tree` 默认最多 **4096** 个条目；达到上界**必须说出来**（`pathsTruncated` / `entriesTruncated` + 文字里那句），`--all` 显式解除，退出码不变。实测：**41 个节点 → 1 048 576 条路径 / 626 MB**，而图的形状来自上游清单 |
| [ADR-025](./adr-025-no-builtin-engine-defaults.md) | 引擎选择**不设**内置默认 | 已定（2026-10-04）：`typeCheck` / `typeDecl` / `css` 三个 kind **刻意不预置**；缺声明 → `exit 3`，报错点名 kind、列出目录里的候选、指向 `ngm.engines.json`；声明了但未装 → `exit 5` |
| [ADR-026](./adr-026-exit-code-6-internal-failure.md) | 加一个退出码 **6 = 内部失败** | 已定**并已实施**（2026-10-08）：panic 与"结论有了却送不出去"（原先与策略失败共用码 1）分出来；码 1 的四类策略来源一个不动 |
| [ADR-029](./adr-029-loopback-addresses.md) | 仓库地址：**收下回环**（只收 `localhost` / `127.0.0.1`） | 已定**并已实施**（v0.83；原编号 028，因与 ADR-028「直接依赖边进 lock」撞号改 029）：单标签主机名此前一律拒绝，于是"本机跑一遍"不可做，而 `127.0.0.1` 一直能用——**同一件事两种结论**。收下两个字面量、其余照旧拒绝（反笔误不受影响），报错把三条出路说清；`[::1]`、端口、`git://` 协议、`net:` 门禁**都不动**（各自的边界写进 ADR-029 §代价与边界） |
| [ADR-027](./adr-027-attestation.md) | attestation：**接一层谓词，不自造签名** | 已定**并已实施**（v0.70）：`ngm attest` 输出**未签名**的 in-toto Statement v1（谓词 SLSA provenance v1）；确定性优先（无时间戳、`invocationId` = lock 的哈希）；签名交给用户/CI 的标准工具（cosign） |
| [ADR-028](./adr-028-dependency-graph-in-lock.md) | 把**直接依赖边**写进 ngm.lock | 已定（2026-10-09，**v0.73 实现**）：每依赖新增**恒在**的 `requiredBy`（直接引用者，排序去重，直接依赖为 `[]`）——`why` 离线 0 spawn、SBOM 增依赖关系段；lockfileVersion 不动（可选字段 + 解码器忽略未知字段是既有契约）；**只存直接边**（全路径是指数的，ADR-024）；`verify`/`tree`/`attest` 明确不变 |
| [ADR-030](./adr-030-mirror-reclaim.md) | 层 1（mirror）的**占用、残骸与整理** | 已定**并已实施**（2026-10-15，v0.100）：`store usage` 增报 mirror（含点名 **一个 ref 都没有**的目录）、`store prune` 收 `objects/pack/tmp_*`（门槛 **1 小时**；实测 git 自己只清 **14 天**以上的）、新增 `store gc`；**拒绝** `--single-branch` 与部分克隆（会打断 refType 的完整性与"离线 digest 重放"，见 ADR-004/008/010） |
| [ADR-031](./adr-031-module-collections.md) | 一个仓库承载**多个模块**（Module Collections） | 已定（2026-10-20）：模块身份 `repo + subPath`；模块自己的 `ngm.json` 权威，根清单仅在缺失时回退并 warning；不做 workspace/registry；tag 可选，commit 是真相 |
| [ADR-032](./adr-032-scoped-content-proof.md) | **模块级内容证明**：`moduleDigest` + `subtreeOid` | 已定**并已实施**（2026-10-20，v0.102）：根依赖保留 `archiveDigest`，模块依赖只写 `moduleDigest + subtreeOid + version`；lockfileVersion 2.0；模块 content store/vendor/verify landing 均按模块 scope |
| [ADR-033](./adr-033-language-neutral-core.md) | 抽离**语言无关**的 Git 模块证明核心 | 已定（2026-10-20，v0.104 搬迁完成）：`core/{atomicfile,digest,errs,git,lock,resolve,vendor,verify}` 已落地；`gitee.com/idcu-go/modproof v0.2.0` 已发布，ngm 模块 proof 已切换外部依赖，idcu-go 仍登记 `planned`（仅剩双消费者） |
| [ADR-034](./adr-034-module-cache.md) | **module cache** 与 mirror 的边界 | 已定**并已实施**（2026-10-20，v0.104）：ADR-030 对 mirror 的拒绝不变；module cache 只服务 lock 钉住的 `(commit, subPath)`，可 `blob:none`/`--depth=1`；install-from-lock 优先使用，filter 不支持明确回退；verify --offline 复用且禁止惰性联网 |
| [ADR-035](./adr-035-refType-narrow-default.md) | `refType` 的**窄默认**（仅完整 commit hash） | 已定**并已实施**（2026-10-20）：完整 40 位 hex 可省略 refType 并按 commit 写回；tag/branch/短 hash 仍必填；三处共用 `resolve.IsFullCommit`；不做全局默认 |
| [ADR-036](./adr-036-infer-ref-type-opt-in.md) | `--infer-ref-type`：显式 opt-in 的远端证据推断 | 已定**并已实施**（2026-10-20）：只在 `add` 缺 `--ref-type` 且非完整 commit 时生效；tag/branch 二选一，同名或缺失即失败；顺带修 `git.LSRemote` 参数顺序 |
| [ADR-037](./adr-037-per-module-publish-override.md) | 逐模块 `publish` **完全覆盖**全局 `--releases` | 已定**并已实施**（2026-10-21）：`publish` 显式 true/false 直接决定，nil 才跟随全局；不做 AND/OR 短路；只影响 release 索引 |
| [ADR-038](./adr-038-no-own-git.md) | **不自研 Git**，保持子进程 + 单一边界 | 已定（2026-10-28）：运行时 Git 只在 `internal/core/git`；不自研对象/协议；未来只考虑 go-git/gitoxide/libgit2 backend |
| [ADR-039](./adr-039-exit-code-precedence.md) | `verify` 退出码 **2 压过 4**：已确证的篡改 > 没查成 | 已定**并已实施**（2026-10-29）：原"4 优先"的理由只排除 0/1，没排除 2；而 4 的语义是可重试，于是 CI 会把被篡改的树当成网络抖动重试。`ExitCode()` 改为先记漂移分类再看 `Err` |
| [ADR-040](./adr-040-adopt-proof-layer.md) | 从"包管理器"退回"证明层"：`ngm adopt` 为已装好的依赖出证据 | 已定**并已实施**（2026-10-29）：只读 `node_modules` + `package-lock.json`，摘要与 `archiveDigest` 同构；读不了的 lock 在 `notes`/`coverage.unknown` 里具名；**不接管安装**，因此不再把 registry 互通列为待办 |

---

## 流程

1. 复制模板，编号为下一个可用编号（`adr-XXX-<slug>.md`）
2. 状态先标 `提议`，讨论定稿后改 `已定`
3. 决策被推翻时：新 ADR 标注"取代 ADR-XXX"，旧 ADR 状态改为 `被取代`，**不删除**

## 模板

```markdown
# ADR-XXX：<决策标题>

- **状态**：提议 / 已定 / 已废弃 / 被取代（by ADR-YYY）
- **日期**：YYYY-MM-DD
- **范围**：<影响的领域>

> **结论**：<一句话，读完就知道选了什么>
> **代价**：<诚实写出这个决策付出了什么>

---

## 背景
<面临的问题与约束>

## 决策
<明确的一句话决策；可与上方"结论"重复，也可更精确>

## 理由
<支持点；以及诚实的代价。若上方"结论/代价"两行已写清，本节可省略>

## 后果
<决策带来的影响与后续约束>

## 相关文档
- <链接>
```

> **为什么把"结论 / 代价"提到最前面**：ADR 的价值是让后来者几秒钟内理解"选了什么、付出什么"。
> **代价必须写**——只列好处的 ADR 是宣传，不是决策记录。

## 约定

- ADR 是**决策快照**，不随实现频繁修改；协议、清单、接口的"活文档"在各自唯一事实源位置（见 [README](../README.md) 的文档约定）
- 与 ADR 冲突的实现必须先修订 ADR，再改代码