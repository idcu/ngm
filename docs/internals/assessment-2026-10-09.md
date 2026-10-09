# ngm 全维度评估（2026-10-09 · 逐行源码复核 + 竞品对照）

> 这是一份**外部视角的评估**，不由项目文档复述而来。
> 方法：逐行读完 `cmd/ngm`（32 个非测试文件，6073 行）与 `internal/*` 全部非测试源码，
> 交叉核对测试覆盖；`go build` / `go vet` / 全量 `go test` 实测；竞品侧做了 2026 年现状检索。
> 与 [project-state.md](./project-state.md)（项目自评）并列阅读——**冲突处以实测为准**。

---

## 0. 一句话结论

> **工程成熟度显著高于同类早期项目，产品价值区间真实但极窄，而采用度是 0。
> 过去 53 个版本（v0.13–v0.65）里 29 个"产品代码 0 行改动"，项目已经陷入自建校验网的自我循环。
> 下一步不该是第 33 张契约网，而是：修掉 5 个动摇核心承诺的缺陷，然后把 PackageGate 这张牌打出去。**

---

## 1. 体检数据（本次实测，非引用）

| 项 | 实测值 | 说明 |
|---|---|---|
| Go 产品代码 | **22,995 行** / 109 文件 | `internal/` 15 包 + `cmd/ngm` |
| Go 测试代码 | **31,899 行** / 160 文件 | **测试:产品 = 1.39 : 1** |
| Markdown 文档 | **28,406 行** / 197 文件 | **文档行数 > 产品代码行数** |
| 测试函数 | **519** 个 | 分布见下表 |
| `go build ./...` | ✅ 干净 | |
| `go vet ./...` | ✅ 干净 | |
| 全量 `go test` | 13/15 包 PASS | 见 §1.2 |
| 外部依赖 | **仅 1 个**（`wazero`） | 无 cobra、无 Go git 库 |
| 提交 / tag | 283 commits / 65 tags | 单人（idcu），2026-09-29 起 |

### 1.1 测试分布（不平衡是真实的）

| 包 | 测试函数 | 产品行数 | 评价 |
|---|---|---|---|
| `cmd/ngm` | 161 | 6,073 | 契约网密集 |
| `internal/git` | 63 | 1,793 | 充分 |
| `internal/vendor` | 55 | 2,223 | 充分 |
| `internal/adapter` | 54 | 3,261 | 充分 |
| `internal/resolve` | 41 | 1,622 | 充分 |
| `internal/supplychain` | 23 | 922 | 充分 |
| `internal/security` | 16 | 727 | 含真实 Deno 端到端 |
| `internal/config` | 15 | 995 | 有字段接线网 |
| **`internal/verify`** | **0** | **969** | **⚠️ 安全关键路径零单测** |
| **`internal/observability`** | **2** | **847** | ⚠️ 仅靠 cmd 层间接覆盖 |

> `internal/verify` 是"证明"这件事的全部落地：三级检查、漂移分类（expected/unexpected/critical）、
> 退出码优先级。**969 行、零包内单测**，只由 `cmd/ngm` 的验收测试间接看着。这是最大的覆盖缺口。

### 1.1b 测试预算的去向（实测：最慢的 25 个测试 ≈ 29 分钟）

对 `cmd/ngm` 做 `-v` 计时，取耗时 ≥30s 的测试（161 个中的 25 个）：

| 耗时 | 测试 | 性质 |
|---|---|---|
| 137.6s | `TestV22DeclaredCodesAreMeasuredOrKnownGaps` | 契约网 |
| 117.7s | `TestV36EveryFailingReportCarriesANextStep` | 契约网 |
| 117.4s | `TestV50MachineReadableReportsCarryTheNextStepToo` | 契约网 |
| 114.1s | `TestV17ExplanationIsBoundedEndToEnd` | 功能 + 网 |
| 113.7s | `TestV02ObservabilityAcceptance` | 产品验收 |
| 105.0s | `TestM3Acceptance` | 产品验收 |
| 98.6s | `TestM5Acceptance` | 产品验收 |
| 73.0s | `TestV33ActionLinesNameSomethingExecutable` | 契约网 |
| 71.0s | `TestV06VerifySpawnBudget` | 性能门禁 |
| 67.6s / 67.1s ×2 / 64.9s / 62.1s / 58.9s | `V34` `V30` `V32` `V51` `V31` | 契约网 |
| 47.7s / 47.4s / 45.9s / 35.9s / 35.7s / 30.4s | `M4` `V03Integrations` `V02SupplyChain` `V02InstallModes` `V02ObservabilityGolden` `V03Mappings` | 产品验收 |
| 33.8s / 33.3s / 31.3s | `V16` `V37` `V40` | 契约网 |

**这 25 个测试合计约 29 分钟，占整个包运行时间的绝大部分。其中契约网/元测试约占 64%（~18.6min），
产品行为验收约占 36%（~10.4min）。**

这不是说网没用——它们抓到过真缺陷（v0.40 的 `--` 重排、v0.41 的 `--dir=`、v0.44 的死赋值）。
但**测试预算的去向本身就是一份证据**：项目把多数算力花在验证"自己的承诺是否自洽"，
而不是验证"产品行为是否正确"。这与 §5 的判断是同一件事的两种读数。

### 1.2 本次全量测试实测结果

```
ok    internal/adapter 30.9s    ok  internal/config 1.5s     ok  internal/digest 0.2s
ok    internal/errs    0.2s     ok  internal/git    151.0s   ok  internal/integrations 3.5s
ok    internal/lock    1.2s     ok  internal/mappings 5.4s   ok  internal/observability 0.3s
ok    internal/resolve 190.3s   ok  internal/supplychain 4.1s ok  internal/testutils 62.9s
ok    internal/vendor  226.8s
FAIL  internal/security 7.9s    —— TestSandbox_RealDeno_NetOnlyWhenGranted（本机网络受限，环境相关）
?     internal/verify           —— no test files
```

- 我这次用默认 `-timeout 10m` 跑 `cmd/ngm` 触发了 Go 的包级超时。**这不是项目缺陷**——
  `ci.yml:55` 与 `release.yml` 都用 `-timeout 40m`，且历史记录已确认本机 PATH 有 44 项、
  Windows 上 `exec.LookPath` 与 `t.TempDir()` 清理都慢。**它是慢，不是坏。**
- `internal/security` 那条失败是真实 Deno 沙箱联网用例，依赖本机网络可达性。

---

## 2. 架构：五层，分层是干净的

```
cmd/ngm      21 个子命令 · 手搓 flag 分发（无 cobra）· 单一错误出口 runErr
   ↓
resolve      归一化 / refType / 分层 BFS 图 / root-wins / 策略门禁
git          唯一进程构造点 newGitCommand（门禁 → 计数 → WaitDelay）
lock         确定性序列化（字段顺序=契约）
digest       archiveDigest 规范清单
   ↓
vendor 4 层  ① mirror(裸仓) ② content store(blob 池+树清单) ③ ngm.vendor(hardlink) ④ cache(可弃)
   ↓
verify / supplychain / security / observability / mappings / integrations / adapter
```

**分层纪律是真的**（不是文档里说说）：

- `resolve` 不依赖 `config`/`supplychain`，用函数注入（`CheckRepo`、`EnsureMirror`）反向解耦
- `git` 包只依赖一个 `Permissions` 接口
- 全仓库 `exec.Command` **只允许出现在 `git/spawn.go`**，由文本级元测试机械守卫
- `runErr` 是**唯一**的错误渲染点 → 300 个构造点共享一套双通道（人读 + JSON 信封）渲染

### 2.1 archiveDigest 的字节级格式（这是产品的地基）

```
manifest = "ngm-archive-digest/v1" NUL
         ( path NUL mode NUL sha256(content) NUL )*     按 path 的 UTF-8 字节序升序
archiveDigest = "sha256:" + hex(sha256(manifest))
```
- 目录不成条（`ls-tree -r`）；symlink 的"内容"是链接目标字符串；**不做 CRLF 转换**
- LFS 指针与 gitlink 显式拒绝（exit 3），绝不静默哈希
- 完全离线可重放（本地 mirror）

### 2.2 v2 存储布局（v0.8 的成果，值得肯定）

```
~/.ngm/content/
  blobs/<aa>/<blob名>     blob名 = sha256(mode ‖ 0x00 ‖ content)
  trees/<digest>/{manifest.json, meta.json}
  sha256/<digest>/…       v1 遗留，只读不迁移
```

**`blobKey` 掺入 Git mode 是这个文件里最重要的设计**：hardlink 共享 inode，inode 的 mode 是共享的，
纯内容寻址无法表达"同一份字节一处 644 一处 755"。掺入 mode 后可执行位天然正确。
实测量到的收益：**12 个 commit 从 20.0× 放大降到 0.6×（1.88 MiB → 59.9 KiB）**。

---

## 3. 源码级缺陷清单（本次逐行读出来的，按严重度）

> 这些是**文档里没有的**。项目自己的状态页写的是"未完成任务"，不是"代码里的错"。

### P1 — 动摇核心承诺，应尽快修

| # | 位置 | 问题 |
|---|---|---|
| 1 | `internal/resolve/graph.go:380-386` | `ResolveRef` **未透传 `Policy`** → 图解析阶段的 `git ls-remote` 绕过 `run:git` 门禁。而 `reftype.go:29-32` 自己写的不变量是"ngm 里不应存在绕过门禁的 git 调用路径"。配置 `deny:["run:git"]` 时仍会为每个 tag/branch 依赖起子进程。（已逐行核对确认） |
| 2 | `internal/vendor/links.go:203,213` | **symlink 目标与清单 path 均无逃逸校验**。`os.Symlink(e.LinkTarget, target)` 原样重建 Git 仓库里的链接目标；`filepath.Join` 会清洗 `..`，故 `e.Path="../outside/x"` 可写到 `ngm.vendor/` 之外。项目最忌讳的失败形态正是"看起来正常但指向别处"——这一处恰恰是它。 |
| 3 | `internal/resolve/graph.go:203-229` | **先解析、后判重**。`mergeFrontier` 只在本层去重、不查 `seen`，跨层菱形依赖（A→C 且 A→B→C）会让 C 完整重新解析一次，含一次真实 `git fetch --prune`。复杂度从 O(节点) 退化到 O(边)。测试没抓到是因为 fixture 的 `EnsureMirror` 太便宜。 |
| 4 | `cmd/ngm/install.go:220` + `internal/vendor/content.go:214` | **每依赖 6 次 git spawn，digest 完整算两遍**（`BuildArchiveDigest` 2 次 + `Put` 内部再算 2 次 + `ListTree` + `CatFileBatch`）。且全串行。这是 install 的主要成本。 |
| 5 | 全链路（`content_v2.go:198-227`、`content.go:299`） | **无一次 `fsync`**。"清单存在 ⇒ blob 在池里"是整个 store 的核心不变式，但它只在**顺序**层成立，**持久性**层不成立——断电可产生"清单在、blob 空"。 |

### P2 — 正确性 / 一致性

| # | 位置 | 问题 |
|---|---|---|
| 6 | `reftype.go:101` vs `lock/schema.go:198` | 解析接受 **7–39 位短 commit**，lock 要求**必须 40 位**。⇒ 短 commit 能通过解析并写入 lock（`installFresh` 不调 `Validate()`），下次读 lock 直接 exit 3，且用户无法通过正常路径修复。真实的往返不一致。 |
| 7 | `lock/io.go:58` | `os.CreateTemp` 创建 **0600**，rename 后 `ngm.lock` 权限变化（Unix）。lock 是要提交进 Git 的文件。 |
| 8 | `lock.Write` 全部调用点 | 写前**从不 `Validate()`** → 非法 lock 能落盘（与 #6 叠加）。 |
| 9 | `resolve/normalize.go:220-232` | 剥离 userinfo 用 **`LastIndex("@")`**：`https://evil.example.com/a@github.com/org/repo` → 解析为 `github.com/org/repo`。 |
| 10 | `vendor/mirror.go:28` | 注释声称"跨进程并发由 .lock 文件（M3 引入）防护"，**实现里只有进程内 `sync.Mutex`**，全仓库无 `O_EXCL`/flock。注释与实现脱节。 |
| 11 | `security/permissions.go:236` | **`CheckWrite` 全仓库零调用** → `write:` 命名空间是装饰性的。而该文件开篇写着"一个只被解析、不被执行的权限配置比没有更糟"。自我矛盾。 |
| 12 | `supplychain/osv.go:183,197-210` | ① `http.DefaultClient` **无客户端级超时**，卡住的 HTTPS 可无限挂起（这是审计门禁路径）；② 响应解析把 `severity` 强假设为 `string`，而 OSV 现行 schema 是**对象数组** → 真实响应会整体解析失败报 exit 4，且测试用的是自造 payload，**未对真实 API 验证过**。 |
| 13 | `config/config.go:313-352` | `mergeProject` **原地修改入参** → `Resolved.Effective` 与 `Resolved.Project` **是同一指针**，与 `config.go:175-189` 的文档注释矛盾；`mergeGlobal` 是**空操作**（全局层在 config 包内根本没参与合并，实际生效在 `cmd/ngm/engine.go:143` 与 `env.go:71`）。 |
| 14 | `mappings/validate.go:102` vs `schema.go:86-96` | 全局 vendor 模式下 `to` 是绝对路径，会被自己的校验判为 fatal。`ToJoin` 明明支持绝对路径。自相矛盾。 |
| 15 | `content_maintain.go:341-379` + `content_v2.go:242` | **`Prune` 与 `Put` 争抢 `.unpack-*` 前缀**。`Put` 的临时清单目录就建在 `treesRoot` 下且用同一前缀，而 `Prune` 无条件 `RemoveAll` 所有 `.unpack-*`，**无年龄门槛、无进程锁**。并发 `install` + `prune` → 活动临时目录被删 → install 报错。（`PruneOrphans` 有 24h 门槛，`Prune` 没有。） |
| 16 | `content_maintain.go:428-432` | `--older-than=0` 被允许（只拒绝负数）→ 24h 安全窗完全失效，会删掉"刚写入、尚未被清单引用"的 blob。 |
| 17 | `verify.go:466-470` + `integrity.go:175-183` | **默认（非 `--deep`）verify 对等长字节篡改完全盲**：浅校验只比 size 与结构，而 hardlink 模式下 vendor 与 blob 是同一 inode，篡改同时改变两侧。CI 默认跑 `ngm verify` 拿不到字节级保证。 |
| 18 | `internal/verify/` | **969 行 0 单测**（见 §1.1）。 |

### P3 — 清理 / 加固

- `git/auth.go:79` `AssertNoSecrets` 写好**从未接线**；`clone.go:68` `FetchMirrorTags` 死代码（注释说的问题实际未处理）
- `Mirror.Ensure` 对**已存在** mirror 也 fetch → 每次 install 都触网，与 ADR-010 的"不预先 fetch"哲学不一致
- `git/tree.go:130` 与 `:164` 重复了"跳过 tree 条目"的过滤条件，改一处漏一处会**静默产生错误的 digest**
- `install` / `update` 两个写命令**没有 `--json`**（CI 最需要的恰好没有机器可读输出）
- **55 处裸 `return 3`** 绕过整个错误模型（不产错误信封、不产 `hint:`）
- `cmd/ngm/store.go:224` 用**字符串比较 Duration** 判断用户是否改过默认值
- 注释密度极高（如 `spawn.go` 88 行里 55 行注释）——可追溯性强，但已出现至少 3 处"注释与代码不符"（#10、#13、`root.go:459` panic 退 6 而注释写 1）

---

## 4. 竞品全维度对比（2026-10 现状）

### 4.1 一张表

| 维度 | npm | pnpm | Yarn(Berry) | Bun | Deno | **ngm** |
|---|---|---|---|---|---|---|
| **定位** | 默认 registry | registry + 省盘 | registry + PnP | 一体化 | 运行时+安全默认 | **Git 依赖证明层** |
| registry 包管理 | ✅ | ✅ | ✅ | ✅ | ✅(JSR) | ❌ 明确非目标 |
| **Git 依赖** | ✅ | ✅ | ✅(审批) | ✅ | ✅ | ✅ **主战场** |
| refType 声明 | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ **必填（独有）** |
| commit 锁定 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| 内容摘要 | checksum | integrity | checksum | integrity | integrity | ✅ **规范清单 digest** |
| **ref 漂移复核** | ❌ | partial | partial(lock 一致性) | ❌ | ❌ | ✅ **三态区分（独有）** |
| **Git 依赖漏洞扫描** | ❌(面向 registry) | ❌ | ❌ | ❌ | ❌ | ✅ **按 commit 查 OSV（独有）** |
| postinstall 控制 | partial | ✅ | partial | partial | ✅ 默认 deny | ✅ 默认 deny + 沙箱 |
| minimumReleaseAge | ✅ | ✅(**v11 起默认 1440min**) | ✅ | ✅ | ✅ | ✅ |
| 仓库白名单 | ❌ | ❌ | ✅ | partial | ❌ | ✅ |
| 内容寻址 store | ❌ | ✅ | ✅ | ✅ | ✅ | ✅(v2 blob 池) |
| **store 回收** | — | ✅ `store prune` | ✅ | ✅ | ✅ | ⚠️ 仅残骸 + 孤儿（无可达性） |
| 离线安装 | ✅ cache | ✅ | ✅ | ✅ | ✅ | ✅ **content store 命中时零 git** |
| 确定性 lock | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ **字节级（除 resolvedAt）** |
| SLSA 等级 | **L3**(Sigstore 默认 provenance) | L1 | L1(hardenedMode) | L1 | L1 | **L1+（不同轴）** |
| **文件级签名/attestation** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌（ADR-014 决定不做） |
| 安装速度(无变更) | 慢 | 快 | 快 | **~12ms** | 快 | 11ms(10 依赖) |
| bundler / HMR / test / LSP | 外置 | 外置 | 外置 | ✅ 内置 | ✅ 内置 | ❌ 明确非目标（adapter 调外部） |
| 生态采用 | **~55% 份额** | 高 | 中 | 增长 | 中 | **0 star / 0 fork / 0 外部用户** |
| 多语言 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌（路线图列为"后续探索"） |

### 4.2 🔴 最重要的新事实：PackageGate（2026-01）

**ngm 的 `docs/COMPETITIVE-ANALYSIS.md` 事实截止 2026-09-29，但完全没有提到它。**

> 2026-01-26，Koi Security 披露 **PackageGate**：npm / pnpm / vlt / Bun 共 6 个零日。
> 其中 **npm 那一条正是"Git 依赖的恶意 `.npmrc` 覆盖 git 二进制路径 → 绕过 `--ignore-scripts` 实现 RCE"**。
> pnpm（2 周，2 个 CVE）、vlt（8 天）、Bun（3 周）都修了。
> **npm 以 "works as expected" 关闭了报告**，并表态"用户有责任审查自己安装的包"。

这件事对 ngm 的意义，比过去 53 个版本的任何一条都大：

1. **npm 官方认定的威胁模型，就是 ngm 的产品假设**——"Git 依赖是一条被低估的攻击面"。
   而全球最大的包管理器**明确拒绝修**。
2. ngm 在这一点的设计上**天然免疫**：不读依赖内的 `.npmrc`（它根本不解析 npm 配置）、
   不执行依赖内的脚本（postinstall 只跑沙箱内 JS 钩子）、git 调用走唯一构造点且受 `run:git` 门禁。
3. 但 **ngm 的文档里没有这句话**，README 里也没有，竞品对比里也没有。

> **一个把全部赌注押在"Git 依赖安全"上的项目，错过了 9 个月前发生的、由对手官方背书的
> 最强验证。** 这是本次评估里最可惜的一条。

### 4.3 与 SLSA 的关系（项目自己的判断是对的，但可以更进一步）

- npm 已是 **L3**（`npm audit signatures` + Trusted Publishing 默认附 provenance）
- **provenance 与 archiveDigest 正交**：前者证明"谁、哪次 CI、哪个 commit **构建**的"，
  后者证明"这份**内容**是不是锁定的那个 commit"。**tag 重打、branch 前进发生在构建之前，provenance 一概看不见。**
- ngm 在 SLSA 轴上的天然位置是 **L2 的一半**：有"内容 ↔ commit"绑定（强于 L1），
  但**没有签名**，拿不到 L2 要求的"由已知来源构建"的可验证部分。
- **建议**：接一层 SLSA 谓词（给 manifest 加 attestation），而不是自造签名体系。
  ADR-014 决定不做"自证文件签名"是对的，但那不等于不做 attestation——这两件事被混为一谈了。

---

## 5. 优缺点（清晰版）

### ✅ 优点

1. **工程纪律是同类早期项目里少见的。** 不是"写了测试"，是把纪律写成了**会过期的判据**：
   - git 进程构造点唯一 + 文本级元测试扫描 `exec.Command`（"规则写在第二个地方，一定会有一处不一致"）
   - spawn 预算门禁，且**带下界**（防止"计数器坏了永远返回 1"让门禁一路绿）——仪器说谎比没有仪器更糟
   - 配置字段接线表（48 个字段逐一登记覆盖它的测试函数，且断言那个测试函数**真的存在**）
   - 覆盖集**必须派生自源码**，不许手抄（v0.43/45/46 连续三版栽在这上面，然后把它立成了纪律）
   - 每条判据都有"牙齿"验证（故意退回修复，确认它会红）
2. **诚实是结构性的，不是姿态。** 公开 20× 的坏消息、把 3s 目标降级为观测值（因为跨机器方差 ≥ 收益）、
   记录"两条性能目标未达标"、写"四类自嗨风险"自检表。多数项目不会这么写。
3. **真正独有的三项能力**：`refType` 必填、ref 漂移三态、按 commit 查 OSV。在主流工具里确实无对应物。
4. **确定性做得很实**：lock 字节级一致（除 `resolvedAt`）、并行但按索引写回保证结果顺序无关、
   LFS 报错取最小索引保证跨机器同一条路径。
5. **依赖极简**：`go.mod` 只有一个 `wazero`。无 cobra、无 git 库，全部 `os/exec` 调 git——
   认证完全交给 git，ngm 从不触碰 token。
6. **v0.8 的存储重构成果实在**：20.0× → 0.6×，且 `blobKey` 掺 mode 这个设计解决了 hardlink 无法承载可执行位的硬问题。

### ❌ 缺点

1. **采用度是 0。** 0 star / 0 fork / 无外部用户 / 单人维护。技术账算得再细也不改变"没有人用过它"。
2. **过去 53 个版本里 29 个"产品代码 0 行改动"。** v0.13→v0.65 几乎全是在给已有表面加契约网。
   网**确实**抓到过真缺陷（v0.40 的 `--` 重排、v0.41 的 `--dir=`、v0.44 的死赋值、v0.62/63 的误判），
   但边际收益已经明显递减——v0.64 修的是"某文档缺了自己的行"，v0.65 修的是"矩阵的某一列没有自己的证据"。
   **项目在给自己做体检，而不是给用户做产品。**
3. **文档（28,406 行）比产品代码（22,995 行）还多。** 且 197 份文档分布在 `docs/` 七个子目录，
   项目自己在 v0.12 就写了"一个人接手时读不到它"——两年（65 版）后这个判断依然成立，只是更严重了。
4. **核心承诺有可戳的洞**（见 §3）：`run:git` 门禁有一处绕过、symlink 无逃逸校验、
   默认 verify 对字节篡改盲、store 无 fsync。**对一个卖点是"可证明"的工具，这几条尤其要命。**
5. **性能路径没优化完**：install 每依赖 6 次 spawn 且 digest 算两遍；图解析跨层重复解析（含真实 fetch）。
   而 `verify` 已经做到 2/3 次并有门禁——**两条路径的工程投入不对称**。
6. **CLI 层复制粘贴严重**：`installFresh` 与 `update` 的核心循环几乎逐行重复；
   "写 JSON + 检查写失败"在 5 个文件各写一遍（`engines.go` 已有 `writeJSON` 却没人用）；
   每个命令重复声明两遍 flag 表（20 命令 × 2 = 40 处）。
7. **非默认端口自建 Git 连不上**（`Canonical` 只存 Host+Path，加端口是 schema 级变更）。
   "静默"那一半已修（改为明确拒绝），但对自建 GitLab/私有 Git 的用户是硬阻断。
8. **没有 SBOM 导出、没有 attestation、没有签名**——而这三项正是合规场景（也是唯一愿意为可证明性付钱的场景）的入场券。
9. **`internal/verify` 969 行零单测**，`internal/observability` 847 行只有 2 个测试函数。

---

## 6. 下一步开发方向

### 判断前提

**问题不是"还能加什么能力"，而是"过去 53 版在做的事已经不产生新价值了"。**
建议明确结束 v0.13 以来的模式，把版本预算重新分配：**每 3 版至少 1 版有用户可见的能力增量。**

### 第 0 层 · 修掉动摇承诺的缺陷（1–2 天，立刻）

这 5 条不是"技术债"，是**产品质量问题**——一个卖"可证明"的工具，不能在这些点上出错：

1. `graph.go:380` 补 `Policy: opts.Policy`（闭合"没有绕过门禁的 git 调用"这条自己定的不变量）
2. `links.go` 落地前校验 symlink 目标与 path 不逃逸包目录（`filepath.Rel` + `IsLocal`），
   并在 verify 里把逃逸 symlink 报为 critical
3. lock 三连：写前 `Validate()`、拒绝短 commit、临时文件 `Chmod(0o644)`
4. `write:` 命名空间：要么给 `CheckWrite` 一个真实施加点，要么在文档与 `enforcement_test.go` 里标为"未实现"
5. `internal/verify` 补包内单测（三级检查 / 漂移分类 / 退出码优先级）

### 第 1 层 · 把 PackageGate 这张牌打出去（v0.66，1 周）

**这是本次评估里性价比最高的一件事，且几乎不需要写代码。**

- 把 PackageGate 写进 `docs/COMPETITIVE-ANALYSIS.md` 与 README 的"为什么用 ngm"：
  **npm 官方认定为 "works as expected" 的那条攻击路径，ngm 在设计上天然免疫。**
- 做一个**可执行的对照 demo**（一个脚本 + 一个 fixture 仓库）：
  同一份恶意 Git 依赖，在 `npm install --ignore-scripts` 下发生什么、在 `ngm install` 下发生什么。
  **能被跑一遍的对比，比任何一张表都有说服力。**
- README 首页加一节"**什么时候你不该用 ngm**"的反面——"什么时候你**该**用"：
  只用 npm 包 → 不该用；有私有 fork / 需要追溯上游 commit / 要交付审计材料 → 该用。
  现在 README 有"为什么你可能不应该用"，**没有"为什么你应该用"**。

### 第 2 层 · 性能（v0.67，1–2 周）

- install 路径：让 `Put` 接受调用方已算好的 digest / 复用已列出的 tree → **6 spawn → 2**
- `mergeFrontier` 先查 `seen` 再解析 → 消除跨层重复解析（每次含一次真实网络 fetch）
- 这两条是**量级改进**（2–3×），而且已经有现成的仪器（spawn 预算）可以证明它

### 第 3 层 · 打开唯一愿意付钱的市场（v0.68+）

合规/审计场景是 ngm 唯一的付费场景，而它现在缺入场券：

- **SBOM 导出（SPDX / CycloneDX）**——lock 里已经有 commit + digest + 来源链，导出几乎是免费的
- **给 manifest 加 SLSA attestation**（不是自造签名，是接一层谓词）
  → 把 ngm 从"L2 的一半"推到"内容侧的 L2"，且与 npm 的 L3 **互补而非竞争**
- `ngm verify --json` 的报告直接可被 CI 策略引擎消费（现在是报告，不是证据）

### 第 4 层 · 明确停止

- **停止新建契约网。** 已有 32+ 张，覆盖 21 命令 × 8 配置形状 × 参数维度 × 退出码 × 双通道。
  它抓到的最后一条产品缺陷是 v0.55；之后 10 版抓的全是**文档与判据自己的问题**。
- **停止补发历史版本**（已暂缓，维持）——正确的判据是"有没有人在等这些二进制"，答案是没有。
- `remote` adapter 保持搁置（ADR-020 的结论仍然成立）。

### 第 5 层 · 唯一真正能回答"值不值得"的事

**找一个真实用户。** 技术侧的账已经算得足够细，再加一个门禁的边际收益低于一个真实用户。
这一条从 v0.12（45 个版本前）就写在"下一步"里，至今没有变——**它就是那个没有被做掉的第一优先级。**

---

## 7. 与项目自评的差异（供对照）

| 项 | project-state.md 的说法 | 本次复核 |
|---|---|---|
| 阻塞 | "采用的阻塞是许可证/元数据" | 许可证已改 MIT（v0.12）；**真正的阻塞是没人知道它存在**，且竞品对比缺了 PackageGate 这张牌 |
| 下一步 | "填 description/topics" → "找真实场景" | 方向对，但**排在前面 5 条 P1 缺陷之后**——带着"symlink 可逃逸 / 默认 verify 对字节篡改盲"去找用户，风险很高 |
| 覆盖 | 未提 | **`internal/verify` 969 行零单测**是最大的覆盖缺口 |
| 竞品 | 事实截止 2026-09-29 | **缺 PackageGate（2026-01）**——9 个月前发生的、对手官方背书的最强验证 |
| 进度 | "65 版全部交付" | 交付是真的，但 **29/53 版产品代码 0 行改动**，模式本身需要被叫停 |

---

## 相关文档

- [项目状态评估](./project-state.md)（项目自评）· [能力矩阵](./capability-matrix.md) · [指标](./metrics.md) · [路线图](./roadmap.md)
- [竞品对比分析](../COMPETITIVE-ANALYSIS.md)
