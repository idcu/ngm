# ADR-050：`minimumReleaseAge` 的默认值（P1D，但完全未配策略仍不门禁）

- **状态**：已定（2026-11-21）
- **范围**：供应链策略的晾晒期默认值；`install` / `update` 的命令行为
- **依据**：[竞品全维度差距评估 2026-11-21 §4.3 #13](../analysis/ngm-competitive-gap-full-2026-11-21.md) ·
  [competitive-followup spec B1](../specs/assessment-2026-11-21-competitive-followup/spec.md)
- **与 ADR-009 的关系**：**补记，不推翻**。ADR-009 的"没配策略=不门禁"在本 ADR 里被精确化为
  "**完全没有 `supplyChain` 段**=不门禁；写了段但没写冷却=P1D"。

> **结论**：`supplyChain.minimumReleaseAge` 的默认值改为 **P1D（24 小时）**——
> 与 pnpm 11+（`minimumReleaseAge=1440`）和 Deno 2.9（默认 24h）的 2026 默认对齐，
> 理由是**独立的供应链安全理由**（新版本投毒在数小时内被发现和撤回，晾晒期让攻击来不及进入），
> 不是"更像 npm"（ADR-044 准入规则）。逃生口有两层：**完全不配 `supplyChain` 段**对项目关闭
> （ADR-009 的可见配置哲学）；`--allow-fresh` 对单次 install/update 放行。

## 决策

1. **默认值 P1D 的触发条件**：仅当项目写了 `supplyChain` 段但**没有**写 `minimumReleaseAge` 时生效。
   **完全没有 `supplyChain` 段**（`sc == nil`）时仍返回空策略、不门禁——这是 ADR-009 的底线承诺：
   "把没配策略当作全部拒绝会让所有既有项目瞬间装不上"。两者区别在配置里是可见且可解释的。
2. **不追溯已锁 commit**：门禁只在 install / update **解析新 ref 之后**对新图执行（`checkReleaseAges`）。
   已锁在 `ngm.lock` 里的 commit 只要 reinstall 走 lock 路径（不重新解析），就不受默认门禁影响；
   用户主动 `update` 一个新提交的依赖才会被晾晒期判定——这正是期望行为。
3. **逃生口**：
   - 项目级：**完全不写 `supplyChain` 段** → 不门禁（ADR-009 底线：没配策略不挡任何项目）。
     不用 `P0D` 当开关——`ParseISODuration` 有意拒绝零时长（"配了却不生效"的错觉），
     这个既有安全决定不改。
   - 单命令：`ngm install --allow-fresh` / `ngm update --allow-fresh` → 本次解析跳过晾晒期
     （`Policy.WithoutMinimumAge()` 副本，白名单等其他门禁不变，不泄漏回原策略），
     并在 stderr 打出 warning；用于紧急修复且未被 `allowlistRepos` 覆盖的场景。
4. **白名单优先级不变**：`allowlistRepos` 显式放行的仓库仍不受晾晒期约束（ADR-009 的紧急通道）。

## 代价与边界

- **破坏性变更**：写了 `supplyChain` 段的既有项目，其新解析的依赖现在要等 24h。
  必须有 CHANGELOG `Unreleased` 段与帮助文本说明，并提供 `P0D` / `--allow-fresh` 两个逃生口。
- **时间源不可信**：committer date 由提交者控制可回填（ADR-009 已记这一局限）——晾晒期挡的是
  "刚发布就被发现撤回"的窗口，不是对抗蓄意回填时间的攻击者。
- **不门禁的场景**：完全没 `supplyChain` 段的项目、`--allow-fresh`、白名单仓库。

## 判据

- `TestV139MinimumAgeDefaultsToP1D`：有段无字段 → 24h；无段 → 0（ADR-009 底线不变）。
- `TestV139AllowFreshBypasses`：新 commit <24h 默认拒、`--allow-fresh` 放行。
- `TestV139LockedCommitsNotRetroJudged`：已锁 commit 走 lock 路径不被重判。
- 变异：把默认值改回 0 → 红；去掉 `--allow-fresh` → 红。

## 相关文档

- [ADR-009 供应链策略](./adr-009-supply-chain-policy.md)（本 ADR 精确化其默认承诺）
- [ADR-044 独立生态](./adr-044-independent-ecosystem.md)（独立安全理由，非兼容目标）
- [竞品差距评估 §4.3](../analysis/ngm-competitive-gap-full-2026-11-21.md)
