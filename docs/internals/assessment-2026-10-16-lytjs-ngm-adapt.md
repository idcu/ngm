# 评估：把 qforge / plotpin 的通用模块融入 lytjs，并由 ngm 消费（2026-10-16）

> **结论**：方向可行，但**不能按原样执行**。三条实测硬约束决定了路径必须改形——
> lytjs 的 76 个包**今天一个都不能被 ngm 消费**（入口指向未提交的 `dist/`）；
> ngm **不解析 workspace 依赖**，而 lytjs 包之间的联系全是 `workspace:*`；
> qforge **没有许可证**，它的代码在搬进 MIT 仓库之前不可分发。
>
> **推荐路径**：`提取通用内核 → 融入 lytjs 的 L0（不改主线发布形态）→ 在 lytjs 开一条
> ngm 适配分支，交付「产物化切片」→ 用 ngm 在真实 Gitee 上消费一个切片`。
> 整条路径**不触碰 ngm core 一行代码**，因此与
> [ADR-006：为什么 lytd 并入 ngm（及并入后的边界）](../adr/adr-006-lytd-merge.md) 不冲突——
> 方向相反：lytjs 在这里是**被消费方**，不是 ngm 的能力来源。

---

## 0. 事实源与复读方式

这份评估的判据不是记忆，而是三处可复读的读数：

| 读数 | 命令（在 ngm 仓库根执行） |
|------|---------------------------|
| 库标准体检（逐条，退出码 1 = 不符合标准） | `bash scripts/check-library-standard.sh <dir>`；Windows 入口 `powershell -NoProfile -File scripts\check-library-standard.ps1 -Dir <dir>` |
| 产物侧体检（消费方视角） | `bash scripts/check-vendored-standard.sh <project-dir>` |
| 行尾真伪（仓库事实 vs 检出产物） | `git -C <dir> ls-files --eol` + `git -C <dir> config core.autocrlf` |

分析用的三个仓库副本位于 `d:/repos/mine/{lytjs,qforge,plotpin}`。为了区分「仓库事实」与
「这台机器检出时产生的东西」，在**本地分析副本**上把 `core.autocrlf` 设为 `false` 并重新检出
（不涉及任何上游仓库）。第 2 节记录这次校准的读数——**它改变了一条工单**。

**2026-10-19 复核**：四个仓库自 10-16 起**没有新提交**（qforge `58e33ac` · plotpin `42bbab5` ·
lytjs `1d546c89`，都停在 2026-10-15），lytjs **没有** `ngm-adapt` 分支；ngm 的 HEAD 仍是 v0.99 的提交，
工作树有 32 处改动（v0.100 / v0.101 在飞）。因此本页的读数仍然成立；[四份交接方案](../handover/README.md)
里每份都带「开工前置检查」，用来确认读到的是同一状态。

---

## 1. 三个仓库的基线（实测）

| 仓库 | 规模 | 许可证 | 代码形态 | 库标准体检的必做失败 |
|------|------|--------|----------|----------------------|
| **qforge**（idcu/qforge，题库与刷题） | `js/**` 约 9,612 行 / 54 个文件 | **无** | 非打包的 54 个 IIFE 全局脚本，`<script>` 顺序加载；无 `package.json`、无 `.gitattributes`、无构建 | **6 条**：`pkg-json-exists` · `pkg-name` · `pkg-version` · `entry-resolvable` · `license` ·（行尾那条见第 2 节） |
| **plotpin**（idcu/plotpin，图纸标注） | `js/**` 5,316 行（`js/common/**` 1,258 + `js/app/**` 4,058） | **MIT**（Copyright (c) 2026 idcu） | 同样是非 module 的 IIFE + 全局命名空间，`js/common/**` 与 `js/app/**` **单向依赖**（common 从不引用 app） | **1 条**：`entry-resolvable`。LF / 许可证 / 无钩子 / `.gitattributes` / 无子模块 / 无 LFS 指针全部通过 |
| **lytjs**（lytjs/lytjs，前端框架） | 76 个包；275 个 `*.test.ts`；pnpm monorepo（含嵌套 workspace glob）+ tsup + vitest + changesets | MIT | TypeScript 源码，入口全部指向 `dist/`，而根 `.gitignore` 排除 `dist/` | 根：`no-install-hooks`（根 `package.json` 的 `prepare: husky`）+ `workspace-manifest-unreadable`（4 条 `packages` 写法读不出）；**叶子包：`entry-file-exists` 1 条** |

叶子包的逐条读数（`@lytjs/common-string`，一份可复读的样本）：

```
✔ entry-resolvable    入口 ./dist/index.mjs（来自 package.json，与 InferEntry 三层同口径）
✖ entry-file-exists   声明了但仓库里没有：main ./dist/index.mjs, types ./dist/index.d.ts
✔ dist-committed      入口指向构建产物（./dist/index.mjs）且未被 .gitignore 排除
✖ esm-first           未声明 type（module 或 commonjs）
✖ engines             没声明 engines
✔ license             license = MIT
```

同一条目从**仓库根**体检时 `dist-committed` 会转红（根 `.gitignore` 排除 `dist/`）。
两处读数合起来才是完整的事实：**入口文件不存在，且被忽略规则挡着**。

---

## 2. 一次读数校准：CRLF 不是仓库事实

第一轮体检里，qforge 与 lytjs 都报了 `lf-only` 含 CRLF。**先量再改**（这是本仓库的老规矩）：

| 仓库 | `core.autocrlf` | 索引里 CRLF 的文件 | 工作树里 CRLF 的文件 | 跟踪文件总数 |
|------|-----------------|--------------------|----------------------|--------------|
| lytjs | `true` | **0** | 1,709 | 1,796 |
| qforge | `true` | **0** | 102 | 112 |
| plotpin | `true` | **0** | **0** | 30 |

索引是「git 里真正存着什么」，工作树是「这台机器检出成了什么」。三个仓库的索引里
**一个 CRLF 都没有**；plotpin 因为有 `.gitattributes`（`* text=auto eol=lf`）连检出都没被转换。
关掉 autocrlf 重新检出后：lytjs 的 `lf-only` **转为通过**；qforge 仍报 2 处，
而删掉其中一个文件重新取出后它**也转为通过**——那两处是 `git checkout -f -- .` 没有重写
（stat 缓存）造成的，不是内容事实。

**这条校准改变的是工单内容**：要做的不是「转换 1,709 个文件」，而是
**在仓库根补 `.gitattributes`**，让任何机器上的检出结论一致。标准页也写着这条边界
（[JS Git 库的标准](../guides/js-git-library-standard.md) §必做 9：自检看的是工作树字节）。

---

## 3. 模块提取评估

分级依据是「是否依赖浏览器宿主」与「是否属于某个应用领域」，不是「代码在哪」。

### 3.1 首批（推荐，约 750–950 行）

| 模块 | 来源与行数 | 建议归属 | 依据 |
|------|-----------|---------|------|
| ZIP 读写（stored 写 + raw-deflate 读 + CRC32） | plotpin `js/common/zip.js` 101 ⊕ qforge `js/utils/zip-writer.js` 123 + `zip-reader.js` 99 | `@lytjs/common-zip` | 纯字节 + 标准 Web API；lytjs 全仓**没有** ZIP/CRC32 实现（搜索命中的两处是 "min+gzip" 散文） |
| XML（OOXML 子集） | plotpin `js/common/xml-lite.js` 93 ⊕ qforge `js/utils/xml-lite.js` 221 | `@lytjs/common-xml` | 纯字符串解析；**README 必须写明「OOXML 子集，不是通用 XML」**，否则会被误用 |
| DSV（CSV/TSV） | plotpin `js/common/dsv.js` 30 ⊕ qforge `js/importers/csv-importer.js` 的 `parseRows`/`detectDelimiter` | `@lytjs/common-dsv` | 纯字符串；需补「生成」一侧才算对称 |
| OOXML/XLSX 读写（± DOCX 读） | plotpin `js/common/xlsx.js` 113 ⊕ qforge `xlsx-importer.js` 130 + `office-writer.js` 120（+ `docx-importer.js` 139） | `@lytjs/common-ooxml` | 合并前必须**删掉应用域硬编码**：plotpin 版把表头关键词写死在读取路径里 |
| PDF 生成（图像页） | plotpin `js/common/pdf.js` 44 | `@lytjs/common-pdf` | 极小、通用；依赖 `atob`，需做能力探测 + 明确报错 |

### 3.2 二线（按需，约 1,765 行）

| 模块 | 来源与行数 | 前置改造 |
|------|-----------|----------|
| 计算几何（世界坐标 + 捕捉 + 形状互换） | plotpin `js/app/geometry.js` 463 | 3 处读运行时状态（背景/缩放/精度），必须先参数化 |
| DXF R12 ASCII 读写 | plotpin `js/common/dxf.js` 365 | 纯文本 + 数学，独立性最好 |
| PDF 读取（矢量路径 / 文本 / 栅格重组成 PNG） | plotpin `js/common/pdf-read.js` 423 | 自带能力边界（不支持 CID 字体、CMYK、LZW、压缩对象流），边界要一并搬 |
| FSRS-4.5 调度器 | qforge `js/modules/fsrs.js` 261 | 与框架无关的纯算法；**是否放进框架仓需要单独裁决** |
| FSRS 训练核心（数值梯度 + Adam） | qforge `js/modules/fsrs-train-core.js` 253 | 同上，且受众更窄 |

### 3.3 明确不提取

- **题库领域逻辑**：判分策略（199 行）、答案规范化（63）、去重键（93）、导入适配器与行式解析（`adapters.js` 529 + `line-parser.js` 300）、导出注册表（547）。
- **IndexedDB 资产库**：qforge `utils/assets.js` 196 / plotpin `js/common/content-assets.js` 89——两套都强依赖 IndexedDB，且与 lytjs 已有的 `common-storage` / `common-node-cache` 定位重叠。可提取的只有「内容哈希 + dataURL 转换」这一小块纯逻辑，存储适配留给宿主。
- **UI / 画布层**：qforge `app.js` 1002、`ui-renderer.js` 732、`question-editor.js` 516；plotpin `render.js` 595、`interaction.js` 712、`io.js` 1000。

### 3.4 提取时最容易做错的一步

两个仓库对 ZIP / XML / OOXML / DSV / 内容寻址资产各有一份**同源但已分家**的实现
（例如 CRC32 双方都用标准向量 `0xCBF43926` 断言，但 XML 一份 93 行、另一份 221 行）。
**「择优保留」会引入行为回归**。做法照 v0.97/v0.98 的先例：把两套实现放进**同一批语料**
（两仓 `samples/` 里的真实 xlsx / docx / qbk 文件 + 标准向量 + 边界样例），逐条比结论，
只有「逐条一致」才允许合并——**判据先于代码**。

---

## 4. 融入 lytjs：落点与工程改动

**落点**：首批进 L0 —— `packages/common/packages/{zip,xml,dsv,ooxml,pdf}`（34 → 39 个包）；
二线进一个**新分组** `packages/ecosystem/packages/kit/packages/*`（它们是「能力包」，不是框架原语）。

**每个新包按 `packages/_templates/` 的约定生成**（`package.json` / `tsup.config.ts` / `tsconfig.json` /
`src/index.ts` / `tests/` / `README.md`），并补齐模板里已有的 `files` / `sideEffects` / `exports` 条件导出。

**必须同步改的工程点**（不改则新包进不了门禁，或门禁的覆盖面静默变小）：

| # | 位置 | 实测现状 | 动作 |
|---|------|---------|------|
| 1 | `pnpm-workspace.yaml` | 已有嵌套 glob（common / ecosystem 下的子分组） | 新分组要加一条 glob |
| 2 | `scripts/build-order.ts` | 76 个包的构建顺序表 | 新包必须入表，否则构建顺序漏 |
| 3 | `scripts/check-zero-deps.ts` | 扫描范围只含 `packages/{common,plugins,ecosystem}/packages`，且**只查 `dependencies`**（不查 `devDependencies`） | 放在新分组里会**落在扫描范围之外**；要么扩范围，要么把包放在已被覆盖的 glob 内 |
| 4 | `scripts/check-deps.ts` 的 `LAYER_MAP` | **已过期**：仍列着 `@lytjs/lytx`、`@lytjs/ai` 等本仓不存在的包 | 加新包层级之前先修它，否则新包没有层级归属 |
| 5 | `PUBLISH-MANIFEST.md` / `.size-limit.json` | 76 个包的清单与体积预算 | 补行、加预算 |
| 6 | 测试与构建 | vitest + tsup | 断言从两仓的 `selftest.mjs` / `*.test.mjs` 迁到 vitest；按涉及模块估约 600–1,000 条断言 |

**移植的固定四步**：① IIFE → ESM + TS（剥包装、加导出、补类型，保留中文注释）；
② 宿主耦合改成能力探测 + 明确报错（`atob` / `DecompressionStream` / `Blob`）；
③ 与另一仓实现做**对照网**；④ 断言迁到 vitest 并保留原来的边界样例。

---

## 5. lytjs 的 ngm 适配分支：三种形态

分支**不改源码管线**，它交付的是「可被 vendor 的消费形态」。三条形态的取舍：

| 选项 | 做法 | 致命点 |
|------|------|--------|
| **A 源码入口** | 每个包的 `ngm.json` 把 `main` / `types` 指向 `./src/index.ts` | 消费者必须能编 TS，还要自己解析全部内部包；类型入口的语义也变了 |
| **B 提交 dist** | 分支上由 CI 构建并提交 `dist/`，`.gitignore` 开白名单 | digest 随构建产物波动；76 个包的产物入库量巨大；**仍然解决不了 `workspace:*` 边不可见** |
| **C 自包含切片（推荐）** | 用打包器把 `@lytjs/core` 及其全部 `@lytjs/*` 内部依赖**内联**成少数几个切片（core / ui / ssr / router） | 失去包粒度与部分树摇；需要一条 ADR 说明「npm 形态是 76 个包，ngm 形态是切片」 |

**为什么推荐 C**：ngm 的模型有三个已实测的性质——不替依赖构建（安装钩子默认 deny，
指向不存在的入口就是没有入口）、传递依赖**只读仓库根的 `ngm.json`**（见
[依赖解析](../architecture/dependency-resolution.md) §传递性依赖，实现是
`internal/resolve/graph.go` 的 `readUpstreamDeps`）、monorepo 子路径只落地子目录。
在这三条之下，「一条声明就能用」的形态只能是**自包含**的。A 与 B 都会把「解析内部依赖」
这件本该由产物体承担的事推给消费者。

**分支改造清单**（每条都有机械判据）：

1. 入口指向**已提交的文件** ⇒ 消掉唯一的必做失败 `entry-file-exists`；
2. 每个切片自带 `ngm.json`（`schemaVersion` / `name` / `version` / `runtime` 四项必须过
   `ngm config validate`），`main` / `types` 指向产物；
3. 内部依赖边若要自动展开，只能写在**仓库根 `ngm.json`** 里（边只读根清单）；
   否则由消费者显式声明；
4. 去掉根 `package.json` 的 `prepare: husky`（钩子默认不执行，且自检判它必做失败）；
5. 仓库根补 `.gitattributes`（`* text=auto eol=lf`）——让任意机器上的行尾结论一致（见第 2 节）；
6. 补 `type` / `engines` / `files`（推荐项，成本极低）；
7. **不可变语义化标签**：仓库内可见的标签最新到 `v6.7.0`，而根版本号是 `6.9.6`，
   两条线并不对齐 ⇒ 适配分支需要自己的版本线与标签，且一旦被别人锁定就**永不移动**；
8. 两侧自检进 CI：库侧 `check-library-standard`，消费侧 `check-vendored-standard`。

---

## 6. ngm 侧的消费与验证

闭环序列（在真实 Gitee 上，靠本地镜像或离线路径**不算**验证了抓取）：

```
ngm init github.com/my-org/offline-app --runtime=node
ngm add gitee:lytjs/lytjs#path=<切片路径>@<适配分支标签> --ref-type=tag
ngm install
ngm verify
ngm mappings validate
ngm build --outfile=dist/app.js
```

四条断言（缺一条这次消费就没被证明）：

1. `ngm.lock` 里该依赖的 commit 与上游标签解析出的 commit 一致（`refType` 真的是 `tag`）；
2. `ngm verify` 报 `✓ match`，且 `--deep` 也过（字节级）；
3. `ngm.mappings.json` 的 `path` 与落地的子路径一致，入口来自切片的 `ngm.json`；
4. 裸导入能被 `ngm build` 解析进 `ngm.vendor` ——这是 ngm 在引擎之上唯一多加的那件事。

两侧自检都要跑：库侧保证「上游按标准长」，消费侧保证「我拿到的产物也按标准长」——
后者的退出码 2 表示「一个依赖都没查」，与「全查过且都合格」必须分得开。

---

## 7. 实施顺序总览

顺序由两条依赖决定：**① qforge 的许可证阻塞它的代码被搬运；② lytjs 的适配分支不依赖任何提取
工作**，而它的结论决定新包的产物形态。于是次序是：

```
(0) qforge 补许可证 · 两仓补 .gitattributes 与清单
 └─>(1) lytjs 适配分支：先用**现有**的包做通一个切片 ──> ngm 在真实 Gitee 上消费它
      └─>(2) lytjs 首批三个新包（zip / xml / dsv）+ 两仓对照网
           └─>(3) plotpin 先回收（MIT、行尾已就绪、无法律阻塞）
                └─>(4) qforge 回收（index.html 与测试夹具的耦合更多）
                     └─>(5) OOXML / PDF 生成 ──> 第二轮回收
                          └─>(6) 二线（几何 / DXF / PDF 读取 / FSRS，按需）
```

**每一步的产物都要能被下一步消费**：适配分支的形态决定新包的产物目录；新包的标签决定回收时
写进 `ngm.json` 的版本；两个应用回收成功后，才允许删除本地旧实现。

**若目标只是「让 qforge / plotpin 复用这些库」**：把 (2) 与 (3) 提前——先做只含
zip / xml / dsv 的最小切片，价值在「离线、可证明的依赖链」，不在包的数量。

---

## 8. 详细实施方案（四个项目，按文件与步骤）

### 8.1 阶段 0：四个前置（不做完，后面全是返工）

| # | 项目 | 动作（具体文件） | 判据 |
|---|------|-----------------|------|
| 0.1 | **qforge** | 新增根 `LICENSE`（MIT）+ `CONTRIBUTING.md` 与 CLA/DCO + `TRADEMARK.md`，并把**真实题库数据移出公开仓**——许可已决策（见第 10 节），这一步**阻塞 qforge 的一切代码被搬运** | 自检的 `license` 项转通过；公开仓里没有来源不明的题目数据 |
| 0.2 | **qforge** | 新增根 `.gitattributes`：照抄 plotpin 的 13 行（`* text=auto eol=lf` + png/jpg/zip/xlsx 标 binary） | 关掉 `core.autocrlf` 重新检出后 `lf-only` 通过；开着 autocrlf 时读数不再被误读 |
| 0.3 | **qforge** | 新增根 `package.json`：`name` / `version` / `license` / `private` / `engines` / `scripts.test`，并**显式**写 `"type": "commonjs"`（`js/**` 是经典脚本；`scripts/*.mjs` 不受影响） | `pkg-json-exists` / `pkg-name` / `pkg-version` 转通过。**`entry-resolvable` 仍会红——那不是缺陷，见 8.6 的 N2** |
| 0.4 | **lytjs** | 环境基线：Node ≥ 22、pnpm ≥ 9，`pnpm install`，并跑通 `pnpm build` / `pnpm test` / `pnpm type-check` / `pnpm check-zero-deps` / `pnpm gate` | 五条命令在本地基线全绿——这是后面所有改动的参照 |
| 0.5 | **qforge / plotpin** | README 读数修正：qforge「支持 6 种题型」与实际 7 种；plotpin 的断言数三处互相矛盾（355 / 242 / 595） | 改成**脚本实际打印的那个数**，README 与它逐字相同 |

### 8.2 阶段 1：lytjs 的 ngm 适配分支（先做，因为它决定形态）

分支：`git switch -c ngm-adapt v7.0.0-dev`。第一版**只做一个切片**（挑内部依赖最少的一个包）。

| # | 动作（具体文件） | 判据 |
|---|-----------------|------|
| 1.1 | 新增 `scripts/build-ngm-slices.ts`：用该包现有的 tsup/esbuild 配置产出**自包含**切片（`noExternal: [/^@lytjs\//]`），输出 `packages/<pkg>/ngm-dist/index.mjs` + `index.d.ts` | 产物里不再出现 `@lytjs/` 的裸导入（一次全文搜索即可证明） |
| 1.2 | 产物目录命名为 `ngm-dist/`，**不要用 `dist/`**（根 `.gitignore` 第 7 行排除后者）；坚持用 `dist/` 就必须写白名单 | 从**仓库根**体检该子包时 `dist-committed` 通过——这正是第 1 节两处读数不一致的那条 |
| 1.3 | 新增 `packages/<pkg>/ngm.json`：`schemaVersion` / `name` / `version` / `runtime` / `main: "./ngm-dist/index.mjs"` / `types: "./ngm-dist/index.d.ts"` | 该目录下 `ngm config validate` 通过；`entry-file-exists` 转通过 |
| 1.4 | 新增**根** `ngm.json`（四项必填，`dependencies` **留空**）。理由：传递依赖只读仓库根那一份，留空可让消费者显式声明，避免"消费一个包却拉进整仓" | 根体检不再因缺清单而红；消费方 `ngm.lock` 里只出现它声明过的依赖 |
| 1.5 | 根 `package.json` 删掉 `"prepare": "husky"`——**只在适配分支上删**，主线保留贡献者钩子 | `no-install-hooks` 转通过 |
| 1.6 | 新增根 `.gitattributes`（同 0.2） | `gitattributes-lf` 转通过，且子包的行尾判据跨机器一致 |
| 1.7 | 补推荐项：根与切片都写 `engines`；切片写 `"type": "module"` 与 `files` 范围 | `esm-first` / `engines` / `publish-scope` 转通过 |
| 1.8 | CI 新增一个 job：标签推送时构建切片并**提交产物**，然后打标签。标签建议 `v7.0.0-ngm.1`，但**先用自检器量一次**预发布后缀能否过语义化标签那条判据（本文档第 8 节的未验证项） | 标签指向包含产物的那个提交；再推一个提交后，标签**一动不动** |
| 1.9 | 在 ngm 侧消费它（第 6 节的四条断言）。消费前先允许 `net:gitee.com`——按 v0.76 的实操记录，首次 install 会被权限系统按设计拦下 | `ngm verify` 报 `✓ match`；`ngm build` 能解析裸导入；两侧自检通过 |

### 8.3 阶段 2：lytjs 首批三个新包（zip / xml / dsv）

| # | 动作（具体文件） | 判据 |
|---|-----------------|------|
| 2.1 | 按 `packages/_templates/` 生成骨架：`packages/common/packages/{zip,xml,dsv}/{package.json,tsup.config.ts,tsconfig.json,src/index.ts,tests/,README.md}` | `pnpm build` 后三个包各自产出 `index.mjs` / `index.cjs` / `index.d.ts` |
| 2.2 | `scripts/build-order.ts` 的 `BUILD_ORDER` 加入三个包（放在 L0 段、其依赖之前） | `pnpm build` 不报顺序错误 |
| 2.3 | **先修** `scripts/check-deps.ts` 的 `LAYER_MAP`（仍列着本仓不存在的包），再登记新包层级 | 层级检查不报"新包没有归属" |
| 2.4 | `PUBLISH-MANIFEST.md` 加三行；`.size-limit.json` 加三条预算 | 发布清单与体积门禁通过 |
| 2.5 | 移植（每包固定四步）：IIFE → ESM + TS；宿主耦合改成能力探测（`atob` / `DecompressionStream` / `Blob`，缺失时报明确错误）；补类型与 JSDoc；保留中文注释 | `pnpm type-check` 零错误；`pnpm check-zero-deps` 通过（三个包都落在已覆盖的 `packages/common/packages` 下） |
| 2.6 | **对照网**：`packages/common/packages/<name>/tests/reference/` 放两仓原实现（只读参考，注明来源与许可证），`tests/parity.test.ts` 用同一批语料跑两边逐条比结论（CRC32 用标准向量 `0xCBF43926`，XML / ZIP / DSV 用两仓的边界样例） | 对照全绿；**任何一处不一致都必须先解决，不许登记成"等价"** |
| 2.7 | 断言迁移：plotpin `scripts/selftest.mjs` 的第 2/3/4/5/6 小节、qforge `scripts/xml-lite.test.mjs` 的 37 条，转成 vitest | 迁移后的用例数不少于原小节；应用侧原套件仍全绿 |
| 2.8 | 三个新包加进 1.1 的切片清单，按 1.8 打新标签 | 与 1.9 同一套断言 |

### 8.4 阶段 3：plotpin 先回收（它是更简单的那个）

选它先做：MIT 已就绪、行尾已由 `.gitattributes` 保证、没有许可证阻塞。

| # | 动作（具体文件） | 判据 |
|---|-----------------|------|
| 3.1 | 新增 plotpin 的 `ngm.json`（项目清单：`name` / `version` / `runtime` / `dependencies` 三条带 `#path=` 的声明） | `ngm config validate` 通过 |
| 3.2 | `ngm add gitee:lytjs/lytjs#path=<切片路径>@<标签> --ref-type=tag`（三条），然后 `ngm install` | `ngm.lock` 记录三个 commit 与 digest；`ngm verify` 全 `✓` |
| 3.3 | **提交 `ngm.vendor/`**：它是派生物，但静态站点的离线承诺要求它随仓库走。这一步要写进 README 的交付说明 | 克隆仓库后**不跑** `ngm install` 也能双击运行 |
| 3.4 | 三个本地实现改成**薄适配**：`js/common/zip.js` 等保留文件名与全局名，内容改为"取 vendored 切片的实现并挂到 `window.Zip`"。加载方式二选一：<br>(a) 把这三个 `<script>` 改成 `<script type="module">`——模块脚本在 `DOMContentLoaded` 之前执行，而应用是在 `DOMContentLoaded` 里启动的，时序天然成立；<br>(b) 若存在**求值期**引用（不是函数体内引用），先用单文件转换产出 IIFE 经典脚本再照旧加载 | 先**列出**所有对 `Zip` / `XmlLite` / `Xlsx` / `DSV` 的引用并标出"求值期 / 惰性"；只有全部为惰性才允许走 (a) |
| 3.5 | 改 `scripts/selftest.mjs`：它按 `index.html` 顺序 `vm.runInContext` 全局脚本。加一个前置步骤：`await import()` 那几个 module 适配层，再把导出挂进 sandbox | `node scripts/selftest.mjs` 打印的断言总数不少于改造前 |
| 3.6 | 新增最小 CI（plotpin **今天没有 CI**）：一个 job 跑 `node scripts/selftest.mjs` 与产物侧自检 | CI 绿；把 `ngm.vendor/` 删掉时产物侧自检**必须红** |
| 3.7 | 删除已被替代的本地实现体 | 行数减少量与提取量相符；测试全绿 |

### 8.5 阶段 4：qforge 回收（耦合更多，放在 plotpin 之后）

| # | 动作（具体文件） | 判据 |
|---|-----------------|------|
| 4.1 | 同 3.1–3.3：`ngm.json` + 依赖声明 + 提交 `ngm.lock` / `ngm.vendor/` | 同 3.2 |
| 4.2 | 改 `scripts/_harness.mjs`：它现在"读 `index.html` 的 `<script src>` 顺序、用 `new Function` 垫 `window`"。加一个前置：加载 vendored 切片并挂到 sandbox 的 `QB.ZipWriter` / `QB.ZipReader` / `QB.XmlLite` / `QB.decodeUtf8` 等名字上 | 17 个测试套件**零改动**地全绿 |
| 4.3 | 改 `index.html` 的脚本清单（第 41–103 行那一段）：三个通用实现改为 module 适配层或 IIFE 产物（判定同 3.4） | 浏览器里 `file://` 双击可用——这是两仓的核心承诺 |
| 4.4 | 删掉旧实现体（`js/utils/zip-writer.js` / `zip-reader.js` / `xml-lite.js`） | `js/**` 行数下降；冒烟套件（419 条）全绿 |
| 4.5 | CI（`.github/workflows/test.yml`）增加产物侧自检与 `ngm verify` | CI 绿；删除 `ngm.vendor/` 时红 |

### 8.6 阶段 5：OOXML / PDF 生成 → 第二轮回收；阶段 6：二线与 ngm 侧改进

**阶段 5（OOXML / PDF）**：

| # | 动作 | 判据 |
|---|------|------|
| 5.1 | 建 `@lytjs/common-ooxml`：**合并** plotpin 的 `xlsx.js`（读写）与 qforge 的 `office-writer.js`（xlsx/docx 写）+ `xlsx-importer.js`（多表读）+ `docx-importer.js`（docx 读）。合并前先删掉 plotpin 读取路径里的**应用域硬编码**（表头关键词） | 两个应用的 `samples/` 真实文件往返一致 |
| 5.2 | 拟定公开 API 并写进包 README（避免两套心智模型）：`readXlsx(bytes)` 返回多表、`writeXlsx(sheets)`、`readDocx(bytes)` 返回段落 + 图片字节、`writeDocx(blocks)`；**图片只以字节返回，不绑定任何存储**（IndexedDB 归应用） | 类型与 JSDoc 齐备；应用侧不再各自适配 |
| 5.3 | 建 `@lytjs/common-pdf`（plotpin 的 44 行图像 PDF 生成） | 多页 PDF 结构断言通过 |
| 5.4 | 按 1.8 打标签；两个应用各做一轮回收（同 3.3–3.7 / 4.2–4.5） | qforge 的 office-export 套件（静态 59 条，README 记 1,148 条动态）与 plotpin 的 XLSX / PDF 小节全绿 |

**阶段 6（二线，按需）**：几何（先做 3 处 `state` 参数化）→ DXF → PDF 读取 → FSRS
（**先裁决归属**：把与框架无关的算法放进框架仓会稀释它的定位，建议独立仓或单列分组）。
每项的判据同 2.5–2.7。

**ngm 侧：本次评估捞出的三条改进**（都不阻塞上面的流程，可穿插做，每条都要先立判据）：

| # | 缺口（实测） | 建议 | 判据（牙齿） |
|---|-------------|------|-------------|
| N1 | 行尾判据在 Windows 检出下会**误报**：三个仓库的索引里 CRLF 文件数为 0，而工作树里 lytjs 有 1,709 个（`core.autocrlf=true` 的产物） | 自检器报 CRLF 时**同时报索引侧的行尾**（`git ls-files --eol` 的 `i/` 与 `w/`），把"仓库事实"与"检出产物"分开说 | 一个 `core.autocrlf=true` 的临时仓库夹具 ⇒ 报告写明"索引为 LF，这是检出产物" |
| N2 | 标准只有**包 / monorepo 根 / 嵌套**三种角色，缺"**应用**（不是库）"：qforge 这类静态站点会永远红 `entry-resolvable` | 要么加一个应用角色（入口判定为 `index.html`），要么在标准页写明"应用形态不适用这条判据"，并让自检器接受一个角色参数 | 应用形态夹具 ⇒ 该条记"不适用"并**打印原因**（照 v0.76 / v0.79 的"不适用"先例） |
| N3 | **消费方会静默少装**：`checkUpstreamPackageJSON` 只在 `ngm.json` **不存在**时被调用（`internal/resolve/graph.go` 第 483 行），且只对 `looksLikeGitSpec` 的写法报警（同文件第 562 行）——`workspace:*` 一条都不匹配。lytjs 的切片**两份清单都有**，于是那次检查连发生都不会发生 | 上游 `package.json` 的 `dependencies` / `devDependencies` 里出现 `workspace:` 前缀时，无论 `ngm.json` 是否存在都给一条警告，点名这些依赖并提示"请显式声明对应的 `#path=` 子路径" | 夹具：`package.json` 声明 `"@x/y": "workspace:*"` 且**有** `ngm.json` ⇒ 期望一条警告点名 `@x/y`；删掉该条目 ⇒ 警告消失。**先测量当前行为，再决定做不做** |

### 8.7 每一步都不做的事（防返工）

- **不在 lytjs 主线上删 `prepare` 或提交产物**——那是适配分支的形状，主线要保留贡献者钩子与
  76 个包的发布形态。
- **不在提取完成前删除两个应用里的旧实现**——半搬状态会让两个仓库同时不可运行。
- **不把 `dist/` 加进 lytjs 主线**：那条红是"主线不是消费形态"的正确读数，不是缺陷。
- **不为让测试变绿而放宽判据**（先例：v0.84 不放宽 `git://`，v0.94 不为 `subPath` 放宽地址校验）。
- **不把两仓的领域逻辑**（判分 / 题库 / 画布 / 交互）**搬进框架仓**——它们不是通用能力。

---

## 9. 风险与诚实边界

**法律**：qforge 没有许可证 ⇒ 它的代码在搬进 MIT 仓库之前**不可分发**（这是前置条件，不是偏好）。
plotpin 是 MIT，搬运时保留版权声明。

**血统**：两仓同源分家的实现，合并的风险不在功能，而在**行为差异**——没有对照网就不该合并。

**发布面**：lytjs 已是 76 个包 + changesets + size-limit 的仓库；新增包会扩大发布与维护面。
二线必须独立分组（`kit`），避免「框架被非框架需求牵着走」。

**形态冲突**：C 选项与「8 层 76 个包」的哲学不完全一致。建议写成一条 ADR：
「npm 形态 76 个包；ngm 形态切片」——把不一致变成**写下来的选择**，而不是含糊。

**备选方案（更小的爆炸半径）**：把提取物放进独立仓（例如 `idcu/kit`），代价是失去 lytjs 的
gate / changesets / CI / vitest 设施。它的目标是「两个应用复用」，不是「框架长大」。

**本方案不触碰 ngm core**，与 ADR-006 一致；建议在文档里补一句边界说明，
防止后人把「lytjs 适配 ngm」误读成「lytjs 的能力并入 ngm」。

**未验证项（如实记录）**：

1. 未在 lytjs 上执行安装与构建（本次勘察是只读的），因此切片的**实际体积与构建耗时为未测量**；
2. 76 个包的**全量**体检输出没有被完整保存——本次只读到根与样本包的逐条读数；
3. 「真实公网消费」尚未发生，需要一次真人推送（与 v0.94/v0.95 的边界同类）；
4. 适配分支的标签是否允许预发布后缀（例如 `v7.0.0-ngm.1` 能否过标准的语义化标签判据）
   **未验证**——先用自检器量一次再定版本线。

---

## 10. 需要拍板的决策

| # | 决策 | 选项 |
|---|------|------|
| 1 | 适配分支的名字与语义 | `ngm-adapt` / `release/ngm`；它只做产物化，还是允许改主线形态 |
| 2 | 消费形态 | A 源码入口 / B 提交 dist / **C 自包含切片（推荐）** |
| 3 | 包归属 | 首批进 L0、二线进新分组 `kit` —— 是否同意 |
| 4 | 许可证 | **已决策（2026-10-16）**：qforge / plotpin 确定商业化 → 走 open-core——**核心 MIT、商业能力专有、内容资产单独许可**，两个应用立即引入 CLA/DCO 保留退出通道；逐项目的落地步骤见[交接方案](../handover/README.md) |
| 5 | 顺序 | 先做 P3 的最小闭环（三个包 + 一次真实消费），还是先做满 P1/P2 |

---

## 相关文档

- [JS Git 库的标准](../guides/js-git-library-standard.md)（本文档所有「必做失败」的判据出处）
- [依赖解析](../architecture/dependency-resolution.md)（传递依赖的唯一来源 · 子路径语义）
- [依赖管理](../guides/dependency-management.md)（`refType` / 子路径 / 漂移三态）
- [ADR-006：为什么 lytd 并入 ngm](../adr/adr-006-lytd-merge.md)（本文档的边界：方向相反）
- [ADR-008：archiveDigest 的定义](../adr/adr-008-archive-digest.md)（为什么行尾与产物确定性是硬约束）
- [ADR-009：供应链策略](../adr/adr-009-supply-chain-policy.md)（钩子默认 deny）
- [外部逐行评估（2026-10-09）](./assessment-2026-10-09.md)（同类评估的写法先例）
