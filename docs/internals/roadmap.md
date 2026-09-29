# 路线图

> ngm 的产品路线。**v0.1 已实现**（四条退出标准全部达成，实测结论见 [v0.1 复盘](../development/v0.1-retrospective.md)）；
> v0.2 及以后仍为规划。

---

## 总原则

**先窄后宽**：先把"Git 依赖可证明性"做深做透，再扩展生态集成。

**第三方优先**：不自研追赶 esbuild/tsc/Vite/Deno，全部通过 adapter 调用。

**核心精简**：ngm core 只做依赖证明层，不做构建/测试/文档/Dev Server/LSP。

---

## v0.1 — Git 依赖可证明性闭环

**目标**：`ngm install && ngm verify` 能证明 Git 依赖的可追溯性。

**必须完成**：

| 模块 | 任务 |
|------|------|
| Git | 4 种协议归一化、凭证管理 |
| 解析 | refType 解析（commit/tag/branch）、依赖图、冲突检测 |
| 锁定 | refType + commit + archiveDigest + resolvedAt |
| vendor | mirror / content store / hardlink tree / cache |
| verify | ref 漂移检测 + digest 检查 + 退出码 |
| adapter | esbuild subprocess adapter |
| mappings | 生成 ngm.mappings.json |
| 维护 | `ngm cache clean`（缓存层可随时清空） |

**退出标准**（四条**均已达成**，证据见 [v0.1 复盘](../development/v0.1-retrospective.md) §1）：

- 能在真实项目上跑通 `ngm install` ✅
- lock file 跨平台字节级一致 ✅
- verify 能检测出 tag 重打和 branch 前进 ✅
- digest 重放不匹配能阻断构建（exit 2）✅

**尚未达成的非阻塞项**：性能目标 5 条中 2 条未达标（`verify` 在 100 依赖规模上超目标
6.3× / 2.2×），根因是每个依赖 4 次 git 子进程，列入 v0.2 优化项。

---

## v0.2 — 供应链防护完整

**目标**：把 refType + commit + digest + vendor + verify + OSV 收束为统一流程。

| 模块 | 任务 |
|------|------|
| OSV | OSV.dev 查询 + 缓存 + 报告 |
| 策略 | minimumReleaseAge、白名单、postInstallPolicy |
| audit | `ngm audit` 命令 + 报告格式化 |
| 可观测性 | why / tree / outdated |
| adapter | tsc / deno / postcss adapter |
| CI | `--frozen-lockfile` / `--offline` 模式 |

---

## v0.3 — 引擎生态与协议

**目标**：让 ngm 能接入主流构建工具。

| 模块 | 任务 |
|------|------|
| adapter | wasm adapter、remote adapter |
| 集成 | Vite / esbuild / Deno / Webpack 集成脚手架 |
| sandbox | Deno 沙箱模式 |
| 凭证 | `~/.ngm/config.json` 权限管理 |
| mappings | v2 协议（支持 monorepo） |

---

## 后续探索（不承诺）

| 方向 | 前提 |
|------|------|
| 多语言 Git 依赖（Go/Rust/Python） | JS/TS 场景证明可行 |
| 自研 transformer（兜底） | subprocess 有不可接受短板 |
| 远程构建缓存 | 企业级需求 |
| SBOM 导出（SPDX/CycloneDX） | 合规需求 |

---

## 里程碑依赖关系

```
v0.1（可证明性闭环）
  │
  ├── 必须优先：refType + commit + digest + vendor + verify
  │
  ▼
v0.2（供应链完整）
  │
  ├── 依赖 v0.1 的 lock schema 和 verify 机制
  │
  ▼
v0.3（引擎生态）
     │
     └── 依赖 v0.1 的 adapter 协议和 mappings 协议
```

**关键路径**：v0.1 的 lock schema 和 adapter protocol 一旦稳定，后续里程碑才能展开。

---

## 诚实边界

1. **里程碑可能延期**：这是早期项目的常态
2. **范围可能收缩**：如果 v0.1 证明某些设计不成立，会调整
3. **不做大承诺**：ngm 是窄场景工具，不追求通用替代品

---

## 相关文档

- [能力矩阵](./capability-matrix.md)
- [健康度指标](./metrics.md)
- [模块分解（P0~P8）](../modules/)
- [开发计划（v0.1 ~ v0.3）](../development/)
