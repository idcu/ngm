# ADR-007：Node 还是 Deno？

- **状态**：已定
- **日期**：2026-09-29
- **范围**：ngm core 实现语言 + 用户项目宿主运行时

---

## 背景

ngm 的本质不是"再做一个前端构建器"，而是：

> 拉 Git 仓库 → 解析 refType → 锁 commit + archiveDigest → vendor 落地 → verify 漂移 → 供应链门禁

这类工具的关键属性是：单文件分发、文件系统/网络/子进程权限可控、Git/archive/hash/cache 可靠、不依赖前端生态、CI 友好。

争论焦点：ngm 应该基于 Node 还是 Deno 实现？

---

## 决策

**三层模型**：

1. **ngm core**：Go 单二进制（实现语言）
2. **用户项目宿主运行时**：Node.js **或** Deno（ngm 不替用户选择）
3. **安全敏感子命令**：可借 Deno 权限模型 / 独立沙箱

> 实现语言用 Go；宿主运行时 Node/Deno 都支持；安全模型学 Deno。

---

## 全维度对比：Node vs Deno vs Go（对 ngm 的意义）

| 维度 | Node.js | Deno | Go（最终选择） | 对 ngm 的含义 |
|------|---------|------|---------------|-------------|
| 生态兼容 | 最大，npm 原生 | npm 可用但非 100%，native addon 有坑 | 无 JS 生态 | ngm core 不靠 JS 生态，Go 优势被削弱但可接受 |
| TypeScript | 需 tsx/tsc/strip-types | 原生 TS，零配置 | 需 codegen 或 echo 框架 | Deno 开发体验更好，但 ngm core 是 Go |
| 安全模型 | 默认全开，22+ 有实验权限 | default-deny，`--allow-*` | 无（core 无运行时） | **Deno 的权限模型适合安全敏感子命令** |
| 单二进制分发 | 需 pkg/nexe/SEA，体积大 | `deno compile` 原生 | `go build` 原生，极小 | **Go 分发最轻** |
| Git/archive/hash | 调 git/写 fs 都行 | 同样行，权限更干净 | 标准库 + 第三方库成熟 | Go 最可靠 |
| 调外部引擎 | spawn 简单 | 需配 `--allow-run/--allow-env` | os/exec 标准库 | Node 省心，Deno 更可审计 |
| 用户心智 | "肯定装了 Node" | "还要装 Deno" | "还要装 Go（仅编译时）" | Node 零门槛，Deno 多一步 |
| 长期运维 | LTS 成熟 | LTS 较短但已生产可用 | 强兼容承诺 | 企业项目 Node 更稳 |
| 自举/文档/CLI | 社区方案多 | cliffy/std 很舒服 | flag/echo/text/template | Go 写 CLI 干净利落 |
| 前端生态集成 | Next/Vite/Astro/Vitest 一等公民 | 支持但偶有兼容问题 | 无 | **用户项目运行时选 Node 更安全** |

---

## 为什么 Go 适合 ngm core

ngm core 的工作是文件系统密集 + IO 密集 + 需要单二进制：

- 内容寻址缓存（hash 大量文件）
- hardlink/symlink 管理
- Git 仓库 mirror
- 内容清单生成与内容树解包（见 ADR-008）
- lock file 读写
- verify / audit 的批量计算

这些 Go 的标准库和并发模型都处理得很好，且**不需要 JS 运行时**。

---

## 为什么宿主运行时支持两者

ngm 不替用户选运行时。用户项目的构建链可能是：

**Node 项目**：
```bash
ngm add github:org/ui@v2.0.0
ngm build --engine=esbuild   # adapter → esbuild
```

**Deno 项目**：
```bash
ngm add github:org/ui@v2.0.0
ngm build --engine=deno      # adapter → deno
```

ngm 只关心：

> 这个 Git ref 解析成了哪个 commit，
> 这个 commit 的 archiveDigest 是不是你上次审过的那个。

---

## 为什么安全模型学 Deno

Deno 的 default-deny 权限模型非常适合"跑不可信依赖"：

```bash
# Deno 的权限模型
deno run --allow-net=github.com --allow-read=$NGM_VENDOR --allow-run=git script.ts
```

ngm 可以把 `ngm verify --sandbox` / `ngm audit` / postinstall scan 设计成：

> core Go 做决策，Deno 做沙箱执行环境

这给 ngm 一个竞品没有的卖点：**供应链检查本身也在沙箱里跑**。

---

## 什么时候不推荐 ngm

- 你只用 npm registry 包 → 用 pnpm
- 你要生产级前端构建 → 用 Vite（ngm 只负责把依赖放进 vendor）
- 你要插件生态 → Vite/Webpack 生态远大于自研
- 你想要"一个命令搞定一切" → ngm 故意不做

---

## 后果

- ngm 不再和 Vite 比 HMR
- ngm 不再和 pnpm 比 node_modules 磁盘
- ngm 只和"Git 依赖可证明性"这个窄问题较劲
- 文档里所有"我们更快/我们一体化/我们自研替代 X"全部删除
- 新增文档：`runtime-model.md`、`security-model.md`、`node-vs-deno.md`

---

## 相关文档

- [运行时模型](../architecture/runtime-model.md)
- [安全模型](../architecture/security-model.md)
- [Node vs Deno 指南](../guides/node-vs-deno.md)
- [ADR-001：为什么用 Go](./adr-001-go.md)
