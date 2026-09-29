# CLI 参考

> 成熟度列标注该命令自哪个版本可用；`v0.1` 的命令已实现，并由[端到端验收](../development/v0.1-plan.md)守护。
> 未实现的命令不会静默成功——它们明确返回 `exit 3` 与可读提示。

---

## 命令一览

| 命令 | 作用 | 成熟度 | 主要来源文档 |
|------|------|--------|-------------|
| `ngm init <name> [--runtime=node\|deno] [--dir=<path>] [--force]` | 初始化项目（同时生成 `src/index.ts`，不覆盖已存在的） | v0.1 | [快速上手](./quickstart.md) |
| `ngm add <git-url>[@<ref>] --ref-type <type> [--path=<子路径>] [--dry-run]` | 添加依赖（refType 必填） | v0.1 | [依赖管理](./dependency-management.md) |
| `ngm install [--digest]` | 解析并安装依赖（有 lock 则尊重 lock） | v0.1 | [依赖管理](./dependency-management.md) |
| `ngm update [<dep>] [--all] [--offline] [--digest] [--store]` | 更新 ref 与锁定 | v0.1 | [依赖管理](./dependency-management.md) |
| `ngm remove <dep>` | 移除依赖（只改声明） | v0.1 | [依赖管理](./dependency-management.md) |
| `ngm verify [<dep>...] [--offline] [--deep] [--json] [--strict] [--allow-drift]` | ref 漂移 + digest 重放检查 | v0.1 | [信任模型](../architecture/trust-model.md) |
| `ngm build [<entry>] [--engine=<name>] [--outfile=<path>] [--production] [--dry-run]` | 构建（adapter） | v0.1 | [构建](./build.md) |
| `ngm typecheck [<entry>] [--engine=<name>] [--tsconfig=<path>] [--dry-run]` | 类型检查（adapter；v0.1 无适配引擎） | v0.1 | [构建](./build.md) |
| `ngm css <input.css> [--engine=<name>] [--outfile=<path>] [--minify] [--dry-run]` | CSS 编译（adapter；v0.1 无适配引擎） | v0.1 | [构建](./build.md) |
| `ngm mappings validate` | 校验 mappings 与 lock / vendor 一致性 | v0.1 | [P4 — 生态与协议](../modules/p4-ecosystem.md) |
| `ngm cache clean` | 清空缓存层（不影响可证明性） | v0.1 | [vendor 4 层](../architecture/vendor-layers.md) |
| `ngm config validate\|show` | 配置校验与查看 | v0.1 | [配置详解](./configuration.md) |
| `ngm engines list\|info\|validate [--json]` | 引擎管理 | v0.1 | [配置详解](./configuration.md) |
| `ngm install [--frozen-lockfile] [--offline]` | CI 模式安装（禁止解析新 ref / 禁止联网） | v0.2 | [锁定机制](../architecture/locking.md) |
| `ngm audit` | OSV 漏洞扫描 | v0.2 | [供应链防护](../architecture/supply-chain.md) |
| `ngm why <dep>` / `ngm tree` / `ngm outdated` | 依赖洞察 | v0.2 | [可观测性](../architecture/observability.md) |
| `ngm integrations add <tool>` | 生成构建工具集成配置 | v0.3 | [P5 — 外部工具集成](../modules/p5-integrations.md) |
**content store 的回收尚未排期**：v0.1 的 store 只增不减，`ngm cache clean` 只清缓存层
（不影响可证明性）。store GC 不在 v0.2 范围内，等需要时再排——这里**不预告具体命令名**，
以免文档承诺一个不存在的 `ngm store gc`。

**v0.1 的构建引擎范围**：只适配了 esbuild（`bundle` + `transform`）。`tsc` / `deno` / `postcss`
尚未适配，声明它们会得到明确的 `exit 5` 而不是静默降级——自行声明 subprocess 引擎的方法见
[配置详解](./configuration.md) 与 [P4 协议](../modules/p4-ecosystem.md)。

---

## 全局约定

- **退出码**：0 成功 / 1 策略失败（含引擎运行失败）/ 2 完整性失败 / 3 配置错误 / 4 Git 网络失败 / 5 引擎不可用；完整定义见[可观测性](../architecture/observability.md)
- **配置文件**：`ngm.json` / `ngm.lock` / `ngm.mappings.json` / `ngm.engines.json` / `~/.ngm/config.json`，见[配置详解](./configuration.md)
- **`--offline`**（v0.1 适用于 `update` / `verify`）：禁止网络访问，只用本地 mirror；资源未命中即失败（exit 4）。
  `install --offline` 属 v0.2。
- **`--json`**：机器可读输出走 stdout，人类文本与诊断走 stderr；CI **不应**解析人类文本。

---

## 常用组合

CI 门禁（v0.1 可用）：

```bash
ngm install            # 有 ngm.lock → 尊重 lock，不重新解析 ref
ngm verify --json      # 读 driftKind 与退出码：0 通过 / 1 非预期漂移 / 2 完整性失败
ngm build --outfile=dist/app.js
```

完全离线可复现（`--frozen-lockfile` / `install --offline` 属 v0.2）：

```bash
ngm install            # v0.2: ngm install --frozen-lockfile --offline
ngm verify --json
```

开发循环：

```bash
ngm add github:my-org/utils@main --ref-type branch
ngm install
ngm verify
ngm build --dry-run    # 先看 ngm 会怎么调引擎
```

---

## 相关文档

- [快速上手](./quickstart.md)
- [依赖管理](./dependency-management.md)
- [构建](./build.md)
- [配置详解](./configuration.md)