# `cmd/ngm` 拆包可行性测量（2026-11-21）

> **状态：测量报告，未拆包**。依据 [ADR-047](../adr/adr-047-package-main-size.md) 的触发条件与
> [competitive-followup spec §3.3](../specs/assessment-2026-11-21-competitive-followup/spec.md)。
> "不怕风险"是授权，**不是 ADR-047 的触发条件本身**——本轮只把"若拆，成本是多少"变成可复现数字，
> 不动 `package main` 的结构。所有数字附采集命令。

## 0. 结论

**当前不达到 ADR-047 的触发条件**（同类跨文件耦合缺陷 ≥2 次 / 约定失守造成用户可见缺陷 /
编译测试时间可观测退化）。但"不怕风险"授权下做了一次**隔离测量**：拆包成本是**中高**——
主要不在文件规模，而在报告渲染横跨 36 个非测试文件的输出调用。

触发条件若将来达成，按本报告 §3 的边界执行即可，不必重新测量。

## 1. 实测读数（命令可复现）

| 项 | 读数 | 采集命令 |
|---|---|---|
| 非测试 Go 文件 | **43** | `find cmd/ngm -maxdepth 1 -name '*.go' ! -name '*_test.go' \| wc -l` |
| 测试 Go 文件 | **185** | `find cmd/ngm -maxdepth 1 -name '*_test.go' \| wc -l` |
| 非测试行数 | **11,172** | `find ... -print0 \| xargs -0 cat \| wc -l` |
| golden 文件 | **9** | `find . -name '*.golden' -o -name '*golden*' \| wc -l` |
| 含 Usage 常量的文件 | **28** | `grep -rl 'Usage = \`'` |
| `newCmdContext*` 入口文件 | **29** | `grep -rc 'newCmdContext\|newCmdContextNoJSON'` |
| `writeJSONDocument` 调用点 | **15** | `grep -rn 'writeJSONDocument' cmd/ngm/*.go \| grep -v _test` |
| `emitWriteReport` 调用点 | **7** | 同上 |
| 含 `fmt.Fprint*` 的非测试文件 | **36** | `grep -rlE 'fmt\.Fprint'` |

> **口径纠正**：旧 ADR-047 写"44 非测试文件、约 2.2 万行"，本报告实测是 **43 文件、11,172 行**；
> 旧 `verifyUsage` 写"105 行"，实测 **79 行**（D2）。两者都是历史快照漂移，以本报告为准。

## 2. 成本估算（若拆）

按 ADR-047 的边界原则"命令实现 / 报告渲染 / 环境装配"三类：

| 抽出对象 | 需改调用点 | 需重验判据 | 风险 |
|---|---|---|---|
| 报告渲染层（`writeJSONDocument` + `emitWriteReport` + help 输出） | 15 + 7 + 9 golden | `TestV26`（JSON 形状）、`help_golden`、`observability_golden`、`TestCmdContextIsTheOnlySkeleton` | **中高**：字节契约与 golden 强耦合，抽出后必须逐字节对齐 |
| 环境装配层（`projectEnv` / `GraphOptions` / 路径注入） | 29 个 cmdContext 入口文件 | `TestCmdContextIsTheOnlySkeleton`、`config_wiring_acceptance` | 中：入口骨架是唯一通道，抽出要保持单一出口 |
| 命令实现（28 个命令文件） | 28 个 Usage 常量 + 各自 flag | `TestHintsNameFlagsTheCommandAccepts`、help golden | 高：收益最低（只是挪文件），风险最高 |

**总量级**：报告渲染层是最小可行切片（22 个调用点 + 9 golden），但收益也最小——它不改变任何
用户可见行为。真正的痛点是"跨文件耦合靠约定"而非目录大小，而这道约定已有两道机械守卫在守。

## 3. 建议的拆包边界（触发条件达成时）

1. **先抽报告渲染**，不先拆命令：它边界最清晰（输出函数集中），且能立即被 `TestV26` / golden 验证。
2. **保持 `cmdContext` 为唯一入口**：抽包后所有命令仍走同一骨架，不引入第二套报告通道。
3. **不建接口层**：ADR-047 明确"不为拆而新建接口层"——抽出的是包级函数，不是抽象接口。
4. **每个抽出单独提交 + 全量判据**：11-19 教训——新增包会打中 `TestCoreBoundary...` 类的计数判据，
   必须跑全量 `cmd/ngm`，不能只跑定向。

## 4. 触发条件现状（ADR-047 三条逐条核对）

| 条件 | 当前状态 |
|---|---|
| 同类跨文件耦合缺陷 ≥2 次 | **未达成**：11-20/11-21 的评估缺陷都是逻辑/文档，不是"约定失守" |
| 约定失守造成用户可见缺陷、现有守卫抓不到 | **未达成**：两道骨架守卫在册且绿 |
| 编译/测试时间可观测退化 | **未达成**：本机 `go test ./cmd/ngm` 约 30 分钟是测试逻辑串行，非单包规模导致 |

## 5. 相关文档

- [ADR-047 拆包触发条件](../adr/adr-047-package-main-size.md)
- [全维度评估 2026-11-20 §2.3](./ngm-full-assessment-2026-11-20.md)
- [competitive-followup spec §3.3](../specs/assessment-2026-11-21-competitive-followup/spec.md)
