# 运行时模型

## 三层架构

```
┌─────────────────────────────────┐
│  Host project runtime           │  Node.js / Deno
│  （用户代码实际运行的环境）       │
│  Next / Astro / Hono / 原生 TS  │
└───────────────┬─────────────────┘
                │
    ngm build / ngm typecheck
    （adapter 调用；v0.1 只适配了 esbuild）
                │
                ▼
┌─────────────────────────────────┐
│  Engine adapters                │
│  ┌──────────┬──────────┐      │
│  │ esbuild  │  tsc     │      │
│  ├──────────┼──────────┤      │
│  │  deno    │ postcss  │      │
│  ├──────────┼──────────┤      │
│  │  self    │  remote  │      │
│  └──────────┴──────────┘      │
│  embed / subprocess /          │
│  wasm / remote                 │
└───────────────┬─────────────────┘
                │
                ▼
┌─────────────────────────────────┐
│  ngm core (Go)                  │
│  ┌───────────────────────────┐ │
│  │ 依赖解析 / lock / vendor  │ │
│  │ cache / hardlink / verify │ │
│  │ audit / supply-chain gate │ │
│  └───────────────────────────┘ │
│  单二进制，无 JS 运行时         │
└─────────────────────────────────┘
```

---

## 各层职责

### ngm core（Go）

**只做依赖证明层**：

- Git URL 归一化（4 种协议）
- refType 解析（commit/tag/branch）
- commit 解析与锁定
- archiveDigest 计算
- vendor 4 层管理（mirror / content store / hardlink tree / cache）
- lock file 读写
- verify 漂移检测
- audit / OSV / 白名单门禁

### Engine adapters

**调用外部工具完成构建/类型/CSS**。adapter 类型（embed / subprocess / wasm / remote）与接口定义唯一维护在[引擎 adapter 模型](./engine-adapter.md)。

### Host runtime（Node / Deno）

**用户项目实际运行的环境**，ngm 不干预：

- Node.js 项目：Next / Astro / Vitest / Playwright
- Deno 项目：原生 TS、Deno std、Deno test
- ngm 生成 `ngm.mappings.json`，构建工具（Vite/esbuild/Deno）读取后把裸导入映射到 vendor 路径

---

## 为什么这样分层

### Go 适合 core

- 单文件分发：ngm 一个二进制，CI 不需要装 Node/Deno
- 文件系统语义强：hardlink / symlink / content store 可控
- 并发模型适合 IO 密集：并行拉 Git、并行算 hash、并行校验
- 不背 JS 生态包袱：ngm 不会被"依赖漂移"反噬

### 为什么不做 Node 实现

- Node 项目已经有 pnpm/npm/yarn，ngm 不做 registry 包管理
- ngm core 的工作（文件系统/缓存/hash）Go 更擅长
- Node 单二进制分发需 pkg/nexe/SEA，体积大且体验差

### 为什么不做 Deno 实现

- Deno compile 单二进制比 Node 好，但仍需携带 JS runtime 开销
- Go 的 os/exec + 标准库在 CLI 场景更成熟
- Deno 的权限模型是**安全层**的价值，不是**实现语言**的理由

### 为什么宿主可以是 Deno

Deno 的 default-deny 权限模型非常适合"跑不可信依赖"：

```bash
deno run \
  --allow-net=github.com \
  --allow-read=$NGM_VENDOR \
  --allow-run=git \
  ./verify-script.ts
```

ngm 可以把 `ngm verify --sandbox` / `ngm audit` / postinstall scan 设计成：

> core Go 做决策，Deno 做沙箱执行环境

---

## 不做的事

自研引擎与相关能力的归属清单，唯一维护在[能力矩阵](../internals/capability-matrix.md)。

---

## 相关文档

- [ADR-001：为什么用 Go](../adr/adr-001-go.md)
- [ADR-007：Node 还是 Deno？](../adr/adr-007-runtime-node-deno.md)
- [引擎 adapter 模型](./engine-adapter.md)
- [安全模型](./security-model.md)
