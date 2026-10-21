# ADR-035：refType 的窄默认（仅完整 commit hash）

- **状态**：已定
- **日期**：2026-10-20
- **范围**：`ngm.json`、上游 `ngm.json`、`ngm add`
- **修订**：[ADR-004](./adr-004-reftype.md)

> **结论**：`refType` 只在一种语法上无歧义的情况下可以省略：`ref` 是完整 40 位十六进制 commit hash。此时按 `commit` 处理并写回声明。tag、branch、7–39 位短 hash 仍然必须显式写明 `refType`。
>
> **代价**：`ngm.json` 的“声明自描述”从“所有字段必填”变成“一个窄例外”；解析器、`add`、上游清单三处必须使用同一条判据，否则会出现“本地能写、远端读不了”的分叉。

---

## 背景

ADR-004 的原则是“声明用 refType，锁定用 commit”，`refType` 必填，ngm 不猜。这个原则避免了 `v1.2.3` 既是 tag 又是 branch 时的静默误判。

但完整 40 位 hash 不存在这个歧义：它只能是 commit。要求用户为它再写一次 `refType=commit` 只增加仪式，不增加信息。AI 生成的清单、快速测试的 commit 引用尤其容易漏这个字段。

## 决策

1. **窄默认**：`ref` 匹配完整 40 位 hex 且 `refType` 为空 → 视为 `commit`，并写回声明。
2. **其余仍必填**：tag、branch、7–39 位短 hash、空 ref → 缺少 `refType` 仍然报错。
3. **三处同一判据**：
   - `internal/config` 的 `Dependency.Validate`；
   - `internal/resolve` 的上游清单解析；
   - `cmd/ngm/add` 的未给 `--ref-type` 分支。
   判据只有 `resolve.IsFullCommit` 一个实现。
4. **lock 永远显式**：lock 里不写“默认值”，始终记录解析后的 `refType`。
5. **不做全局默认**：不把“空 refType”默认成 commit；那会把最常见的 tag/branch 错误变成“commit 长度错误”。
6. **不做隐式 tag/branch 推断**：`add` 不为了少打 flag 去 `ls-remote`；证据式推断可以作为未来的 `--infer-ref-type` 选项，但不改变 schema。

## 后果

- 完整 commit 的声明更短；tag/branch 仍需明确意图。
- `ngm add <repo>@<40-hex>` 不再要求 `--ref-type`。
- 错误提示必须说清“只有完整 40 位 hash 才可省略”，避免用户误以为 tag 也可以。
- 如果未来支持 SHA-256 仓库的 64 位 commit，需同步扩展 `IsFullCommit` 与本文；在此之前，64 位仍要求显式 `refType`。

## 相关文档

- [ADR-004：refType 声明](../adr/adr-004-reftype.md)
- [配置详解](../guides/configuration.md)
- [模块集合架构](../architecture/module-collections.md)
