# ADR-053：晾晒期全局默认与信任降级防护

- **状态**：已定（2026-11-24）
- **范围**：`supplyChain.minimumReleaseAge` 的默认值与关闭方式；锚点强度不得降级的策略
- **依据**：[竞品全维度差距评估 2026-11-24 §6 P0 前两项](../analysis/ngm-competitive-gap-full-2026-11-24.md)
- **与 ADR-009 / ADR-050 的关系**：**只对 `minimumReleaseAge` 的默认值覆盖 ADR-009 的"空策略底线"**；
  ADR-050 的"写了段才默认 24h"被本 ADR 推进为"全局默认 24h"。其它门禁（白名单、OSV 严重级别、
  postinstall、verifyOnLock）在"没配 supplyChain 段"时**仍按 ADR-009 放行**。

> **结论**：`FromConfig(nil)` 现在也返回 `minAge = 24h`（pnpm 11 / Deno 2.9 的 2026 默认姿态）。
> 项目级关闭必须**显式**写 `minimumReleaseAge: "PT0S"`——这不是"配了没生效"，而是用户主动
> 承认放开晾晒期；`P0D` 仍被 `ParseISODuration` 拒绝，保持既有语义。单命令逃生口仍是
> `--allow-fresh`（`WithoutMinimumAge()` 副本，不泄漏）。另新增 `trustPolicy: no-downgrade`：
> 锚点强度 commit/dirDigest > tag > branch，只拦"从强退到弱"，升级与同级放行。

## 决策

1. **默认值的作用域**：只有 `minimumReleaseAge` 的默认值越过 ADR-009 的空策略底线。
   `IsEmpty()`（没有显式 supplyChain 段）不再等于"所有门禁都不生效"的判据；
   但其它门禁仍只在显式配置时执行，避免把"默认安全"扩张成"默认全拒"。

2. **关闭方式必须是显式动作**：`PT0S` 在 `supplychain.FromConfig` 里特判为 0；
   `P0D` 与其它零时长仍按 `ParseISODuration` 拒绝。理由与 ADR-009 一致：
   用户写下"0 秒"却没生效，比没有这个字段更危险。

3. **CLI 的落地形态（已接线）**：Policy API 层的全局默认是 24h（V211），
   但 `checkReleaseAges` 仍按 `AgeGateActive()` 执行——只有项目显式写了 supplyChain
   段才强制门禁。`ngm doctor` 对没有 supplyChain 段或没写 minimumReleaseAge 的项目
   给出 warn，提示补上 `P1D`；不把"未配置的项目已被 CLI 保护"写成事实。
   没有让 `ngm init` 默认写 P1D：全仓库有 176 处测试安装调用都用刚提交的 commit，
   默认写入会把无关判据全部打红，而 doctor 提示已经满足"显式告知"的目标。
   若将来切换 CLI 强制范围，先统一夹具加 `PT0S` / `--allow-fresh`，再改判断。

4. **信任降级防护**：`Policy.trustPolicy` 取 `""`（关闭）或 `no-downgrade`。
   `CheckAnchorNoDowngrade(oldRefType, newRefType)` 是纯比较：commit / dirDigest（local-dir）
   强于 tag，tag 强于 branch；空类型、local-git（单看类型判不出强度）视为未知并放行。
   只拦降级，不要求每次 update 都升级。旧 lock 条目缺失即放行（新依赖没有历史强度可比）。

5. **门禁时机与失败语义**：在 `update` 图解析**之后**、写新 lock **之前**，用 `previous` lock
   按 `name#subPath` 建索引与 `g.Nodes` 同键比较。命中返回 `CodeConfigInvalid`（exit 3），
   message 点明"哪个依赖从什么退到什么"，hint 给出"固定到 commit/tag 或显式关闭 trustPolicy"。
   与 ADR-009 的策略拒绝同口径；不把 provenance 当作内容可信证明。

6. **config 层字段（已接线）**：`SupplyChainConfig` 增加
   `TrustPolicy string \`json:"trustPolicy,omitempty"\``，`Validate()` 限定取值
   `"" | "no-downgrade"`，`FromConfig` 从该字段填充 `Policy.trustPolicy`；
   `Validate()` 对 `PT0S` 开白名单。`ParseISODuration` 仍拒绝其它零时长（保持 P0D 语义）。
   `WithTrustPolicy()` 保留作为显式副本入口（测试与一次性策略变更），不再是唯一开启方式。

## 代价与边界

- 破坏性变更（Policy 层）：`FromConfig(nil).MinimumReleaseAge()` 由 0 变 24h；
  依赖该值判断"是否门禁"的调用方必须改用 `AgeGateActive()` / `MinimumReleaseAge() > 0`。
- 晾晒期的时间源仍是 committer date（可被回填），挡的是"刚发布就被撤回"的窗口，不是蓄意攻击者。
- 降级防护只比较 RefType 强度，不比较 digest；内容变化仍由 digest / verify 门禁负责。
- `local-git` 的强度无法由类型判定，一律放行；若要严格化需改成比较实际 ref 解析结果。

## 判据

- `TestV211DefaultReleaseAge`：无段=24h、有段无字段=24h、PT0S=0、P0D 仍拒绝、`--allow-fresh` 不泄漏。
- `TestV212CheckAnchorNoDowngrade`：commit→branch / local-dir→tag / tag→branch 红；升级、同级、未知类型绿；策略关闭绿。
- `TestV212TrustNoDowngradeGate`：update 接线四分支（commit→branch 红、branch→commit 绿、
  策略关闭绿、旧条目缺失绿）与 subPath 键隔离。

## 相关文档

- [ADR-009 供应链策略](./adr-009-supply-chain-policy.md)（除默认值外仍然有效）
- [ADR-050 默认晾晒期](./adr-050-default-release-age.md)（被本 ADR 推进为全局默认）
- [ADR-052 本地来源](./adr-052-local-sources.md)（dirDigest 作为强锚点）
