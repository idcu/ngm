# qforge 整改方案（v3 · 2026-10-17）

> 交给：qforge 维护者。
> **背景**（一句话）：qforge 的通用模块（ZIP / XML / OOXML / CSV）要提取进 lytjs（MIT），再由 ngm
> 以"可证明的 Git 依赖"方式被消费；qforge 确定走商业化。完整评估见
> [评估文档](../internals/assessment-2026-10-16-lytjs-ngm-adapt.md)（在 ngm 仓库里）。
> **v3 相比 v2**：补完成定义与整改总表、补**基线测量**（否则"断言数不减"无法判定）、
> 把占位符换成[跨仓约定](./README.md)、每项补回滚方式、
> 末尾加可勾选的**验收看板**。

## 0. 完成定义（做到这三条就算完）

1. 公开仓有 `LICENSE` + CLA + 商标声明，且**没有来源不明的题目数据**；
2. 通用模块只存在于 lytjs（本地只剩薄适配层，`js/core/**` 的实现体已删）；
3. **断网**克隆仓库、不跑 `ngm install`、双击 `index.html`：导入导出全可用，`ngm verify` 全绿。

## 1. 许可与商业化的决定（已定）

| 项 | 决定 |
|----|------|
| 核心代码（`js/core/**` + `js/app/**`） | **MIT** |
| 商业能力（组织级功能） | **专有**：独立私有仓，或同仓 `enterprise/` 并标 `UNLICENSED` |
| 内容资产（真实题库、行业模板） | **单独许可**，不进公开仓 |
| 品牌 | 注册名称与 Logo，并写 `TRADEMARK.md`（**MIT 不授予商标权**） |
| 退出通道 | 现在就引入 **CLA / DCO** |

理由：① 付费方是组织，买的是责任、合规、集成与内容——**没有许可证的仓库，客户法务那关过不去**；
② 四个项目要是一条可解释的线（ngm / lytjs 是 MIT，内核也 MIT，商业能力专有）；
③ 真正会被抄走的是**内容与 know-how**，不是这几千行代码。
**不选 AGPL / BUSL**：会挡住"内网、涉密、政企"客户或失去开源身份；而它是离线单机，被云厂商白嫖的风险极低。
落地前请律师过一遍 `LICENSE` / CLA / 商标文本。

## 2. 现状读数与基线

| 项 | 读数 |
|----|------|
| 规模 | `js/**` 约 9,612 行 / 54 个文件，全部是 `(function (global) {…})(window)` 的经典脚本，靠 `index.html` 顺序加载 |
| 元数据 | **无 `LICENSE`**、**无 `package.json`**、**无 `.gitattributes`**、无构建步骤 |
| 测试 | `scripts/` 下 17 个 `*.test.mjs`（自建 harness，按 `index.html` 顺序用 `new Function` 垫 `window`）；CI 是 Node 20/24 × UTC / Asia-Shanghai，**不装依赖** |
| 断言数 | README 写 2,296 条，但**没有一次统一的统计**——所以 R0 之后先量一次并写进本文档 |
| 库标准自检 | 必做失败 **6 条**：`pkg-json-exists` · `pkg-name` · `pkg-version` · `entry-resolvable` · `license` ·（行尾那条见下） |
| 行尾**不是**仓库问题 | 索引里 CRLF 文件数 **0**，工作树 102 个——`core.autocrlf=true` 的检出产物。**不要**去"转换"任何文件 |

```
git -C <qforge> ls-files --eol | Select-String 'w/crlf' | Measure-Object -Line
powershell -NoProfile -File <ngm>\scripts\check-library-standard.ps1 -Dir <qforge>
```

**开工前置检查**（三条命令；期望值来自 **2026-10-19 复核**）：

| 命令 | 期望 |
|------|------|
| `git log -1 --format='%h %ad' --date=short` | `58e33ac 2026-10-15` |
| `git tag --list` | 空（0 个） |
| `git status --short` | 空 |

对不上 ⇒ **仓库已经动过**：先重跑上面的复读命令、更新本节数字，**再**照方案执行。
量行尾前先 `git config core.autocrlf false`（否则你会看到 102 个"CRLF"，而索引里是 0）。

## 3. 整改总表（用来排期）

| # | 做什么 | 阻塞谁 | 估时 |
|---|--------|--------|------|
| R0 | 许可 + CLA + 商标声明 + **测试总入口**（同时得到基线数字） | 阻塞一切代码搬运 | 0.5 天 |
| R1 | 内容资产隔离（真实题库移出公开仓） | R9 的合规物会列它 | 0.5 天 |
| R2 | `.gitattributes` | 不阻塞 | 0.25 天 |
| R3 | `package.json`（显式 CJS）+ `npm test` | 不阻塞 | 0.25 天 |
| R4 | 三层模块边界清单 | **R6 / R7 依赖它** | 0.5 天 |
| R5 | 企业版接线点（只留接口） | 可后置 | 1–2 天 |
| R6 | harness 支持 vendored 模块 | **R7 的验收靠它** | 1 天 |
| R7 | 用 ngm 装回切片 | **R8 依赖它** | 1 天 |
| R8 | 切换实现并删旧 | 不阻塞 | 0.5–1 天 |
| R9 | CI + 合规交付物 | 不阻塞 | 0.5 天 |

**最短路径**（等 lytjs 的切片期间就能做完）：R0 → R2 → R3 → R4 → R6，约 2.5 天；
切片到手后再 R7 → R8，约 2 天。

**不做会怎样**（按失效模式排序，用来判断优先级）：

| 不做的项 | 最坏会发生什么 |
|---|---|
| R0 许可 | 客户法务直接否；**代码无法合法分发**（今天就是"保留所有权利"） |
| R1 内容资产 | 真实题库随 MIT 一起公开，且可能侵害第三方权利——**这一步不可撤回** |
| R4 模块边界 | 商业代码混进 MIT 目录，"哪部分是 MIT"从此说不清；拆分成本随时间上升 |
| R6 测试夹具 | 切换时没有可对照的尺子，"断言没少"只能靠肉眼 |
| R7 / R8 | "离线可证明、双击即用"退回成口号；两套实现长期并存 |

**文件级交付清单**（评审时按这张表看 diff）：

| 类型 | 文件 |
|---|---|
| 新增 | `LICENSE` · `TRADEMARK.md` · `CONTRIBUTING.md` · `.gitattributes` · `package.json` · `docs/MODULE-BOUNDARY.md` · `samples/README.md` · `scripts/run-all-tests.mjs` · `ngm.json` · `ngm.lock` · `ngm.vendor/**` |
| 修改 | `README.md`（许可 + 交付形态）· `index.html`（脚本清单）· `scripts/_harness.mjs`（vendored 前置）· `.github/workflows/test.yml`（自检 + verify + 许可门禁 + SBOM） |
| 删除 | `js/utils/zip-writer.js` · `js/utils/zip-reader.js` · `js/utils/xml-lite.js` 的**实现体**（保留薄适配层） |
| 移出 | `samples/` 里的真实题库（进私有内容仓或企业版仓） |

**我们大概率会怎么搞砸**（写在前面，比事后复盘便宜）：

1. **把 CRLF 当仓库缺陷去批量转换**——索引里本来就是 LF，转一遍只会制造一个巨大的无意义 diff；
2. **把 `enterprise/` 放进公开仓**——"哪部分是 MIT"从此说不清，客户与贡献者都会误读；
3. **改 `index.html` 时忘了求值期引用**——某些脚本在**加载时**就快照了 `QB.*` 上的对象，改成 module
   后它们拿到 `undefined`，而测试用的是旧加载顺序，看不出问题；
4. **在 R7 之前删旧实现**——半搬状态让仓库不可运行，回滚要动两个提交；
5. **让 license 校验在未授权时直接不可用**——核心用户被"惩罚"，与 open-core 的定位相反。

## 4. 逐项动作

### R0 · 许可与商业化基线（**最先做**）

- 动作：
  1. 新增根 `LICENSE`（MIT 全文 + `Copyright (c) 2026 idcu`）；
  2. README 增一节「许可与商业版」：核心 MIT、**商业使用与闭源集成都被允许**、企业版专有；
  3. 新增 `TRADEMARK.md`（名称/Logo 使用规范），并启动商标注册（通常第 9 类软件、第 42 类 SaaS）；
  4. 新增 `CONTRIBUTING.md` + **CLA/DCO**（提交即同意以当前许可发布，并授予项目所有者重新许可的权利）；
  5. 新增 `scripts/run-all-tests.mjs`：遍历 `scripts/*.test.mjs` 逐个 spawn，最后打印
     **断言总数与失败数**；`package.json` 的 `scripts.test` 指向它。
- 为什么第 5 条在 R0：后面 R6/R8 的"断言数不减"需要一个**可复现的基线**；
  README 里的 2,296 是手抄的，不是一次运行的输出。
- 判据：`LICENSE` / `package.json.license` / README 三处许可名逐字一致；`npm test` 退出 0 并打印总数，
  把这个数记到本文档 §5 的看板里。
- 回滚：纯新增文件，`git revert` 即可。

### R1 · 内容资产隔离（法律与商业的双重前置）

- 动作：`samples/` 下的**真实题库**（538 / 541 题那几份）移出公开仓（进私有内容仓或企业版仓）；
  公开仓只留小型自造样例；新增 `samples/README.md`，逐文件写清**来源**与**许可**两列。
  若数据来自第三方或客户，先确认授权再决定去留。
- 判据：公开仓里不存在来源不明的题目数据；每个样例文件都有来源与许可。
- 回滚：文件移动是独立提交；`git revert` 会把它放回去（但**授权问题不会因此消失**，先确认再动手）。

### R2 · `.gitattributes`

- 动作：加根 `.gitattributes`（照抄 plotpin 的 13 行：`* text=auto eol=lf` + 图片/压缩包标 binary）。
- 判据：自检的 `gitattributes-lf` 通过；`git ls-files --eol` 里 `i/lf` 与 `w/lf` 一致。

### R3 · `package.json`

- 动作：`name` / `version` / `license` / `private: true` / `engines.node` / `scripts.test`，
  并**显式**写 `"type": "commonjs"`（`js/**` 是经典脚本；`scripts/*.mjs` 不受影响）。
- 判据：`pkg-json-exists` / `pkg-name` / `pkg-version` 通过。**`entry-resolvable` 仍会红**——
  qforge 是**应用**不是库，这条是形态不适用（ngm 侧补角色，见 [ngm.md](./ngm.md) 的 N2）。

### R4 · 三层模块边界（把"许可边界"变成目录边界）

| 层 | 内容 | 许可 |
|----|------|------|
| `js/core/**` | 要进 lytjs 的内核：`utils/zip-writer.js` 123 行 · `utils/zip-reader.js` 99 · `utils/xml-lite.js` 221 · `importers/csv-importer.js` 的 `parseRows`/`detectDelimiter` · `modules/office-writer.js` 120 · `importers/xlsx-importer.js` 130 · `importers/docx-importer.js` 139 | MIT |
| `js/app/**` | 应用本体：判分 / 题库 / 画布 / 交互 / 存储 / 统计 / 组卷等其余 47 个文件 | MIT |
| `enterprise/**` | 组织级能力（R5）：集中授权与审计、合规导出、与甲方系统对接、内容服务 | 专有（不在公开仓） |

- 动作：写 `docs/MODULE-BOUNDARY.md`（上表 + 每个文件的去处）。
- 判据：`js/core/**` 的每个文件都能对上一个 lytjs 新包；公开仓里**不出现** `enterprise/**`。

### R5 · 企业版接线点（只留接口，不做实现）

- 动作：
  1. **授权**：`LicenseProvider` 接口 + 离线校验（签名 license 文件 + 内置公钥）；未授权**只降级、不报错**；
  2. **功能开关**：一处集中声明（`QB.FEATURES`）列出全部商业功能点；
  3. **数据出口**：审计/合规导出沿用既有 `EXPORT-FORMAT`，企业版只加字段、不加分支；
  4. **后端契约**：`data/api-adapter.js` 与 `API-CONTRACT.md` 已预留前后端分离——服务端从这条契约长出来。
- 判据：**没有** license 文件时核心版功能完整；商业功能点集中在一处可列举。

### R6 · 测试夹具支持 vendored 模块（旧实现还在时先做）

- 动作：改 `scripts/_harness.mjs`——它按 `index.html` 的 `<script src>` 顺序读文件、用 `new Function`
  造 `window` 后执行。加**前置步骤**：`await import()` 薄适配层，再把导出挂到 sandbox 的
  `QB.ZipWriter` / `QB.ZipReader` / `QB.XmlLite` / `QB.decodeUtf8` / `QB.OfficeWriter` 上。
  同时给 `run-all-tests.mjs` 加一个 `--against vendored|local` 开关，两种状态都能跑。
- 判据：17 个套件全绿，断言总数**等于 R0 记下的基线**（不是"看起来没少"）。

### R7 · 用 ngm 装回通用模块

```
ngm init github.com/idcu/qforge --runtime=node        # 已有 ngm.json 就跳过
ngm add gitee:lytjs/lytjs#path=packages/common/packages/zip@<标签> --ref-type=tag
ngm add gitee:lytjs/lytjs#path=packages/common/packages/xml@<标签> --ref-type=tag
ngm add gitee:lytjs/lytjs#path=packages/common/packages/dsv@<标签> --ref-type=tag
ngm install
ngm verify
```

- **提交 `ngm.lock` 与 `ngm.vendor/`**：qforge 的承诺是"双击即用、离线可用"，vendored 依赖必须随仓库走。
- 企业版：**同一条路**消费同一批 MIT 切片（ngm 不读不存 token，私有仓同样可用）——内核只有一份。
- 判据：`ngm verify` 三条全 `✓`；克隆后不跑 `ngm install` 也能双击运行。
- 回滚：`ngm.lock` / `ngm.vendor/` / `ngm.json` 是三个独立提交，`git revert` 回到"只声明、未安装"的状态。

### R8 · 切换实现并删掉旧的

- 加载方式二选一：
  - (a) 把这几个 `<script>` 改成 `<script type="module">`——模块脚本在 `DOMContentLoaded` 之前执行，
    而应用在 `DOMContentLoaded` 里启动，时序天然成立；
  - (b) 若存在**求值期**引用（不是函数体内引用），改用单文件转换产出经典脚本再照旧加载。
- **先判定**：列出所有对 `QB.ZipWriter` / `QB.ZipReader` / `QB.XmlLite` / `QB.decodeUtf8` /
  `QB.OfficeWriter` 的引用并标注"求值期 / 惰性"；**只有全部为惰性才允许走 (a)**。
- 判据：17 个套件全绿（断言数 = 基线）；浏览器 `file://` 双击可用（实测一次）。
- 回滚：**旧文件删在最后一个提交里**，前一个提交是可运行的完整状态——这是本项的回滚点。

### R9 · CI 与合规交付物

- 动作：CI 增加 ①产物侧自检 ②`ngm verify` ③**许可门禁**（出现 GPL/AGPL 依赖即失败）
  ④每个发布版本生成 SBOM 与来源证明（`ngm sbom` / `ngm attest`）并随版本存档。
- 判据：CI 绿；删掉 `ngm.vendor/` 时自检**必须红**；每个版本都有 SBOM 与 attestation。

## 5. 验收看板（逐行勾）

| # | 判据 | 命令 | 期望 |
|---|------|------|------|
| 1 | 测试与基线一致 | `npm test` | 退出 0；断言总数 = §2 记下的基线 |
| 2 | 许可与清单 | `check-library-standard`（见 §2） | `license` / `pkg-*` / `gitattributes-lf` 通过；`entry-resolvable` 记"不适用"（N2 落地前为红，需在 README 写明原因） |
| 3 | 依赖可证明 | `ngm verify` | 三条全 `✓` |
| 4 | 产物侧合规 | `check-vendored-standard`（见 §2） | 退出 **0**（不是 2） |
| 5 | 离线双击 | 断网 + 克隆 + 不装依赖 + 双击 `index.html` | 导入 xlsx/docx、导出 xlsx/docx、备份恢复全部成功；控制台无 404 |
| 6 | 商业边界 | `git ls-files` 查 `enterprise/` | 公开仓 0 条 |

## 6. 商业化路线

| 收入线 | 内容 | 许可/形态 |
|--------|------|-----------|
| 企业版授权 | 集中授权与审计、成绩与合规导出、与甲方系统对接、题库内容服务 | 专有 |
| 服务 | 私有部署、定制开发、培训、支持 SLA | 合同 |
| 内容 | 行业题库与模板 | 单独许可 |

**建议的商业版功能**（从现有能力里挑，不新造需求）：批量组卷与考试管理、成绩与合规归档、
与甲方系统的 REST 对接（契约已预留）、题库内容订阅、离线部署与升级服务。

## 7. 不做的事

- 不把商业代码放进公开仓；不在公开仓出现 `enterprise/**`。
- 不把领域逻辑（判分 / 题库 / 画布 / 交互）搬进 lytjs——它们不是通用能力。
- 不把授权校验做成"未授权就不可用"：降级，不惩罚。
- 不为过标准而给 `index.html` 造一个 JS 入口；不在 R7 跑通前删旧实现。

## 8. 交接与待决策

| 方向 | 内容 |
|------|------|
| **我需要 lytjs 提供** | 切片的地址 / `#path=` / 标签，以及"切片自包含（内部依赖已内联）"的说明 |
| **我提供给 lytjs** | ① ZIP / XML / OOXML / CSV 的边界样例；② 断言清单与基线数字；③ 许可允许范围内的真实文件夹具 |
| **需要 qforge 自己决策** | ① 企业版放**独立私有仓**还是同仓 `enterprise/`（推荐前者）；② `samples/` 真实题库的授权与去处；③ 商标注册的范围与类别 |
