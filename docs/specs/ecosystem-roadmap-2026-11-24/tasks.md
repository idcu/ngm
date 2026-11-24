# Tasks：ngm 生态维度开发计划

来源：[spec.md](./spec.md) · 依据 [生态维度路线](../../analysis/ngm-ecosystem-roadmap-2026-11-24.md) · 基线 `6f4bc46`。
状态：**全部待办（14 项）**。每项写清"判据 + 变异检验 + 文件边界"；勾选时必须填证据。

> **实施时改判（2026-11-24，以本节为准）**：
> 1. E4 的 `build --provenance` **不新增 `--json` 输出**：回执是文件，机器读它由 `verify --receipt --json` 承担；避免为了一个旁车文件给 build 引入第二套 stdout 契约。spec §3.1 的"`--json` 时 stdout 只放报告"因此不落地，不视为未完成。
> 2. E13 的 Vite 覆盖是**契约级端到端**：本机无 Vite，用仓库内离线替身加载真实生成的 `vite.config.ts` 并执行其 specifier/entry 逻辑；PATH 上有真 Vite 时额外真跑一次。不宣称 CI 已真跑 Vite（V232 的 1 处 t.Skip 已登记进 skip 台账）。
> 3. E7 的 fail-closed 由 adapter 的 `buildInvocation` 单点实现，内置 postcss 与自定义 CSS 引擎共用；旧判据 `TestPostcss_SaysSoWhenMinifyIsIgnored` 按新语义改写为 `TestPostcss_FailsClosedWhenMinifyIsRequested`。
> 4. E8 的 `expectedVersion`/`expectedDigest`/`supportsMinify` 属于 `adapter.Entry`（`ngm.engines.json`），已登记进 config 字段接线网；ADR-058 入册。
>
> 若源码或 ADR 再推翻假设，先在本节记录改判，再改任务正文；spec 正文保留为决策快照。

## 实施证据（2026-11-24）

| 任务 | 判据 | 变异结果 |
|---|---|---|
| E1 | TestV220ExamplesAreRunnableScaffolds | 删任一 `ngm.json` → 红；恢复 → 绿 |
| E2 | TestV221PositioningCoversFourQuestions | 删"构建谁做"标题 → 红；恢复 → 绿 |
| E3 | TestV222CapabilityMatrixMatchesCode | 矩阵命令改名 / 删 canonical 行 → 红；恢复 → 绿 |
| E4 | TestV223BuildReceiptIsDeterministicAndSidecar | 回执写时间戳 → 同输入字节判据红；恢复 → 绿 |
| E5 | TestV224ReceiptVerifySeparatesIntegrityFromMissingTool | exit 2 改 5 → 红；恢复 → 绿 |
| E6 | TestV225BuildCacheKeyAndSource | 键去掉 mappingsDigest → miss 判据红；恢复 → 绿 |
| E7 | TestV226CssMinifyFailsClosed + TestPostcss_FailsClosedWhenMinifyIsRequested | 撤 fail-closed → 红；恢复 → 绿 |
| E8 | TestV227EngineBindingCodes | 摘要不符改映射 5 → 红；恢复 → 绿 |
| E9 | TestV228MinifyDeterminismScopeIsStated | 删"不跨工具承诺" → 红；恢复 → 绿 |
| E10 | TestV229RunExplainHasNoSideEffects | 让 explain 调 exec → 红；恢复 → 绿 |
| E11 | TestV230TaskOrchestrationNonGoalsAreExplicit | 删非目标条目 → 红；恢复 → 绿 |
| E12 | TestV231ThirdPartyEngineSampleShape | 删 `ngm.engines.json` → 红；恢复 → 绿 |
| E13 | TestV232ViteIntegrationConsumesMappings | alias 生成改空 → 红；恢复 → 绿 |
| E14 | TestV233ActionHasReceiptMode | 删 receipt mode 的 verify 步骤 → 红；恢复 → 绿 |
> **判据编号**：V220–V233，与历史 V001–V219 不冲突。
> **证据格式**：`判据名 + 变异结果`（例如"V223：回执时间戳字段删除后同输入字节一致→红；恢复→绿"）。

---

## 0. 依赖与并行序

| Wave | 任务 | 前置 | 可并行 |
|---|---|---|---|
| W0 | E1 / E2 / E3 | 无 | 三者完全并行 |
| W1 | E4 → E5 → E6 | W0 合入 + spec §3.1 拍板 | E4 之后 E5/E6 可并行 |
| W2 | E7 / E8 / E9 | W1 的 E4 已写入 engine digest | E7/E8/E9 可并行 |
| W3 | E10 → E11 | 无（不改 W1/W2 语义） | 可与 W1/W2 并行 |
| W4 | E12 / E13 / E14 | W1 + W2 | 三者可并行 |

---

## W0 · 可采用性基础

- [x] **E1 三个可跑的示例工程**
  - 目标：`examples/` 下建立 ① `esbuild-ts`（bundle + transform + mappings）；② `local-dir-offline`（local 来源 + 离线 install/verify）；③ `deno-engine`（显式声明 deno 引擎 + `engines validate`）。每个 README 链回唯一权威指南，不复制指南正文。
  - 边界：只新增 `examples/**` 与 `docs/guides/examples.md` 入口；不改 core。
  - 判据 `TestV220ExamplesAreRunnableScaffolds`：三个目录各有 `ngm.json` / README / 可识别的命令序列；`deno-engine` 的 `ngm.engines.json` 显式声明；`local-dir-offline` 不出现 registry/semver 字样。**变异**：删掉任意一个 `ngm.json` → 红；恢复 → 绿。
  - 验收附注：示例的可运行性由 `examples/*/smoke.sh`（或等价脚本）人工/CI 跑一次；判据只守结构，不冒充"已跑过构建"。

- [x] **E2 写"ngm 不是 npm"定位页**
  - 目标：`docs/guides/positioning.md`，直接回答四问：依赖从哪来、构建谁做、压缩谁做、任务谁编排；列 ADR-044/052 的决策边界与"常见误解"。
  - 判据 `TestV221PositioningCoversFourQuestions`：四问标题齐全；明确写出 registry/semver/publish/workspace 不做；明确写出 esbuild/tsc/postcss 与 npm scripts/Just/Taskfile 的外置关系；链接到 capability matrix。**变异**：删"构建谁做"段 → 红；恢复 → 绿。

- [x] **E3 capability matrix 进 docs 门禁**
  - 目标：新增机械判据，确保 [capability-matrix.md](../../internals/capability-matrix.md) 里标记"已实现"的命令/能力在 `commands` 表或 adapter catalog 中真实存在，标记"不做"的仍不在代码中（防止文档漂移和偷偷复活）。
  - 判据 `TestV222CapabilityMatrixMatchesCode`：逐条读矩阵表，命令名映射到 `cmd/ngm/root.go` 的 `commands`，adapter kind 映射到 catalog；remote adapter 声明仍为排除项。**变异**：把 matrix 里某个 done 命令改成不存在的名字 → 红；恢复 → 绿。
  - 边界：只新增判据文件与必要的矩阵小修，不改命令注册。

---

## W1 · 构建回执（内核主菜）

- [x] **E4 `ngm build --provenance` 写 `ngm.build.json`**
  - 目标：按 spec §3.1 输出确定性回执：输入摘要、engine name/kind/version/digest、argv、mappingsDigest、outfile digest；默认不开，显式 flag 开启；`--json` 时 stdout 只放报告，不混回执内容。
  - 文件边界：`cmd/ngm/build.go`、`internal/adapter/*`（如需暴露 engine digest）、新增回执包、cli.md/README/CHANGELOG/ADR（新 ADR-057 构建回执契约）。
  - 判据 `TestV223BuildReceiptIsDeterministicAndSidecar`：同项目两次构建回执字节一致；无时间戳/绝对路径；`ngm.lock` 字节不受影响；engine digest 是文件摘要而非路径；缺引擎时 exit 5 且 hint 点明工具。**变异**：往回执写 `time.Now()` → 同输入字节判据红；恢复 → 绿。
  - 风险缓解：engine digest 用 Go 标准库算，不 spawn `sha256sum`。

- [x] **E5 回执复验入口**
  - 目标：`ngm verify --receipt` 只读重算当前 outfile digest，与回执比对；不重跑构建。产物不符 exit 2，engine 缺失 exit 5，两者分开报。
  - 文件边界：`cmd/ngm/verify.go`、新回执包、exit code 契约、cli.md。
  - 判据 `TestV224ReceiptVerifySeparatesIntegrityFromMissingTool`：篡改产物 → 2；删引擎 → 5；两者同时发生按 ADR-039 完整性压过资源缺失；命令不产生新文件、不启动引擎。**变异**：把 exit 2 改成 5 → 红；恢复 → 绿。
  - 明确不做：跨工具/跨平台字节一致性承诺；回执不上传。

- [x] **E6 本地构建缓存**
  - 目标：按 spec §3.3 的键缓存产物；命中时可解释（`source: cache|built`）；污染时失效并重建一次，仍不符则 exit 2。
  - 文件边界：`cmd/ngm/cache.go`（扩展而非新建第二套缓存语义）、新回执包、cli.md、ADR-057 补记。
  - 判据 `TestV225BuildCacheKeyAndSource`：改输入 / 改 engine / 改 argv / 改平台 / 改 mappings 任一都 miss；其他情况命中且 JSON 写 `source`；污染缓存不会静默返回错误产物。**变异**：从键里去掉 mappingsDigest → 对应 miss 判据红；恢复 → 绿。

---

## W2 · 引擎绑定与压缩

- [x] **E7 CSS 压缩显式引擎与 fail-closed**
  - 目标：`css --minify` 时若当前引擎不支持压缩，exit 3 并提示换显式引擎；不再静默忽略。postcss 的现有警告保留为非 minify 路径的兼容性说明。
  - 文件边界：`cmd/ngm/css.go`、`internal/adapter/engines_extra.go`、guides/build.md、CHANGELOG。
  - 判据 `TestV226CssMinifyFailsClosed`：postcss + minify → 3 且 hint 含 esbuild/lightningcss；声明支持 minify 的引擎 → 0；不带 minify 的既有路径不回归。**变异**：把 return 3 改成 warning → 红；恢复 → 绿。

- [x] **E8 引擎版本与摘要绑定**
  - 目标：`ngm.engines.json` 支持 `expectedVersion` / `expectedDigest`；探测不符分别 exit 5 / 2；字段形状写入 guides/configuration.md 与 ADR-058。
  - 文件边界：`internal/adapter/catalog.go`、`cmd/ngm/engines.go`、configuration.md、field wiring 判据（若进配置结构必须登记）。
  - 判据 `TestV227EngineBindingCodes`：版本不符→5、摘要不符→2、都符合→0、都不填→保持旧行为。**变异**：把 expectedDigest 不符映射成 5 → 红；恢复 → 绿。

- [x] **E9 压缩确定性口径文档**
  - 目标：明确 ngm 只承诺"同一 esbuild minify 输入/参数下回执字节确定"，不承诺跨压缩工具、跨版本字节一致；把现有 ADR-013 探针的证据路径写进 guides/build.md。
  - 判据 `TestV228MinifyDeterminismScopeIsStated`：文档出现"同输入/同参数/同引擎"三要素与"不跨工具承诺"；链到探针脚本。**变异**：删"不跨工具承诺" → 红；恢复 → 绿。

---

## W3 · 任务执行可读（不扩张）

- [x] **E10 `ngm run --explain` 只读计划**
  - 目标：输出 pre→main→post 顺序、来源文件、注入的 PATH 段、当前 `run:script` 授权状态；与 doctor 脚本审批预演共用渲染；固定文案"当前策略会拒绝/放行（未执行）"。
  - 文件边界：`cmd/ngm/run.go`、`cmd/ngm/doctor.go`（共享渲染函数）、cli.md、ADR-042 补记。
  - 判据 `TestV229RunExplainHasNoSideEffects`：不写文件、不启进程、不创建权限、不执行脚本；顺序与真实 `run` 同源；`--explain --json` 形状入表。**变异**：让 explain 调 exec → 红；恢复 → 绿。

- [x] **E11 任务编排非目标固化**
  - 目标：在 capability matrix 与 ADR-042 补记里把"任务图/并行/增量/watch/安装型 run/`.bin` shim"列为非目标，并写清反案门槛（实测收益 + 确定性风险）。
  - 判据 `TestV230TaskOrchestrationNonGoalsAreExplicit`：四个非目标都在文档中；`.bin` shim 仍指向 ADR-043 而非"已支持"；无 `dependsOn` 配置字段。**变异**：把 matrix 的"不做任务图"删掉 → 红；恢复 → 绿。

---

## W4 · 生态接口

- [x] **E12 第三方引擎接入样例**
  - 目标：一个独立示例目录，含 `ngm.engines.json`、最小 subprocess 引擎脚本、`engines validate` 与 `build --engine=<name>` 的可复现命令；证明协议不依赖内置清单。
  - 判据 `TestV231ThirdPartyEngineSampleShape`：样例有自己的清单与脚本；不引用内置引擎名；文档链回 p4 契约。**变异**：删 `ngm.engines.json` → 红；恢复 → 绿。

- [x] **E13 integrations 真引擎覆盖**
  - 目标：把 Vite 那条从"内容断言"升级为端到端：真实生成配置、真实跑一次构建（或最小可离线替身），确认 mappings 被消费。
  - 判据 `TestV232ViteIntegrationConsumesMappings`：配置里有 mappings 推导出的 alias；构建成功且产物存在；用户文件未被覆盖。**变异**：把 alias 生成改成空数组 → 红；恢复 → 绿。

- [x] **E14 CI action 增加 build-receipt 模式**
  - 目标：`action.yml` 增加第四种 mode `receipt`，在 CI 里按 `ngm.build.json` 复验产物；不改现有三 mode 默认值。
  - 判据 `TestV233ActionHasReceiptMode`：四种 mode 齐全；新 mode 命令序列含 `build --provenance` 与 `verify --receipt`；现有 verify mode 默认仍是 content。**变异**：删 receipt mode 的 verify 步骤 → 红；恢复 → 绿。

---

## 完成定义与提交纪律

- 每项一提交；提交信息写 `E<编号> <短标题>`，并在 body 列判据名与变异结果。
- 任一 Wave 未全绿，不开下一 Wave 的代码任务；只允许写文档/判据。
- 每个提交后跑：`go test ./...`（或受影响包全量）、`go vet ./...`、`scripts/check-docs-links.sh`、锚点/gofmt/diff-check/discipline。
- 勾选任务时写证据，不写"已完成"三个字；没有变异检验的任务不得勾选。
- 若实施改判，先在本文顶部"实施时改判"记录，再改判据与代码；spec 正文不回写历史。
