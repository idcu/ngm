# 外部安全审计 / 渗透测试 · 立项备忘（2026-11-21）

> **状态：立项建议，未执行**。本文件**不虚构任何审计结论**——它只定义范围、验收标准与前置条件，
> 供预算/排期到位后照此委托第三方。当前 ngm 的供应链安全证据只是自证
> （`scripts/packagegate-demo.sh` 的断言 + 仓库内机械判据），不是第三方背书。

## 为什么立项

竞品全维度评估（2026-11-21）判定：npm 已到 SLSA L3（Sigstore + 透明日志 + trusted publishing），
而 ngm 的"Git 依赖可证明性闭环"目前**只有自证**。评估 §6.3 把外部审计列为"观察，不承诺"，
本轮"不怕风险"授权把它从观察推进到**立项准备**——但审计本身需预算与外部机构，不占工程排期。

## 范围（建议委托给第三方的三块）

| 块 | 要回答的问题 | 关键代码面 | 验收方式 |
|---|---|---|---|
| 1. 供应链门禁 | `net:<host>` / `run:git` / `run:script` / `run:<engine>` 是否可被绕过；默认 P1D 晾晒期与 `--allow-fresh` 逃生口是否可滥用 | `internal/core/vendor/module_cache.go`（首 clone 门禁）、`internal/core/git/spawn.go`（git 单构造点）、`internal/supplychain/policy.go` | 对抗性用例：构造 `.npmrc` 覆盖 git、tag 重打、恶意 commit；断言 fail-closed |
| 2. 沙箱隔离 | postinstall 脚本在 Deno 沙箱里是否真无网、无 run、写入恒不授予；WASM 引擎输出有界捕获是否可被 OOM 绕过 | `cmd/ngm/verify_sandbox.go`、`internal/adapter/wasm.go`、`internal/core/boundbuf` | 逃逸尝试：脚本尝试写宿主文件、触网、fork 子进程；断言全部被拒 |
| 3. verify 信任链 | `verify --content` 的 digest 重放能否被等长改写/符号链接/畸形清单欺骗；退出码优先级（2>4）是否一致 | `internal/core/verify/`、`internal/core/digest/`、`internal/core/lock/` | 变异测试：等长篡改、软链替换、`ls-tree -z` 畸形记录；断言 exit 2/3/4 语义 |

## 明确不在范围

- **上游仓库被攻破 / 恶意 commit 本身**：ngm 的设计边界（信任模型"不能证明什么"），审计不应把它当缺陷。
- **registry 生态**：ngm 不解析 registry 包（ADR-044），不在审计面内。
- **签名体系**：ngm 不签名（ADR-014/027），审计只查"是否正确地不持有密钥"。

## 验收标准（委托前写死）

1. 每一块给出**可复现的 PoC 或明确的"未发现"**，不接受"经检查未发现问题"的无证据结论。
2. 若发现缺陷，按 ngm 的严重度（S0 阻断 / S1 高 / S2 中 / S3 低）分级，并给出源码行号。
3. 报告里区分"设计边界（n-a）"与"缺陷"——把设计边界当漏洞会误导修复方向。
4. 审计方须独立于项目维护者；自证 demo 不替代第三方结论。

## 前置条件（当前未满足）

- 预算与排期：需人填（本备忘不假设金额）。
- 当前可交付的证据：`scripts/packagegate-demo.sh`（自证断言）、`docs/COMPETITIVE-ANALYSIS.md`
  §PackageGate、本报告 §2.3 的 SLSA 采用率实测——这些是审计输入，不是审计结论。
- 触发条件（达到才真正委托）：出现第一个要求第三方背书的采用场景（企业采购 / 合规问卷），
  或维护者决定把 PackageGate 从自证升级为外部背书。当前两者都未发生。

## 相关文档

- [竞品全维度差距评估 §6.3](./ngm-competitive-gap-full-2026-11-21.md)
- [信任模型](../architecture/trust-model.md)（"不能证明什么"）
- [竞品对比 §PackageGate](../COMPETITIVE-ANALYSIS.md)
- [competitive-followup tasks C2](../specs/assessment-2026-11-21-competitive-followup/tasks.md)
