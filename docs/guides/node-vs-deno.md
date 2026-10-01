# Node vs Deno：怎么选

> ngm 不替你选运行时。你的项目可以是 Node 或 Deno，ngm 都支持。

---

## 一句话结论

| 场景 | 推荐 |
|------|------|
| 已有 Node 项目、用 Vite/Next/Astro/Vitest | **Node** |
| 新项目、TS-first、重视安全 | **Deno** |
| 企业环境、CI 只装了 Node | **Node** |
| 想用原生 TS、少装 devDeps | **Deno** |

---

## 全维度对比

| 维度 | Node.js | Deno |
|------|---------|------|
| TypeScript | 需 tsx/tsc/strip-types | 原生 TS，零配置 |
| 包管理 | npm/pnpm/yarn | 内置 import map + JSR |
| 权限模型 | 默认全开 | default-deny（`--allow-*`） |
| 单二进制 | pkg/nexe/SEA | `deno compile` 原生 |
| 生态兼容 | npm 全量 | npm 可用但非 100%，native addon 有坑 |
| LTS | 成熟 | 较短但已生产可用 |
| 测试 | Vitest/Jest | 内置 `deno test` |
| 格式化 | Prettier | 内置 `deno fmt` |
| Lint | ESLint | 内置 `deno lint` |
| Doc | TypeDoc | 内置 `deno doc` |

---

## 用 Node 如果你……

- 项目是 Next.js / Remix / Astro / Express / Fastify
- 用了 native addon（better-sqlite3 / sharp / swc / prisma）
- 企业 CI 只允许 Node
- 团队不想学新运行时
- 依赖大量 npm 生态包

### Node 项目的 ngm 工作流

```bash
# 初始化
ngm init github.com/my-org/my-app --runtime=node

# 添加 Git 依赖
ngm add github:my-org/utils@v1.2.3 --ref-type tag

# 安装
ngm install

# 构建（adapter → esbuild）
ngm build --engine=esbuild

# 类型检查 —— 内置 `typescript`（tsc，v0.2 起）；也可在 ngm.json 里写 "engines": {"typeCheck": "typescript"}
ngm typecheck --engine=typescript
```

---

## 用 Deno 如果你……

- 新项目 TypeScript-first
- 重视 default-deny 权限模型
- 想 `deno fmt` `deno lint` `deno test` 少装 devDeps
- 依赖来自 Git，且希望 postinstall 默认关
- 想用 JSR 生态

### Deno 项目的 ngm 工作流

```bash
# 初始化
ngm init github.com/my-org/my-app --runtime=deno

# 添加 Git 依赖
ngm add github:my-org/utils@v1.2.3 --ref-type tag

# 安装
ngm install

# 构建（adapter → deno）—— deno 已适配，但**需自行声明**（bundle 是 Deno ≥ 2.4 的实验特性）
# ngm build --engine=deno

# 类型检查（adapter → deno）—— 同样需自行声明
# ngm typecheck --engine=deno
```

---

## ngm 对两者的承诺

ngm 不关心你运行时是谁。ngm 只关心：

> 这个 Git ref 解析成了哪个 commit，
> 这个 commit 的 archiveDigest 是不是你上次审过的那个。

### Node 项目

- runtime = node
- 引擎默认：`bundle` / `transform` 用 esbuild（内置）；`typeCheck` 内置 `typescript`（tsc，`optional`，装了就能用）
- mappings 供 Vite/esbuild 读取

### Deno 项目

- runtime = deno
- 引擎默认：与 Node 项目**相同**（内置 esbuild / typescript / postcss）；`deno` 已适配但**需自行声明**
- mappings 需要你把 `ngm.mappings.json` **手工**接进 `deno.json` 的 import map（自动生成属 v0.3）

---

## 混合项目

一个 monorepo 里 Node 和 Deno 混用：

```
monorepo/
├── packages/
│   ├── api/          # Node.js (Fastify)
│   ├── web/          # Deno (Fresh)
│   └── shared/       # 纯 TS，两边都能用
├── ngm.json
├── package.json      # Node 包的 registry 依赖
└── deno.json         # Deno 的 import map
```

ngm 统一管 Git 依赖，生成 mappings，Node 和 Deno 各自消费。

`runtime` 字段只决定默认引擎与 mappings 的推荐消费方式：混合 monorepo 取主运行时（多数包的运行时），各子包按自身工具链消费同一份 mappings。

---

## 相关文档

- [安装指南](./installation.md)
- [快速上手](./quickstart.md)
- [配置详解](./configuration.md)
- [架构：运行时模型](../architecture/runtime-model.md)
- [架构：安全模型](../architecture/security-model.md)
- [ADR-007：Node 还是 Deno？](../adr/adr-007-runtime-node-deno.md)
