# ngm 生态维度路线：独立生态靠什么撑起来（2026-11-24）

> **基线**：HEAD `6f4bc46`，已落地竞品改进 P0/P1。本文回答："依赖管理、代码构建、代码压缩、任务执行有没有计划？光 ngm 撑不起来怎么办？"
> **核心判断**：ngm 不是 npm 的替代品，而是**独立来源模型之上的依赖证明层**。
> "独立生态"不等于"自带全部工具"，它要求三份契约立得住：**来源可证明 / 引擎可替换 / 执行可授权**。
> 前两份已有骨架，第三份已有最小实现；缺的是**构建回执、示例工程、引擎版本绑定与压缩/缓存的明确边界**。
> **事实标记**：`[代码]` = 当前 HEAD 可核验；`[文档]` = ADR/spec/roadmap 已有承诺；`[决策]` = 本文拍板；`[空]` = 代码与文档均无。

---

## 1. 四个维度的真实状态

| 维度 | 已实现 | 有规划/缺口 | 刻意不做 |
|---|---|---|---|
| 依赖管理 | 五类 refType、lock v3、三种摘要、离线重放、verify、adopt（5 种锁）、migrate、modules 集合、patches、trustPolicy `[代码]` | universal lock 判据、构建产物与依赖摘要的关联回执 `[文档/空]` | registry、semver 求解、publish、peer deps、多版本共存、workspace/hoisting、接管 `node_modules` `[文档]` |
| 代码构建 | `build/transform/css/typecheck/typedecl`；adapter `embed/subprocess/wasm`；内置 esbuild/tsc/postcss(optional)；`--alias` mappings；wazero/ WASI preview1 `[代码]` | 构建产物缓存 `[空]`；可复现构建仍是 roadmap 候选 `[文档]`；无独立 `bundle` 命令 `[代码]` | 自研 bundler、HMR、code splitting、Dev Server、remote adapter（源码出本机后无法本地证明）`[文档]` |
| 代码压缩 | `build --production` → esbuild `--minify` + `NODE_ENV=production`；`transform --minify/--sourcemap/--loader/--target/--format`；通用引擎可透传 `Extra` `[代码]` | CSS 压缩被 postcss 明确忽略 `[代码]`；无压缩级别、无压缩产物确定性判据 `[空]` | 自研 minifier、terser/uglify 内置 `[决策]` |
| 任务执行 | `ngm run` 平台 shell 语义、pre→main→post、`node_modules/.bin` PATH、五个环境变量契约、`run:script` 默认 deny、退出码原样透传、`doctor` 脚本审批预演 `[代码]` | 无 `run --dry-run`；任务图/并行/增量/watch 明确不做 `[文档]`；vendored `.bin` shim 重启条件未满足 `[文档]` | 安装依赖型 `run`、npm_config 命名空间、Bazel 式任务编排 `[文档/决策]` |

唯一能力 SSOT 是 [capability-matrix.md](../internals/capability-matrix.md)；路线条目在 [roadmap.md](../internals/roadmap.md) 与 [p4-ecosystem.md](../modules/p4-ecosystem.md)。

---

## 2. 战略回答：ngm 不靠"大而全"撑，靠三份契约撑

1. **来源契约（已成型）**：任何依赖都必须落到 commit / dirDigest / 内容摘要，且可离线重放。这是 ngm 不可替代的根。
2. **引擎契约（已成型但偏薄）**：core 不自研引擎；`ngm.engines.json` 允许声明第三方 subprocess/wasm 工具；`integrations` 把 mappings 喂给外部构建器。`[代码]`
3. **执行契约（最小但够用）**：脚本默认不跑；要跑就走平台 shell 并受 `run:` 授权；`doctor` 先预演再放行。
   `[代码]`

据此，生态策略应是：**内核把证明做成不可替代，外层把工具做成可替换**。
把 bundler/minifier/task runner 自己写一遍，既追不上 esbuild/Turbo/Just，又会削弱第 2 份契约。

---

## 3. 路线（按"撑起生态"的边际收益排序）

### Wave 0 · 先让人能用起来（成本最低，优先做）

| 动作 | 为什么 | 落点 |
|---|---|---|
| 建立 3 个可跑的示例工程 | 仓库里目前没有任何示例 `ngm.json`；用户只能读指南猜组合 `[空]` | `examples/` 下：① esbuild + TS 构建；② local-dir 依赖 + 离线 verify；③ Deno 引擎显式声明 + CI action |
| 写一页"ngm 不是 npm"对照 | 大量误解来自把它当包管理器；把 ADR-044/052 的决策翻成用户语言 | `docs/guides/positioning.md`，直接回答"压缩谁做、任务谁编排、registry 为什么没有" |
| 把 capability matrix 升为发布检查 | 避免文档说支持、代码没接（历史上 vendor.commit 就出过这种事） | 接进 `verify:fast` 的 docs 判据 |

### Wave 1 · 构建回执（最值得补的内核能力）

| 动作 | 为什么 | 落点 |
|---|---|---|
| `ngm build --provenance`（或 `--receipt`） | 目前 lock 证明"依赖是什么"，但**不证明产物由谁、用什么参数生成**；这是"撑起构建生态"的最后一块证据 | 输出 JSON：输入文件摘要、engine name/version/path digest、argv（按键排序）、outfile sha256、mappings 摘要；与 SBOM/attest 同口径确定性 |
| 产物摘要写入 lock 旁车文件 | 让 `ngm verify` 能回答"当前产物是否仍对应锁定的输入与引擎" | `ngm.build.json`，不塞进 `ngm.lock`（lock 是依赖契约，不是产物契约） |
| 本地构建缓存 | 现在 `cache` 只清目录，`material_cache` 是进程内；无按输入键的复用 | 键 = 输入摘要 + engine digest + argv + 平台；只做本地，不做远端（远端触发 ADR-013 的证明问题） |

### Wave 2 · 压缩与引擎版本绑定（解决"薄"的问题）

| 动作 | 为什么 | 落点 |
|---|---|---|
| CSS 压缩改由显式引擎提供 | `postcss` 声明不支持 `--minify`，当前被静默忽略；esbuild 有 css loader 但没进 css 清单 `[代码]` | 允许 `css --engine=esbuild` 或用户声明 lightningcss；不支持时 **exit 3 而不是忽略** |
| 引擎版本绑定 | 现在内置清单有版本与 optional，但用户自定义引擎只探测当前 PATH 版本；同一份 ngm.json 两个月后可能用不同引擎产物 | `ngm.engines.json` 支持 `expectedVersion` / `expectedDigest`，探测不符即 exit 5；与构建回执联动 |
| 压缩级别与确定性声明 | `Extra` 能透传任意选项，但没有"哪些压缩选项保证字节确定"的说法 | 文档固定：esbuild `--minify` 在本项目口径下确定性由现有探针背书；跨工具比较不做承诺 |

### Wave 3 · 任务执行（维持最小域，不扩张成 task runner）

| 动作 | 为什么 | 落点 |
|---|---|---|
| `ngm run --list` 已有；补 `--explain` 只读计划 | 用户知道"会执行什么、按什么顺序、哪些被拒"，但不引入依赖图调度 | 输出 pre→main→post 与来源文件、PATH 注入、授权状态；与 `doctor` 审批预演合并为一个只读面 |
| 明确劝退 task graph / watch / 增量 | ADR 与 spec 已排除；若反案，需要先给出并行收益与确定性风险的实测 | 不写 `dependsOn`，不做调度器；复杂编排交给 npm scripts / Just / Taskfile，ngm 只负责授权与证明 |
| `.bin` shim 不重启 | 重启条件（谁生成、谁证明、跨平台语义）未满足 | 保持 ADR-043；用户需要 `.bin` 时用 `integrations` 或外部包管理器共存 |

### Wave 4 · 生态接口（让第三方能接，而不是等官方支持）

| 动作 | 为什么 | 落点 |
|---|---|---|
| 引擎接入样例与契约测试 | p4 文档已有接入协议，但没有一个仓库外的真实例子 | 一个独立 repo 的 `ngm.engines.json` + 最小 subprocess 引擎，跑通 `engines validate` 与 `build --engine` |
| integrations 覆盖真引擎 | Vite/Webpack/Deno 目前只有内容断言，没有真引擎实测 `[文档]` | 至少补 Vite 一条端到端，证明 mappings 真被消费 |
| CI action 增加 build-receipt 模式 | 现在 action 只有 verify/adopt/adopt-check 三 mode `[代码]` | 增加"按回执复验产物"模式，形成依赖证明 → 构建证明 → CI 证明闭环 |

---

## 4. 明确不做（避免把生态做成另一个 npm）

- registry / semver / publish / peer deps / 多版本共存 / workspace / hoisting / 接管 node_modules。
- 自研 bundler、minifier、测试运行器、Dev Server、HMR、code splitting。
- remote adapter：源码离开本机后，用户无法本地证明产物与输入的关系（ADR-013/020）。
- Bazel 式任务图、并行/增量/watch 调度、安装依赖型 `run`。
- 把 provenance 当作"内容无害"，或把构建回执当作"可复现构建认证"——回执只记录事实，不跨工具承诺字节一致。

---

## 5. 一句话结论

**ngm 单独撑不起"包管理器 +  bundler + task runner"的全栈，也不打算撑。**
它能撑起的是：依赖来源不可伪造、引擎可替换且可验证、脚本执行可授权、构建产物有回执。
缺的不是"再写一个 npm"，而是 Wave 0–2 这三件低成本高杠杆的事：**示例、构建回执、引擎版本绑定**。
做完这三件，ngm 可以作为独立生态的**证明底座**存在；再往上，故意留白并接住外部工具。

## 6. 相关文档

- 定位边界：[ADR-044](../adr/adr-044-independent-ecosystem.md)、[ADR-052](../adr/adr-052-local-sources.md)
- 引擎模型：[engine-adapter.md](../architecture/engine-adapter.md)、[p4-ecosystem.md](../modules/p4-ecosystem.md)
- 能力矩阵与路线：[capability-matrix.md](../internals/capability-matrix.md)、[roadmap.md](../internals/roadmap.md)
- 竞品对比：[ngm-competitive-gap-full-2026-11-24.md](./ngm-competitive-gap-full-2026-11-24.md)
