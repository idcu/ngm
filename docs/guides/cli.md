# CLI 参考

> 成熟度列标注该命令自哪个版本可用；v0.1 ~ v0.4 的命令**均已实现**，v0.1 的由
> [端到端验收](../development/v0.1-plan.md)守护，其后的由各版验收测试守护。
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
| `ngm verify [<dep>...] [--offline] [--deep] [--json] [--strict] [--allow-drift] [--sandbox] [--signatures] [--require-signed]` | ref 漂移 + digest 重放检查；`--sandbox` 追加在 Deno 沙箱里执行依赖自带的 `verify.js`（缺 Deno 且确有脚本 → exit 5）；`--signatures` 追加报告 Git 签名状态（**未签名不是失败**），`--require-signed` 把它变成门槛（未签名/无法用你的密钥验证 → exit 2） | v0.1 / **v0.3 增 `--sandbox`** / **v0.4 增 `--signatures`、`--require-signed`** | [信任模型](../architecture/trust-model.md) · [ADR-012](../adr/adr-012-sandbox.md) · [ADR-014](../adr/adr-014-self-report-signatures.md) |
| `ngm build [<entry>] [--engine=<name>] [--outfile=<path>] [--production] [--dry-run]` | 构建（adapter） | v0.1 | [构建](./build.md) |
| `ngm typecheck [<entry>] [--engine=<name>] [--tsconfig=<path>] [--dry-run]` | 类型检查（adapter） | v0.1 命令 / **v0.2 有引擎**（`typescript` = tsc，未装则 exit 5） | [构建](./build.md) |
| `ngm typedecl [<entry>] --outdir=<dir> [--engine=<name>] [--dry-run]` | 生成 `.d.ts` 声明（adapter）；**`--outdir` 必填**，并报告**实际出现**的文件 | **v0.4 已实现**（此前该能力只有 adapter 与单测，没有命令驱动它） | [构建](./build.md) |
| `ngm transform [<file>] [--engine=<name>] [--outfile=<path>] [--loader=<name>] [--target=<es20xx>] [--format=<fmt>] [--minify] [--sourcemap] [--dry-run]` | **单文件**转换（adapter）；输入默认走 **stdin**，产物默认走 stdout。**不解析导入**——那是 `ngm build`。loader 按 `--loader` → 文件扩展名（推断会说出来）→ `engines.transform.options.loader` 的顺序取；都不适用时**不猜**，"必须有 loader" 由引擎自己判（esbuild 会） | **v0.5 已实现** | [构建](./build.md) |
| `ngm css <input.css> [--engine=<name>] [--outfile=<path>] [--minify] [--dry-run]` | CSS 编译（adapter） | v0.1（`esbuild`）；v0.2 增 `postcss`（无内建压缩，`--minify` 会被明确告知忽略） | [构建](./build.md) |
| `ngm mappings validate` | 校验 mappings 与 lock / vendor 一致性 | v0.1 | [P4 — 生态与协议](../modules/p4-ecosystem.md) |
| `ngm cache clean` | 清空缓存层（不影响可证明性） | v0.1 | [vendor 4 层](../architecture/vendor-layers.md) |
| `ngm config validate\|show` | 配置校验与查看 | v0.1 | [配置详解](./configuration.md) |
| `ngm engines list\|info\|validate [--json]` | 引擎管理 | v0.1 | [配置详解](./configuration.md) |
| `ngm audit [<dep>...] [--json] [--offline] [--no-cache] [--hook=<script.js>]` | OSV 漏洞扫描（按 **commit** 查询 + 24h 缓存）；`--hook` 在沙箱里跑团队自己的策略（报告从 stdin 进入，否决 → exit 1） | **v0.2 已实现**；`--hook` **v0.3** | [供应链防护](../architecture/supply-chain.md) · [ADR-012](../adr/adr-012-sandbox.md) |
| `ngm why <dep> [--json]` | 该依赖的来源路径（有多个父节点时列出全部） | **v0.2 已实现** | [可观测性](../architecture/observability.md) |
| `ngm tree [--osv] [--offline] [--json]` | 依赖树 + 漂移（`⚠`）；漏洞（`✗`）需 `--osv` | **v0.2 已实现** | [可观测性](../architecture/observability.md) |
| `ngm outdated [--offline] [--json]` | 有哪些新版本；查不到报 `unknown` 而非"最新" | **v0.2 已实现** | [可观测性](../architecture/observability.md) |
| `ngm install [--frozen-lockfile] [--offline]` | CI 模式：frozen 禁止解析新 ref / 改写 lock（不一致 exit 3）；offline 禁止联网（资源缺失 exit 4） | **v0.2 已实现** | [锁定机制](../architecture/locking.md) |
| `ngm integrations add <tool> [--dry-run] [--json]` | 生成 `vite` / `esbuild` / `deno` / `webpack` 集成配置（**不覆盖已有文件**，冲突 exit 3） | **v0.3 已实现** | [P5 — 外部工具集成](../modules/p5-integrations.md) |
**content store 的回收至今未排期**：store 只增不减，`ngm cache clean` 只清缓存层
（不影响可证明性）。等需要时再排——这里**不预告具体命令名**，
以免文档承诺一个不存在的 `ngm store gc`。

**内置引擎清单（v0.4 时点）**：`esbuild`（`bundle` + `transform`）、`typescript`（`typeCheck` + `typeDecl`，
自带 `tsc --emitDeclarationOnly`，`optional`）、`postcss`（`css`，`optional`）、`self` stub；
wasm adapter 自 v0.3 起可用（模块路径写在清单里，缺失是**可用性**问题 → `exit 5`）。
`remote` **按 [ADR-013](../adr/adr-013-remote-adapter.md) 决定不发布**，声明它会得到"按决定排除"而不是"待实现"。
自行声明 subprocess 引擎的方法见 [配置详解](./configuration.md) 与 [P4 协议](../modules/p4-ecosystem.md)。

**`transform` 自 v0.5 起有命令入口**（`ngm transform`，见下表）。在此之前它与 v0.4 之前的
`typeDecl` 同型：能力在 adapter 层已实现，却没有任何命令驱动它——于是 `engines.transform`
与 `engines.defaultTransform` 两个配置键对用户没有任何可观察的效果。补上入口时顺带修掉一处
协议缺陷：`genericInvocation` 对 transform **只转发 loader 与 target**，
`--format` / `--minify` / `--sourcemap` 会被静默丢弃（自定义引擎收不到它们）。

---

## 全局约定

- **退出码**：0 成功 / 1 策略失败（含引擎运行失败）/ 2 完整性失败 / 3 配置错误 / 4 Git 网络失败 / 5 引擎不可用；完整定义见[可观测性](../architecture/observability.md)
- **配置文件**：`ngm.json` / `ngm.lock` / `ngm.mappings.json` / `ngm.engines.json` / `~/.ngm/config.json`，见[配置详解](./configuration.md)
- **`--offline`**：禁止网络访问，只用本地 mirror / content store；资源未命中即失败（exit 4）。
  适用于 `verify` / `update` / `install` / `audit` / `outdated`。
- **`--json`**：机器可读输出走 stdout，人类文本与诊断走 stderr；CI **不应**解析人类文本。

---

## 常用组合

CI 门禁（v0.1 可用）：

```bash
ngm install            # 有 ngm.lock → 尊重 lock，不重新解析 ref
ngm verify --json      # 读 driftKind 与退出码：0 通过 / 1 非预期漂移 / 2 完整性失败
ngm build --outfile=dist/app.js
```

完全离线可复现（v0.2 已实现）：

```bash
ngm install --frozen-lockfile --offline
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