# usage 常量与 flag 声明双源 · 成本审计（2026-11-21）

> **状态：审计报告，未合并**。依据 [ADR-048](../adr/adr-048-usage-flag-dual-source.md) 的触发条件与
> [competitive-followup spec §3.3](../specs/assessment-2026-11-21-competitive-followup/spec.md)。
> "不怕风险"不等于"触发条件达成"——本轮只量化"合并的收益 vs 成本"，不重写 golden。

## 0. 结论

**当前不达到 ADR-048 的触发条件**（golden 漏过真实漂移 / AST 守卫维护成本显著上升）。
双源有守卫：`TestHintsNameFlagsTheCommandAccepts` 用 AST 从源码派生每条命令的 flag，
`help_golden` 守文本——"flag 改了忘改 usage"会红。合并为单一事实源**收益低、成本高**（重写 9 个 golden），
且引入生成层本身是新债。**建议维持双源**，触发条件达成再动。

## 1. 实测读数

| 项 | 读数 | 采集命令 |
|---|---|---|
| 含 Usage 常量的文件 | **29** | `grep -rl 'Usage = \`'` |
| 顶层命令 | **28** | `grep -cE '^\s*\{Name:' cmd/ngm/root.go` |
| `verifyUsage` 行数 | **79** | `awk '/const verifyUsage = \`/,/^\`/' cmd/ngm/verify.go \| wc -l` |
| golden 文件 | **9** | `find . -name '*.golden' -o -name '*golden*' \| wc -l` |
| AST flag 守卫 | **1 个主判据**（`TestHintsNameFlagsTheCommandAccepts`）+ 过期登记反查表 | `cmd/ngm/flag_hint_guard_test.go` |

> **口径纠正**：旧 ADR-048 写 `verifyUsage` "105 行"，实测 **79 行**（历史快照漂移，以本报告为准）。

## 2. 成本/收益量化

| 方案 | 改动量 | 重验判据 | 引入的新成本 |
|---|---|---|---|
| A. 维持双源（现状） | 0 | 守卫已在册 | 每加 flag 要同步 usage（靠 golden 兜底，成本约 1 行/次） |
| B. usage 从 flag 声明自动生成 | 29 个 Usage 常量改造 + 引入模板/生成层 + **重写 9 个 golden** | `help_golden`、`TestHintsNameFlagsTheCommandAccepts`、`TestV26` | 生成层是新抽象；中英混杂文本要迁移；生成结果漂移时难定位 |

**收益**：消除"忘改 usage"这一类漂移——但它**已经被 AST 守卫 + golden 双层兜住**，
11-20/11-21 的评估没有一起真实逃逸的 usage/flag 漂移。收益没有证据支撑。

## 3. 触发条件现状（ADR-048 两条逐条核对）

| 条件 | 当前状态 |
|---|---|
| golden 漏过一次真实 usage/flag 漂移（用户按提示用了被拒 flag 并造成后果） | **未发生** |
| 新增命令使 AST 守卫维护成本显著上升（需具体命令数与工时） | **未达成**：28 命令 / 1 个守卫，维护点稳定 |

## 4. 建议

维持双源。**重启条件**（写进本Report，下轮不必重开）：出现一起真实的 usage/flag 漂移逃逸事件，
或命令数增长使 AST 守卫的登记表达不到 28 条量级仍可维护时（给具体工时数字）再评估。

## 5. 相关文档

- [ADR-048 usage 双源容忍度](../adr/adr-048-usage-flag-dual-source.md)
- [全维度评估 2026-11-20 §2.3](./ngm-full-assessment-2026-11-20.md)
- [competitive-followup spec §3.3](../specs/assessment-2026-11-21-competitive-followup/spec.md)
