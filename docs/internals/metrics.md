# 健康度指标

> 下表的目标值均为**设计值**；v0.1 的**实测值**见此表旁的标注与方法说明，
> 完整对照与根因分析见 [v0.1 复盘](../development/v0.1-retrospective.md)。

---

## 核心指标

| 指标 | 目标 | 测量方式 | 备注 |
|------|------|---------|------|
| 依赖解析正确性 | 100% | 测试用例 | refType 解析、冲突检测 |
| lock file 可复现 | 100% | 确定性字段集字节级一致（排除 `resolvedAt`） | 见[锁定机制](../architecture/locking.md) |
| verify 准确率 | 0 误报 / 0 漏报 | 模拟 drift 场景 | 区分预期/非预期 |
| archiveDigest 可重放 | 100% | 相同 commit → 相同 digest | 归一化规则必须稳定 |
| vendor 4 层正确性 | 100% | hardlink 校验 | 跨平台兼容 |

---

## 性能目标与 v0.1 实测

测量方法：`NGM_BENCH=1 go test -count=1 -run TestBaseline -v ./cmd/ngm`（本地 fixture、无网络）。
环境：Windows 10 / 8 核 / go1.27.1 / git 2.56.0。

| 操作 | 目标 | v0.1 实测 | v0.2 实测 | 结论 |
|------|------|-----------|-----------|------|
| cold install（10 个依赖） | < 5s | 1.91s | 1.86s | ✅ |
| warm install（无变更） | < 500ms | 11ms（10 依赖）/ 107ms（100 依赖） | 13ms（10 依赖）/ 114ms（100 依赖） | ✅ |
| verify（100 个依赖，在线） | < 3s | 18.77s | **3.4s → 2.51s**（[ADR-010](../adr/adr-010-online-verify-fetch-policy.md)） | ✅ 达标 |
| offline verify（100 个依赖） | < 5s | 11.12s | **1.97s** | ✅ v0.2 达标 |
| verify --deep（100 个依赖，离线） | — | 12.65s | 2.28s | 参考值（无目标） |
| archiveDigest 计算（100MB 仓库） | < 1s | 452ms | 446ms | ✅ |

> **性能目标全部达标**（v0.2 完成时在线 verify 尚差 1.1×，由 [ADR-010](../adr/adr-010-online-verify-fetch-policy.md)
> 的"ref 问远端、对象按需取"在 v0.3 开工时补上：3.37s → **2.51s**）。
>
> 这条达标来之不易，三次改动各自解决一层问题，且每层都有实测：
>
> 1. **依赖级并发**（默认 8；实测 4→8 是拐点，8→16 已无收益）：5.09s→3.37s
> 2. **成功路径少一次 spawn**（commit 存在性检查改为惰性判定）
> 3. **不再无条件 fetch**（ADR-010）：只读 ref 广播，对象按需才取 → 2.51s ✓
>
> 顺带更正一条**前提不成立**的旧建议：复盘 §3.4 曾建议"按仓库合并 git 调用"，
> 但本基准的 100 个依赖是 **100 个独立仓库**，没有可合并的调用，收益为零。
> 那条建议成立于"多依赖共享同一仓库"的前提，本基准不满足（已在复盘中撤回并写明原因）。
>
> `verify` 的成本始终是 **git 子进程启动**（本机 `git ls-remote` 65ms/次），不是哈希或 I/O——
> 100 MiB 的单次 digest 重放只需 452ms。优化方向与预计收益见
> [v0.1 复盘](../development/v0.1-retrospective.md) §3.4。
>
> ngm 不宣称"比 pnpm/Bun 快"；上表用于**发现自己变慢了**，不是竞争性指标。

---

## 兼容性目标

| 维度 | 目标 |
|------|------|
| Go 版本 | >= 1.22 |
| Git 版本 | >= 2.30 |
| Node.js | >= 22.0.0 |
| Deno | >= 2.0 |
| 平台 | macOS / Linux / Windows |
| Git host | GitHub / Gitee / GitLab / 自建 |

---

## 诚实边界

1. **实测口径有限**：v0.1 的实测来自单一环境（Windows / 本机磁盘）与合成 fixture；
   真实仓库的 ref 数量、对象体积、网络延迟都会改变结果
2. **性能不是卖点**：Bun 无变更重装约 12ms，ngm 不应以速度竞争
3. **正确性优先**：refType + digest 的正确性比性能重要
4. **跨平台兼容是难点**：hardlink 需要同卷文件系统（Windows NTFS 可用），不支持时降级复制
5. **verify 的成本在子进程而非内容**：v0.1 每个依赖约 4 次 git spawn，
   100 依赖因此在 Windows 上约 11s（`--offline`）；见复盘 §3.3

---

## 相关文档

- [能力矩阵](./capability-matrix.md)
- [路线图](./roadmap.md)
