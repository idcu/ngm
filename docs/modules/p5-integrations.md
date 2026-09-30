# P5 — 外部工具集成

> 构建工具集成脚手架：Vite / esbuild / Deno / Webpack 消费 mappings

---

## 状态

**v0.3 已实现**。P5 是 [P4 协议](./p4-ecosystem.md)的消费层：ngm core 只做依赖证明，集成通过 mappings 实现（能力归属见[能力矩阵](../internals/capability-matrix.md)）。

---

## 职责

让主流构建工具能直接消费 `ngm.mappings.json`，并提供集成脚手架命令。

---

## 模块结构

```
internal/integrations/
├── scaffold.go      # 工具抽象、产物与冲突策略、冲突错误与下一步提示
├── resolve.go       # 标识符 → 目标（长键优先）、tsconfig paths、Deno import map
├── vite.go          # Vite 集成
├── esbuild.go       # esbuild 集成
├── webpack.go       # Webpack 集成
├── deno.go          # Deno 集成
├── tsconfig.go      # tsconfig paths（每个工具都带）
└── diff.go          # 冲突时的行级差异

cmd/ngm/
└── integrations.go  # ngm integrations
```

（完整模块树见 [P0 — ngm core](./p0-core.md)。）

---

## 集成清单

| 工具 | 消费方式 | 产物 | 版本 |
|------|---------|------|------|
| Vite | `resolve.alias` 读取 mappings | `vite.config.ts` | v0.3 |
| esbuild | `alias` 读取 mappings | `ngm.esbuild.cjs`（可执行） | v0.3 |
| Deno | import map | `ngm.importmap.json`（静态） | v0.3 |
| Webpack | `resolve.alias` 读取 mappings | `ngm.webpack.cjs` | v0.3 |
| TypeScript | `compilerOptions.paths` | `ngm.tsconfig.json` | v0.3 |

用户侧接入示例见[构建指南](../guides/build.md)；mappings schema 见 [P4](./p4-ecosystem.md)。

### 两类产物，两种所有权

| 类别 | 文件 | 行为 |
|------|------|------|
| **ngm 的文件** | `ngm.esbuild.cjs` / `ngm.webpack.cjs` / `ngm.importmap.json` / `ngm.tsconfig.json` | 内容变化时**重生成** |
| **用户的文件** | `vite.config.ts` / `tsconfig.json` / `deno.json` | **永不覆盖**。内容不同时报冲突（exit 3）并给出差异；只允许新建 |

为什么把 tsconfig 拆成 `ngm.tsconfig.json` + 一个 `extends`：`tsconfig.json` 属于用户，
我们无权改写它（也可能带注释）。`ngm.tsconfig.json` 里**只有** `compilerOptions.paths`——
生成的片段越小，与用户配置冲突的面就越小。

---

## 集成命令

```bash
ngm integrations add vite
ngm integrations add esbuild
ngm integrations add deno
ngm integrations add webpack
```

`--dry-run` 只打印计划，`--json` 输出机器可读报告。

行为约定：

- **幂等**：重复运行结果一致；内容已一致时报 `up to date`，不是错误
- **全有或全无**：任一处冲突就一个文件都不写。半套脚手架（写了配置、没写 tsconfig）
  会让项目停在既不能构建、又看不出缺什么的中间态
- **冲突可操作**：报出 `-`（当前文件）/ `+`（ngm 期望）的行级差异，并给出可粘贴的指引
- 已存在的 `tsconfig.json` / `deno.json` **不是冲突**：它们没有错，只是需要用户加一行
  （`extends` 或 `importMap`），命令会把这行写进提示里并以 exit 0 结束

---

## 诚实说明

1. **mappings 是一层间接性**：增加复杂度，但必要
2. **集成是薄层**：ngm 不追求插件生态数量
3. **构建工具的演进风险**：各工具 alias / import map 机制变化时需要跟进
4. **部分工具已用真工具验证，部分没有**：
   - **实测通过**：生成的 esbuild 脚本真打包（含 monorepo 子路径）、生成的 tsconfig 真被 `tsc` 用来解析 `github:` 前缀——两者都在 CI 的 `engine-integration` job 里跑
   - **未逐条实测**：Vite / Webpack / Deno 的产物只经内容断言（CI 未安装这三个工具）。
     它们的语义按各工具文档写，并把"我不确定的部分"做成了不依赖具体规则的形式
     （Webpack 用 `$` 强制精确匹配、Deno 用精确键 + 前缀键）
5. **Deno 的 import map 是静态的**：依赖增删后必须重新运行 `ngm integrations add deno`。
   其余三个工具在**构建时读取** mappings，不需要重新生成

---

## 相关文档

- [P4 — 生态与协议](./p4-ecosystem.md)
- [架构：引擎 adapter](../architecture/engine-adapter.md)
- [构建指南](../guides/build.md)
- [CLI 参考](../guides/cli.md)
