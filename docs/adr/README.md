# 架构决策汇总

> **结论**：这里是全部 ADR（ADR-001 ~ ADR-058）的唯一汇总文件。单篇 ADR 已随历史文档清理移出工作树，
> 历史正文用 `git log --oneline -- docs/adr/` 查；下表给出每条决策的**当前结论**。

---

## 如何使用这些决策

- 先按主题在表里找编号，读"结论"一列判断是否仍适用于你的场景；需要背景与代价时从 git 历史读当时的单篇。
- 决策是**快照**，不随实现频繁修改；协议、清单、接口的活文档在各自唯一事实源位置（能力矩阵、可观测性、架构文档）。
- 实现与决策冲突时，先在这里修订结论（补一句新结论或新增编号），再改代码；旧决策不删除，只在结论里写明被取代关系。
- 新增决策用下一个可用编号，一行一句话写清"选了什么"；不要创建对象，也不要在本表之外维护第二份清单。

---

## 决策汇总

| ADR | 决策 | 结论 |
|-----|------|------|
| ADR-001 | 为什么用 Go | 核心用 Go 写单二进制，宿主运行时只负责运行用户代码。 |
| ADR-002 | 为什么直接拉 Git 仓库 | 依赖直接来自 Git 仓库，不引入 registry 协议与 host API。 |
| ADR-003 | 为什么纯 vendor 目录 | 依赖以纯 vendor 目录落地，便于审计、离线交付与 digest 重放。 |
| ADR-004 | 为什么声明用 refType 锁定用 commit | 声明层保留人类可读的 ref，锁定层记录不可变 commit。 |
| ADR-005 | 为什么引擎统一接口动态选择 | 引擎走统一接口、按清单动态选择，第三方实现永久优先。 |
| ADR-006 | 为什么 lytd 并入 ngm | lytd 的能力并入 ngm，不保留独立工具。 |
| ADR-007 | Node 还是 Deno | Node 与 Deno 都作为宿主运行时，core 不绑定任何 JS 运行时。 |
| ADR-008 | archiveDigest 的定义 | digest 是规范化内容清单（path / mode / blob-sha256）的 sha256，可本地重放。 |
| ADR-009 | 供应链策略的时机与失败语义 | 空 supplyChain 段不门禁，所有策略只绑锁定 commit 而非 tag/branch。 |
| ADR-010 | 在线 verify 的抓取策略 | 只读一次远端 ref 广播，对象按需才取，无变化时完全不 fetch。 |
| ADR-011 | wasm adapter 的运行时与 ABI | 用 wazero 执行 WASI preview1 命令模块，期待 JS 宿主的 wasm 不在范围内。 |
| ADR-012 | 沙箱与执行边界 | 用 Deno 的 default-deny 沙箱执行单一脚本，最小授权、无写权限、不派生进程。 |
| ADR-013 | remote adapter 的信任边界 | 不发布 remote adapter：源码要离开本机，产物无法被用户本地证明。 |
| ADR-014 | 依赖自述的签名检查 | 不自造签名机制，身份判定交回 Git 自身的签名与用户密钥配置。 |
| ADR-015 | commit 型依赖不做 ref 解析 | commit 型依赖 0 次 git 子进程，tag/branch 不变。 |
| ADR-016 | mirror 远端地址读 config | mirror 地址从 config 读取，形状不认识就回退 git 子进程。 |
| ADR-017 | remote adapter 的发布决策 | 仍不发布，剩余问题写成可判定门槛。 |
| ADR-018 | store 的回收与去重 | 不做按可达性自动删除，写入侧去重是长期解法。 |
| ADR-019 | 层 2 按文件内容寻址 | blob 池 + 树清单已实施，digest 定义不变，symlink 落地模式退化。 |
| ADR-020 | remote adapter 正式搁置 | 该 adapter 在代码里不存在，只有真实需求、本地算力证据或跨生态标准才重启。 |
| ADR-021 | symlink 落地模式 | 保留降级，不实现按需物化一棵 hardlink 树。 |
| ADR-022 | verify 性能目标 | 改为次数门禁加秒数观测，3 秒不再是验收目标。 |
| ADR-023 | 回收无人引用的 blob | 已实施按清单引用与门槛回收，清单读不出来时拒绝删除。 |
| ADR-024 | 解释输出必须有界 | why 默认最多 64 条路径、tree 默认最多 4096 个条目，达界必须明说，all 才解除。 |
| ADR-025 | 引擎选择不设内置默认 | typeCheck / typeDecl / css 刻意不预置，缺声明 exit 3，声明了没装 exit 5。 |
| ADR-026 | 加退出码 6 表示内部失败 | panic 与"结论有了却送不出去"已从码 1 分出，码 1 的四类策略来源一个不动。 |
| ADR-027 | attestation 接一层谓词 | 输出未签名的 in-toto Statement v1，签名交给用户与 CI 的标准工具。 |
| ADR-028 | 直接依赖边写进 lock | 每条依赖增加恒在的 requiredBy，why 离线零子进程，只存直接边。 |
| ADR-029 | 仓库地址收下回环 | 只收 localhost 与 127.0.0.1 两个字面量，其余按原规则拒绝。 |
| ADR-030 | mirror 的占用与整理 | store usage / prune / gc 已实施，拒绝 single-branch 与部分克隆。 |
| ADR-031 | 一仓多模块 | 模块身份是 repo + subPath，模块清单优先，不做 workspace 与 registry。 |
| ADR-032 | 模块级内容证明 | 模块条目用 moduleDigest + subtreeOid，根条目保留 archiveDigest，lock v2。 |
| ADR-033 | 抽离语言无关核心 | core 各包已落地，模块证明切到外部依赖，ngm 只保留双消费者场景。 |
| ADR-034 | module cache 与 mirror 的边界 | module cache 只服务 lock 钉住的 commit 与子路径，filter 不支持时明确回退。 |
| ADR-035 | refType 的窄默认 | 完整 40 位 hex 可省略 refType 并按 commit 写回，tag/branch/短 hash 仍必填。 |
| ADR-036 | 证据式 refType 推断 | 只在显式 opt-in 且远端证据唯一时推断 tag 或 branch，同名或缺失即失败。 |
| ADR-037 | 逐模块 publish 覆盖 | 显式 true/false 直接决定，nil 才跟随全局，不做短路合并。 |
| ADR-038 | 不自研 Git | 运行时 Git 只在单一子进程边界，当前只评估成熟的 Git backend。 |
| ADR-039 | verify 退出码优先级 | 已确证的篡改（2）压过可重试的网络失败（4），避免 CI 把篡改当抖动重试。 |
| ADR-040 | 从包管理器退回证明层 | adopt 只读已装依赖并出具内容证明，不接管安装。 |
| ADR-041 | 按当前项目 lock 的显式回收 | gc 默认 dry-run、apply 才删，只回收候选树独占的 blob，不做自动或跨项目 GC。 |
| ADR-042 | 为项目脚本开沙箱外执行面 | run 只编排用户自己的脚本，独立 run:script 闸门，退出码与信号原样透传。 |
| ADR-043 | vendored 依赖的 .bin shim 暂缓 | 不做：会从第三方内容创建新的可执行面，重启需要真实项目与威胁模型。 |
| ADR-044 | ngm 是独立生态 | npm/pnpm 只是迁移来源，不承诺 registry 协议、semver 求解与 node_modules 布局。 |
| ADR-045 | --json 的契约层级 | 字段、类型、版本、错误信封与退出码是契约，字节不是；产物字节是例外。 |
| ADR-046 | 过期 lock 默认拒绝导出 | sbom / attest 在声明与 lock 不一致时 exit 3，显式放行才写产物。 |
| ADR-047 | cmd/ngm 拆包的触发条件 | 暂不重构：规模本身不是触发条件，需要同类跨文件缺陷或可观测退化。 |
| ADR-048 | usage 与 flag 双源的容忍度 | 暂不合并：golden 与 AST 守卫已兜住漂移，先不增加维护成本。 |
| ADR-049 | 契约网冻结 | 停止新建判据，新判据必须先指着一个已发生的产品缺陷，存量作回归档案。 |
| ADR-050 | minimumReleaseAge 默认 P1D | 写了 supplyChain 段却没写冷却时默认 24 小时，完全不配置段仍不门禁。 |
| ADR-051 | verify 的默认级别 | CLI 默认 structure，CI action 默认 content，两者不同是有意的。 |
| ADR-052 | 通用来源与本地来源 | local-git / local-dir 复用可证明身份，schemaVersion 3、lockfileVersion 3.0.0，旧格式拒绝。 |
| ADR-053 | 晾晒期全局默认与信任降级防护 | init 写入 P1D，no-downgrade 拦住 commit 或 dirDigest 锚点向 tag/branch 降级。 |
| ADR-054 | adopt 的保守迁移与脚本只预演 | 只接受锁中显式 Git 锚点，无法映射的逐条 skipped，doctor 只列待审批脚本不执行。 |
| ADR-055 | 保守的迁移与只读 digest 反查 | migrate 只搬能证明的字段并备份，why 的 digest 反查只读不写。 |
| ADR-056 | 集合约束轻量版与显式补丁 | 只做共享 ref 一致性校验不做求解，patches 只换锚点并留 patchFrom / patchWhy。 |
| ADR-057 | 构建回执契约 | ngm.build.json 是旁车文件不进 lock，同输入同字节，不承诺跨工具可复现。 |
| ADR-058 | 引擎版本绑定与压缩 fail-closed | expectedVersion 不符 exit 5、expectedDigest 不符 exit 2，CSS 压缩能力不支持 exit 3。 |
