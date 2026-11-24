# ngm 不是 npm：生态定位

ngm 只做依赖证明与构建/执行的可读证据，不做包管理器。边界由 [ADR-044](../adr/README.md) 与 [ADR-052](../adr/README.md) 固定；能力归属的唯一事实源是[能力矩阵](../internals/capability-matrix.md)。

## 依赖从哪来

来自 Git ref（commit / tag / branch，按 commit 钉住并记内容 digest），或仓库内的本地目录与本地 Git 仓（`local-dir` / `local-git`）。`ngm.lock` 记录锚点与摘要，`ngm verify` 复核，不解析版本区间。

**不做** registry 包管理、semver 求解与版本范围、publish、workspace/hoisting、多版本共存、接管 `node_modules`。需要这些时交给 pnpm / npm / yarn 等外部工具，ngm 不复活这套机制。

## 构建谁做

外部引擎做：esbuild 负责 bundle 与 transform，tsc 负责 typeCheck / typeDecl，deno 与 wasm 由项目清单自行声明。ngm 只把 engine、argv、mappings 拼好交给 adapter（subprocess / embed / wasm），不自研 bundler。

**外置**：esbuild / tsc / postcss 都不是 ngm 的内置实现，只是已适配的外部工具条目；工具缺失是 exit 5，不是"检查通过"。

## 压缩谁做

外部工具做：CSS 编译与压缩交给 postcss 或其它显式声明的引擎，代码压缩交给 esbuild 等引擎的原生选项。ngm 不自研 minifier；引擎声明不支持压缩时应显式失败（exit 3），而不是静默产出"假装压缩过"的产物。

## 任务谁编排

项目脚本由外部编排器发起：npm scripts / Just / Taskfile 负责"什么时候跑什么"，ngm 只负责被调度的单条命令及其证明（`ngm run <script>` 默认 deny，`ngm run --list` 只读列出）。ngm 不做任务图、并行、增量、watch、安装依赖型 run，也不做 `.bin` shim。

**外置**：npm scripts / Just / Taskfile 是编排层，不属于 ngm core；`ngm run` 不是它们的替代品，只是"被它们调用时如何把执行说清楚"。

## 常见误解

- "ngm 能替代 npm install + semver" —— 不能；依赖来源是 Git / 本地目录，不是版本区间。
- "内置清单 = ngm 自研引擎" —— 不是；esbuild / tsc / postcss 只是已适配的外部工具条目。
- "`ngm run` 能编排任务" —— 不能；它执行单条脚本，编排仍由 npm scripts / Just / Taskfile 外置负责。
- "remote adapter 已支持" —— 没有；remote 按决定排除，见[能力矩阵](../internals/capability-matrix.md)。

## 相关文档

- [示例工程](./examples.md) · [构建指南](./build.md) · [配置详解](./configuration.md) · [能力矩阵](../internals/capability-matrix.md)
- [ADR 索引](../adr/README.md)
