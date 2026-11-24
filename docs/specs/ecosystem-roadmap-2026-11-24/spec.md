# Spec：ngm 生态维度开发计划（2026-11-24）

依据：[ngm 生态维度路线](../../analysis/ngm-ecosystem-roadmap-2026-11-24.md) · 基线 HEAD `6f4bc46`（竞品改进 P0/P1 已合入并推送）。
逐项清单、判据与验收门槛见 [tasks.md](./tasks.md)。

> **本文只定义"做什么、按什么顺序、什么条件才算完成"**，不预先声称任何一项已完成。
> 实施过程中若源码/既有 ADR 推翻本文假设，以 [tasks.md](./tasks.md) 的"实施时改判"记录为准，正文保留为决策快照。

---

## 0. 目标与非目标

**目标**：让 ngm 作为独立生态的**证明底座**可采用——依赖可证明之外，还要让"用了什么引擎、按什么参数生成了什么产物、任务会执行什么"有可读、可验、可离线复核的证据。

**非目标（本 spec 不复活）**：

- 不做 registry / semver 求解 / publish / peer deps / 多版本共存 / workspace / hoisting / 接管 `node_modules`（ADR-044/052）。
- 不自研 bundler、minifier、test runner、Dev Server、HMR、code splitting、remote adapter（ADR-013/020）。
- 不做 Bazel 式任务图、并行/增量/watch 调度、安装依赖型 `run`；`.bin` shim 不重启（ADR-042/043）。
- 不把 provenance 当"内容无害"，不把构建回执当"可复现构建认证"。

---

## 1. 不变量（任何 Wave 都不得破坏）

1. **定位不变量**：[ADR-044](../../adr/adr-044-independent-ecosystem.md) 的边界不可越过；外部工具只经 adapter 接入，不进 core。
2. **证明不变量**：lock / SBOM / attest 的"同 lock 同字节"继续成立；新增构建回执是**旁车文件**，不塞进 `ngm.lock`，不改现有确定性判据。
3. **引擎不变量**：core 不自研引擎；`embed/subprocess/wasm` 契约、`ngm.engines.json` 覆盖机制、mappings 通道不变。remote 仍是排除项。
4. **执行不变量**：`run:script` 默认 deny；新增 `run --explain` 只读、不授权、不执行；退出码 0–6 与 `run` 透传族不变。
5. **JSON 契约**：新增 `--json` 形状必须先写进 [cli.md](../../guides/cli.md) 形状表（`TestV26` 守）；失败信封与退出码一起测。
6. **文档同源**：[capability-matrix.md](../../internals/capability-matrix.md) 是能力唯一真相；新增能力必须同步矩阵、ADR 索引、README、CHANGELOG 与 [roadmap.md](../../internals/roadmap.md)。
7. **判据纪律**：每项带 V220 起的新判据 + 变异检验（撤掉即红、装上即绿）；禁止只加文档不加断言的"支持"。
8. **不引入远端**：构建缓存只做本地；回执不上传；签名仍走用户侧标准工具（[signing.md](../../guides/signing.md)）。

---

## 2. Wave 总览与进入条件

| Wave | 名称 | 内容 | 前置 | 完成门槛 |
|---|---|---|---|---|
| W0 | 可采用性基础 | E1 示例工程 · E2 定位文档 · E3 capability matrix 门禁 | 无 | 3 个示例能离线跑通；能力矩阵进 docs 判据 |
| W1 | 构建回执 | E4 `build --provenance` · E5 回执复验 · E6 本地构建缓存 | W0 合入；§3.1 已拍板 | 回执确定性 + 篡改产物即红 + 缓存命中可解释 |
| W2 | 引擎绑定与压缩 | E7 CSS 压缩显式引擎 · E8 引擎版本/摘要绑定 · E9 压缩确定性口径 | W1 的回执字段就位 | 不支持压缩时 fail-closed；引擎不符 exit 5 |
| W3 | 任务执行可读 | E10 `run --explain` · E11 任务编排非目标固化 | 与 W1/W2 可并行，但不得改 `run` 语义 | 只读计划可测；无执行、无授权泄漏 |
| W4 | 生态接口 | E12 第三方引擎样例 · E13 integrations 真引擎覆盖 · E14 action build-receipt mode | W1 + W2 完成 | 外部引擎跑通；CI 能按回执复验 |

**提交粒度**：一项一提交；W1 的 E4 必须先于 E5/E6；W2 的 E8 依赖 E4 写入的 engine digest 字段。
**并行边界**：W0 与 W3 可与 W1 并行；W2/W4 必须在 W1 之后。
**禁止跨批顺手改**：前一批未合入，不开始后一批代码；只允许先写文档/判据。

---

## 3. 开工前必须拍板的契约决定

### 3.1 构建回执的格式与存放（E4）

现状：`ngm.lock` 证明依赖，不证明产物；`build` 只把 engine argv 拼好交给 subprocess/wasm。
建议：新增旁车文件 **`ngm.build.json`**，顶层形状：

```json
{
  "receiptVersion": 1,
  "inputs": [{ "path": "src/index.ts", "digest": "sha256:..." }],
  "engine": { "name": "esbuild", "kind": "bundle", "version": "0.28.2", "digest": "sha256:..." },
  "argv": ["--bundle", "--alias:..."],
  "mappingsDigest": "sha256:...",
  "output": { "path": "dist/index.js", "digest": "sha256:..." }
}
```

决定点：
- **不放 `ngm.lock`**：lock 是依赖契约，产物回执频繁变化，混入会破坏 lock 字节确定性与判据。
- **不写时间戳**：需要时间由 CI/签名层提供；回执保持同输入同字节。
- **`argv` 按键排序**：沿用 subprocess adapter 的既有约定，避免 map 顺序抖动。
- **`engine.digest` 是工具字节摘要**，不是路径：路径在不同机器不同；工具缺失时 exit 5 并说清缺什么。

实施前必须在 tasks.md 记录最终选择；任何偏离都要先改本节并给出理由。

### 3.2 回执复验的语义（E5）

`ngm verify --receipt`（或独立 `ngm build --verify-receipt`）回答："当前产物是否仍由回执记录的输入+引擎+参数产生？"
建议：
- 只读回执与磁盘产物，重算输出摘要；**不重跑构建**。
- engine 不存在/摘要不匹配 → 与产物不匹配分开报；前者 exit 5，后者 exit 2（完整性失败），不被资源缺失吞掉（ADR-039）。
- 不给"可复现构建"背书：跨工具/跨平台是否字节一致不在承诺内。

### 3.3 构建缓存键（E6）

键建议 = 输入文件 digest 列表 + engine digest + argv + 平台三元组 + mappings digest。
决定点：
- 只做 `~/.ngm/build-cache` 本地缓存；不上传、不共享、不做远端命中。
- 缓存命中必须可在 `--json` 里解释（`source: cache|built`），不能静默。
- 缓存污染按 exit 2 处理（读出的产物与回执输出摘要不符即失效并重建一次，仍不符则红）。

### 3.4 CSS 压缩失败语义（E7）

现状 `postcss` 不支持 `--minify`，ngm 打印忽略后继续。建议改为：
- `css --minify` 且当前引擎声明不支持 → **exit 3**（配置错误，要求显式换引擎），不再静默忽略。
- 由用户声明 `esbuild` 或 lightningcss 等支持 css 压缩的引擎；不内置多个 minifier。

### 3.5 引擎绑定失败语义（E8）

- `expectedVersion` 不符 → exit 5（引擎不可用/不可用版本）。
- `expectedDigest` 不符 → exit 2（完整性失败，压过 5）。
- 两者都不填 → 维持当前探测行为，不做破坏性默认。

### 3.6 `run --explain` 的边界（E10）

- 只读：不执行脚本、不创建文件、不写权限配置、不启动引擎。
- 输出 pre→main→post 顺序、来源文件、PATH 注入、`run:script` 当前是否放行；与 doctor 审批预演共用渲染函数。
- 不引入 `dependsOn`、不并行、不 watch；如未来要反案，先单开 ADR 并给实测收益。

---

## 4. 依赖图（谁先谁后）

```text
E1 示例 ─┐
E2 定位 ─┼─> W0 ─> E4 回执 ─> E5 复验 ─> E6 缓存 ─> W1 ─┬─> E7 CSS 压缩 ─┐
E3 矩阵 ─┘                                               ├─> E8 引擎绑定 ─┼─> W4
                                                        └─> E9 压缩口径 ─┘
E10 run --explain ─> E11 非目标固化 ─> W3（可与 W1/W2 并行）
W1 + W2 ─> E12 引擎样例 ─> E13 真引擎集成 ─> E14 action 回执模式 ─> W4
```

关键阻塞：E4 未落，`engine.digest` 没有写入点，E8 不能做；E4/E5 未落，E14 没有可复验的对象。

---

## 5. 风险与缓解

| 风险 | 触发场景 | 缓解 |
|---|---|---|
| 回执破坏确定性 | 把时间戳/绝对路径写进 JSON | §3.1 明确字段；V220 默认同输入同字节 |
| 构建缓存掩盖真实变更 | 键漏掉 mappings 或平台 | §3.3 全量键；V222 强制改一项即 miss |
| CSS 压缩 fail-closed 影响现有用户 | 现有项目用 postcss + minify | 只在显式 `--minify` 时报错；CHANGELOG 标破坏性 |
| 引擎摘要跨平台不可达 | Windows 上无 sha256sum 类工具 | 用 Go 标准库读文件摘要，不 spawn 外部命令 |
| `run --explain` 被误当成执行 | 输出里带"已批准"字样 | 文案固定为"当前策略会拒绝/放行（未执行）"；V228 断言无副作用 |
| 示例工程变成第二套文档 | 示例与指南漂移 | 示例纳入 docs 判据；每个示例 README 链回唯一权威指南 |

---

## 6. 完成定义（全 Wave 共同）

一个任务只有同时满足以下条件才算完成：

1. 代码落地（如涉及），`go test` 相关包全绿；新增判据带变异检验。
2. 文档同源：capability matrix、README、CHANGELOG、对应 guides/ADR 全部同步；ADR 新增必入索引。
3. `--json` 形状、退出码、usage/help/golden 同步；`TestV26/V22/TestHelpGolden` 全绿。
4. `docs-links`、锚点、gofmt、diff-check、discipline 全绿。
5. tasks.md 勾选并写明证据（判据名 + 变异结果）；若实施中改判，先写"实施时改判"再继续。

## 7. 相关文档

- [生态路线](../../analysis/ngm-ecosystem-roadmap-2026-11-24.md) · [任务清单](./tasks.md)
- [定位边界 ADR-044](../../adr/adr-044-independent-ecosystem.md) · [引擎模型](../../architecture/engine-adapter.md)
- [P4 生态模块](../../modules/p4-ecosystem.md) · [内部路线](../../internals/roadmap.md)
