# 能力矩阵

> ngm 的能力现状。成熟度取值：`done`（已实现，标注版本）/ `planned`（设计承诺，标注目标版本）/ `n/a`（不由 ngm 提供）。
>
> 本文是**能力归属的唯一事实源（SSOT）**：其他文档引用本表，不再复制"不做的能力 → 交给谁"的清单。
> v0.1 的实测结论见 [v0.1 复盘](../development/v0.1-retrospective.md)。

---

## 核心能力

| 能力 | ngm 做法 | 成熟度 | 竞品已有 | 备注 |
|------|---------|--------|---------|------|
| Git 拉取 | 4 种协议归一化 | done (v0.1) | npm/pnpm/Yarn 都支持 | ngm 的 refType 是增量 |
| 依赖锁定 | refType + commit + digest | done (v0.1) | npm 记录 commit | ngm 的 archiveDigest 是增量 |
| vendor 落地 | 4 层模型 | done (v0.1) | pnpm content store | ngm 强调可审计/可提交 |
| ref 漂移检测 | verify | done (v0.1) | Yarn immutable | ngm 区分预期/非预期 |
| 供应链门禁 | JSON-first 策略 | planned (v0.2) | pnpm/Deno/npm | ngm 绑定到 Git commit |
| OSV 集成 | audit | planned (v0.2) | npm/yarn audit | 通用能力 |
| 可观测性 | why/tree/outdated | planned (v0.2) | pnpm | 通用能力 |

---

## 构建能力（通过 adapter）

| 能力 | ngm adapter | 外部引擎 | 成熟度 | 备注 |
|------|------------|---------|--------|------|
| Transform | subprocess | esbuild | done (v0.1) | 有实现，尚无命令暴露（见复盘 §6） |
| Bundle | subprocess | esbuild | done (v0.1) | `ngm build` |
| Tree-Shaking | 不内置 | esbuild 原生 | n/a | 依赖引擎 |
| Type Check | subprocess | tsc / deno | planned (v0.2) | v0.1 声明后明确 exit 5，不静默降级 |
| .d.ts 生成 | subprocess | tsc | planned (v0.2) | 同上 |
| CSS/SCSS | subprocess | postcss | planned (v0.2) | 同上 |

---

## 明确不做的能力

| 能力 | 不做的原因 | 交给谁 |
|------|-----------|-------|
| 自研生产级 bundler | esbuild/Rollup 已覆盖 | esbuild / Rollup / Vite |
| HMR | Vite 已是标杆 | Vite |
| 代码分割优化 | Rollup/Vite 已成熟 | Rollup / Vite |
| Source Map 完整链路 | esbuild/Vite 已成熟 | esbuild / Vite |
| 自研 test runner | Vitest/Deno test 已成熟 | Vitest / Deno test |
| 自研 docs generator | TypeDoc/Deno doc 已成熟 | TypeDoc / Deno doc |
| 自研 LSP | tsserver/Deno LSP 已成熟 | tsserver / Deno LSP |
| 自研 Dev Server | Vite/Deno 已成熟 | Vite / Deno |
| 自研 CSS 编译器 | postcss/lightningcss 已成熟 | postcss / lightningcss |
| registry 包管理 | pnpm/npm/yarn 已成熟 | pnpm / npm / yarn |
| 自研 type checker | tsc 经过十年打磨 | tsc / deno check / tsgo |
| 自研 transformer | esbuild/SWC/Biome 已成熟 | esbuild / SWC / Biome |

---

## 诚实版能力评估

### ngm 真正独有的

1. **refType 必填** + archiveDigest + verify 闭环
2. **vendor 4 层**为可审计目标服务（不只是省磁盘）
3. **供应链策略绑定到 Git commit** 而不是 registry version
4. **mappings 协议**桥接 ngm 与外部构建工具

### ngm 不独有的

1. Git 依赖支持（npm/pnpm/Yarn 都有）
2. content-addressable store（pnpm 已有）
3. OSV 集成（npm/yarn 都有）
4. minimumReleaseAge（pnpm/Deno/npm 都有）
5. 白名单（Yarn approvedGitRepositories）
6. ref 漂移检测（Yarn immutable）

**结论**：ngm 的差异化不在单项能力，而在**流程闭环**和**共享状态模型**。若这些策略只是分别调用外部命令，ngm 就只是配置与编排层。

---

## 相关文档

- [架构总览](../architecture/overview.md)
- [健康度指标](./metrics.md)
- [路线图](./roadmap.md)
