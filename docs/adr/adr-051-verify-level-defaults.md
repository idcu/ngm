# ADR-051：`verify` 的默认级别——CLI structure，CI action content

- **状态**：已定（2026-11-21）
- **范围**：`ngm verify` 的 CLI 默认级别；GitHub composite action 的 `verify-level` 默认
- **依据**：[竞品全维度差距评估 2026-11-21 §4.3 #13](../analysis/ngm-competitive-gap-full-2026-11-21.md) ·
  [competitive-followup spec B2](../specs/assessment-2026-11-21-competitive-followup/spec.md)

> **结论**：CI action 的 `verify-level` 默认改为 **content**（重放 digest + 逐文件哈希）；
> `ngm verify` 的 CLI 默认**保持 structure**（只比路径/符号链接/大小，不读字节）。
> 两者不同是有意设计：CI 是供应链关键路径，值得为字节校验付出成本；本地开发循环要轻。

## 决策

1. **action 默认 content**：GitHub Actions 是产物进入发布/部署的关口，
   一次 tampering 在这里被发现的价值远大于逐文件哈希的成本（100 依赖约 2s 的 content 校验可接受）。
2. **CLI 默认 structure 不变**：改 CLI 默认对所有本地用户是破坏性变更，且本地循环频繁、
   冷 mirror 时 content 会因"资源缺失"退 exit 4——那会把"本地快速反馈"变成噪音。
   用户在需要字节保证时显式 `--content` / `--level=content`。
3. **双向可见**：`action.yml` 的 description 与 bash 注释点明"CLI 默认 structure"；
   `verify --help` 与 CLI 文档保留 structure 说明。选项 (a)"CLI 也改 content"被否——
   理由不是风险，是它把 CI 的强默认强加给了不该付这个成本的本地循环。

## 代价与边界

- 用户从 CLI 迁到 action 时，验证强度会"意外变强"：原来 structure 通过的项目，
  在 action 默认下可能因字节不符而 exit 2。这是**发现真问题**，但要在 CHANGELOG 与
  action description 里写清，避免被当成回归。逃生口：在 action 里显式 `verify-level: structure`。
- `--content` 与 `--level=structure` 同时给会被 ngm 拒绝（两种写法指向不同级别）——
  action 的 bash 段已处理：args 自带级别开关时不注入默认值，用户显式写法赢。

## 判据

- `TestV140ActionVerifyLevelDefaultsToContent`（静态检查 action.yml 默认值 content +
  说明里点出 CLI 默认 structure + verify.go 的 CLI 默认仍是 structure）；变异（默认值改回 structure）红。
- `verify --help` golden 保持不变（CLI 未改）。

## 相关文档

- [ADR-050 默认晾晒期](./adr-050-default-release-age.md)（同批的默认安全姿态决策）
- [竞品差距评估 §4.3](../analysis/ngm-competitive-gap-full-2026-11-21.md)
- [CLI 参考 · ngm verify](../guides/cli.md)
