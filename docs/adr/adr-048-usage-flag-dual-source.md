# ADR-048：usage 常量与 flag 声明双源的容忍度

- **状态**：已定（暂不合并）
- **日期**：2026-11-20
- **范围**：`cmd/ngm` 的 usage 文本与 flag 声明
- **依据**：[全维度评估 2026-11-20 §2.3](../analysis/ngm-full-assessment-2026-11-20.md)

> **结论**：当前**不**把 usage 常量与 flag 声明合并为单一事实源。二者双源是已知的小债，
> 但 golden 测试已经兜住"改了忘改"；合并会重写全部 golden 且带来新的生成层，收益没有证据。

## 背景

`verifyUsage` 单条 105 行，flag 在各自的 `newFlagSet` 里声明。评估报告指出"flag 改了忘改 usage
只能靠 golden 兜底"。但"靠 golden 兜底"在 ngm 里是一种被验证过有效的机制：
`TestHintsNameFlagsTheCommandAccepts` 用 AST 从源码派生每条命令的 flag，
检查错误提示里出现的 `--xxx` 必须是该命令真正接受的名字——双向核对（含过期登记的反查）。
也就是说，双源之间的漂移**有判据在守**，不是一个无人看管的角落。

把 usage 改为从 flag 声明自动生成，需要引入模板/生成步骤、处理中英混杂的现状，
并重写全部 golden；这是一次大改动，而它要防的事已经有低成本守卫。

## 决策

1. **维持 usage 常量与 flag 声明双源**，不引入生成层。
2. **触发条件（任一成立）**：
   - golden 漏过一次**真实**的 usage/flag 漂移（用户按提示用了被拒的 flag 并造成后果）；
   - 新增命令的数量使 AST 派生守卫的维护成本显著上升（给出具体命令数与维护工时）。
3. 在此之前，`TestHintsNameFlagsTheCommandAccepts` 是这道债的守门人，不得削弱。

## 后果

- 保留一处可控的小债，避免一次无证据的大改。
- "提示指向真实 flag"继续由 AST 判据保证，而非靠人记住两处同步。

## 相关文档

- [全维度评估 §2.3](../analysis/ngm-full-assessment-2026-11-20.md)
- [ADR-047 拆包触发条件](./adr-047-package-main-size.md)（同类的"暂缓 + 触发"处理）
