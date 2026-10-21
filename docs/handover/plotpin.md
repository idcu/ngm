# plotpin 整改方案（v3 · 2026-10-17）

> 交给：plotpin 维护者。
> **背景**（一句话）：plotpin 的 `js/common/**` 与几何内核要提取进 lytjs（MIT），再由 ngm 以
> "可证明的 Git 依赖"方式被消费；plotpin 确定走商业化。**它是这条回收路径的验证点，所以先做它**：
> 已经 MIT、行尾已就绪，没有 qforge 那种法律阻塞。完整评估见
> [评估文档](../internals/assessment-2026-10-16-lytjs-ngm-adapt.md)（在 ngm 仓库里）。
> **v3 相比 v2**：补完成定义与整改总表、补**基线测量**、占位符换成[跨仓约定](./README.md)、
> 每项补回滚方式、末尾加可勾选的**验收看板**。

## 0. 完成定义（做到这三条就算完）

1. 有 CI，且它的红绿能反映真实质量（自测 + 产物侧自检）；
2. `js/common/**` 里只留薄适配层，实现体只在 lytjs；
3. **断网**克隆、不跑 `ngm install`、双击 `index.html`：DXF / PDF / 表格导入导出全可用，`ngm verify` 全绿。

## 1. 许可与商业化的决定（已定）

| 项 | 决定 |
|----|------|
| 核心代码（`js/common/**` + `js/app/**`） | **保持 MIT**（`Copyright (c) 2026 idcu`） |
| 商业能力（组织级功能） | **专有**：独立私有仓，或同仓 `enterprise/` 并标 `UNLICENSED` |
| 内容资产（行业符号库、模板库） | **单独许可**，不进公开仓 |
| 公开部署 | **只部署 MIT 核心**；企业版不发布到公开 Pages |
| 退出通道 | 现在就引入 **CLA / DCO** |

**为什么核心保持 MIT**：① 已发布的 MIT 事实上无接收者（0 fork / 0 star / 0 release），此时改许可的
法律收益接近零，却牺牲唯一的获客渠道；② 付费方是组织，买的是责任与集成——核心 MIT 降低试用与内审
门槛，与卖企业版**不冲突**（MIT 不限制你卖任何东西）；③ 真正值钱的是**行业 know-how 与内容**
（符号库、盲区分析口径、对接经验），不是这几千行绘制代码。
**不选 AGPL / BUSL**：目标场景正是内网、涉密、现场无网络——AGPL 的网络交互条款会让采购卡住；
BUSL 则让你失去开源身份。落地前请律师过一遍 CLA / 商标文本。

## 2. 现状读数与基线

| 项 | 读数 |
|----|------|
| 规模 | `js/**` 5,316 行（`js/common/**` 1,258 + `js/app/**` 4,058） |
| 许可 | **MIT**，`LICENSE` 与 `package.json.license` 一致 |
| 行尾 | `.gitattributes` 有 LF 声明，索引与工作树**都是 LF**（三个仓库里唯一不用整改的） |
| 形态 | 非 module 的 IIFE + 全局命名空间；`js/common/**` 从不引用 `js/app/**`（单向依赖） |
| 依赖 | `package.json` 无任何 `dependencies`（`private: true`），只有 `test` 脚本 |
| 测试 | `scripts/selftest.mjs`（约 1,637 行），按 `index.html` 顺序在 Node 里 `vm.runInContext` 全局脚本 |
| **CI** | **没有**（全仓 0 个 workflow 文件） |
| 库标准自检 | 必做失败 **1 条**：`entry-resolvable`（它也不是库，见 R0） |

**基线**：现在就跑一次 `node scripts/selftest.mjs`，把打印的断言数写进本文档——
后面 R6/R8 的"不少于改造前"必须以这个数为准（README 里的 355 / 242 / 595 三个数字互相矛盾，不能用）。

```
node scripts/selftest.mjs
powershell -NoProfile -File <ngm>\scripts\check-library-standard.ps1 -Dir <plotpin>
```

**开工前置检查**（三条命令；期望值来自 **2026-10-19 复核**）：

| 命令 | 期望 |
|------|------|
| `git log -1 --format='%h %ad' --date=short` | `42bbab5 2026-10-15` |
| `git tag --list` | 空（0 个） |
| `git status --short` | 空 |

对不上 ⇒ **仓库已经动过**：先重跑上面的复读命令、更新本节数字，**再**照方案执行。
本仓行尾已由 `.gitattributes` 保证，量之前同样建议 `git config core.autocrlf false`。

## 3. 整改总表（用来排期）

| # | 做什么 | 阻塞谁 | 估时 |
|---|--------|--------|------|
| R0 | 许可基线 + CLA/DCO + 商标声明 | 阻塞"对外交付" | 0.5 天 |
| R1 | README 读数修正（用基线数字） | 不阻塞 | 0.25 天 |
| R2 | 新增最小 CI | **R9 依赖它** | 0.5 天 |
| R3 | 内容资产与部署边界 | 不阻塞 | 0.5 天 |
| R4 | 三层模块边界清单 | **R7 / R8 依赖它** | 0.5 天 |
| R5 | 企业版接线点（只留接口） | 可后置 | 1–2 天 |
| R6 | selftest 支持 vendored 模块 | **R7 的验收靠它** | 1 天 |
| R7 | 用 ngm 装回切片 | **R8 依赖它** | 1 天 |
| R8 | 切换实现并删旧 | 不阻塞 | 0.5–1 天 |
| R9 | CI 补门禁 + 合规交付物 | 不阻塞 | 0.5 天 |
| R10 | （二线）几何状态参数化 | 仅当几何要进框架仓 | 1–2 天 |

**最短路径**：R1 → R2 → R4（约 1.25 天，等切片期间就能做完）；切片到手后 R6 → R7 → R8（约 2.5 天）。

**不做会怎样**（按失效模式排序）：

| 不做的项 | 最坏会发生什么 |
|---|---|
| R0 CLA | 将来想收紧或改许可时**做不到**（已并入的外部贡献无法重新许可） |
| R3 内容与部署 | 企业版被打进公开 Pages——"不部署"不等于"没分发" |
| R4 模块边界 | 商业代码混进 MIT 目录，许可边界说不清 |
| R2 CI | 自测继续只在你自己的机器上跑过——**没人跑的门禁等于没有** |
| R7 / R8 | 离线双击承诺失效；两套实现长期并存 |

**文件级交付清单**（评审时按这张表看 diff）：

| 类型 | 文件 |
|---|---|
| 新增 | `CONTRIBUTING.md` · `TRADEMARK.md` · `docs/MODULE-BOUNDARY.md` · `docs/ASSET-LICENSES.md` · `.github/workflows/test.yml` · `ngm.json` · `ngm.lock` · `ngm.vendor/**` |
| 修改 | `README.md`（许可 + 交付形态 + 断言数）· `index.html`（脚本清单）· `scripts/selftest.mjs`（vendored 前置 + `--against`） |
| 删除 | `js/common/{zip,xml-lite,dsv,xlsx,pdf}.js` 的**实现体**（保留薄适配层） |
| 移出 | 行业符号库 / 模板库（不进公开仓；公开 Pages 只部署核心） |

**我们大概率会怎么搞砸**：

1. **把 selftest 改写成 vitest 而丢掉浏览器桩**——很多断言的价值恰恰是"它在最小桩里也能跑"；
2. **把 `ngm.vendor/` 写进 `.gitignore`**——离线双击承诺当场失效（它是派生物，但必须提交）；
3. **以为"页面不部署企业版"就等于没分发**——只要企业版代码在公开仓，它就已经被分发了；
4. **不记断言基线**——改造后"没少"无法证明，只能靠感觉；
5. **删历史来"重置"**——证据链一起没了，而法律收益接近于零（已发布的 MIT 撤不回）。

## 4. 逐项动作

### R0 · 许可基线与贡献者条款

- 动作：① `LICENSE` 与 `package.json.license` 保持 MIT；README 增一节「许可与商业版」，
  写明"核心 MIT、商业使用与闭源集成都允许、企业版专有"；② 新增 `CONTRIBUTING.md` + **CLA/DCO**；
  ③ 新增 `TRADEMARK.md`，启动商标注册。
- 判据：`LICENSE` / `package.json.license` / README 三处许可名逐字一致；`CONTRIBUTING.md` 与 CLA 存在。
- 回滚：纯新增，`git revert`。

### R1 · README 读数修正

- 动作：把三处矛盾的断言数统一成 §2 的基线数字。
- 判据：README 与 `node scripts/selftest.mjs` 的输出逐字一致。

### R2 · 新增最小 CI

- 动作：新增 `.github/workflows/test.yml`：跑 `node scripts/selftest.mjs`（R9 之后再接产物侧自检）。
- 为什么：自测是它唯一的质量门禁，而**它今天没有任何自动运行的地方**。
- 判据：CI 绿；故意让一条断言失败 ⇒ CI 必须红。

### R3 · 内容资产与部署边界

- 动作：① 行业符号库 / 模板库**不进公开仓**，或标注单独许可；② 公开的在线版（Pages）**只部署 MIT 核心**，
  企业版产物不发布到公开 Pages；③ `docs/ASSET-LICENSES.md` 逐项写清来源与许可。
- 判据：公开 Pages 里没有企业版功能；每个内容资产都有来源与许可记录。

### R4 · 三层模块边界（把"许可边界"变成目录边界）

| 层 | 内容 | 许可 |
|----|------|------|
| `js/core/**` | 要进 lytjs 的内核：`common/zip.js` 101 · `common/xml-lite.js` 93 · `common/dsv.js` 30 · `common/xlsx.js` 113 · `common/pdf.js` 44（二线：`common/dxf.js` 365 · `common/pdf-read.js` 423 · `app/geometry.js` 463） | MIT |
| `js/app/**` | 应用本体：渲染 / 交互 / 面板 / 持久化 / io（3,595 行） | MIT |
| `enterprise/**` | 组织级能力（R5） | 专有（不在公开仓） |

- 动作：写 `docs/MODULE-BOUNDARY.md`（上表 + 每个文件的去处）。
- 判据：`js/core/**` 的每个文件都能对上一个 lytjs 新包；公开仓里**不出现** `enterprise/**`。

### R5 · 企业版接线点（只留接口，不做实现）

- 动作：① **授权**：`LicenseProvider` 接口 + 离线校验（签名 license 文件 + 内置公钥），未授权**只降级**；
  ② **功能开关**：一处集中声明全部商业功能点；③ **审计与交付**：企业版导出带审计信息（沿用既有导出器，
  只加字段）；④ **不动核心数据格式**：工程 JSON 的兼容性由核心维护，企业版只加可选字段。
- 判据：没有 license 文件时核心功能完整；商业功能点集中在一处可列举；工程文件格式向后兼容。

### R6 · 让 `selftest.mjs` 支持 vendored 模块

- 动作：`scripts/selftest.mjs` 现在读 `index.html` 抽 `<script src>` 顺序、拼源码、在最小浏览器桩里
  `vm.runInContext`。加**前置步骤**：`await import()` 薄适配层，再把导出挂到 sandbox 的
  `Zip` / `XmlLite` / `DSV` / `Xlsx` / `Pdf` 上；加 `--against vendored|local` 开关，两种状态都能跑。
- 判据：断言总数**等于** §2 的基线（不是"看起来没少"）。

### R7 · 用 ngm 把通用模块装回来

```
ngm init github.com/idcu/plotpin --runtime=node        # 已有 ngm.json 就跳过
ngm add gitee:lytjs/lytjs#path=packages/common/packages/zip@<标签> --ref-type=tag
ngm add gitee:lytjs/lytjs#path=packages/common/packages/xml@<标签> --ref-type=tag
ngm add gitee:lytjs/lytjs#path=packages/common/packages/dsv@<标签> --ref-type=tag
ngm install
ngm verify
```

- **提交 `ngm.lock` 与 `ngm.vendor/`**：plotpin 的承诺是"无需安装、无需服务器、双击即用"，
  vendored 依赖必须随仓库走；这一步写进 README 的交付说明。
- 企业版：**同一条路**消费同一批 MIT 切片（ngm 不读不存 token，私有仓同样可用）。
- 判据：`ngm verify` 三条全 `✓`；克隆后不跑 `ngm install` 也能双击 `index.html`。
- 回滚：`ngm.json` / `ngm.lock` / `ngm.vendor/` 三个独立提交，可单独 revert。

### R8 · 切换实现并删掉旧的

- 加载方式二选一：(a) 把 `index.html` 里这几个 `<script>` 改成 `<script type="module">`——模块脚本在
  `DOMContentLoaded` 之前执行，而应用在 `DOMContentLoaded` 里启动，时序天然成立；
  (b) 若存在**求值期**引用（不是函数体内引用），改用单文件转换产出经典脚本再照旧加载。
- **先判定**：列出所有对 `Zip` / `XmlLite` / `Xlsx` / `DSV` / `Pdf` 的引用并标注"求值期 / 惰性"；
  **只有全部为惰性才允许走 (a)**。
- 判据：自测全绿（断言数 = 基线）；浏览器 `file://` 双击可用（实测一次）。
- 回滚：**旧文件删在最后一个提交里**，前一个提交是可运行的完整状态。

### R9 · CI 补门禁与合规交付物

- 动作：CI 增加 ①产物侧自检 ②`ngm verify` ③**许可门禁**（GPL/AGPL 即失败）
  ④每个发布版本生成 SBOM 与来源证明。
- 判据：CI 绿；删掉 `ngm.vendor/` 时自检**必须红**；每个版本都有 SBOM 与 attestation。

### R10 · （二线，按需）几何模块的状态参数化

- 现象：`js/app/geometry.js` 有三处直接读运行时状态（背景尺寸、视口缩放、精度设置）——
  搬进框架包之前必须改成**入参**。
- 判据：几何函数在无全局状态下可测（等价于把现有几何断言在 Node 里跑通）。

## 5. 验收看板（逐行勾）

| # | 判据 | 命令 | 期望 |
|---|------|------|------|
| 1 | 自测与基线一致 | `node scripts/selftest.mjs` | 退出 0；断言数 = §2 的基线 |
| 2 | CI 真的在看 | 推一个故意失败的提交 | CI 红（不是绿） |
| 3 | 产物侧合规 | `check-vendored-standard`（见 §2） | 退出 **0**（不是 2） |
| 4 | 依赖可证明 | `ngm verify` | 三条全 `✓` |
| 5 | 离线双击 | 断网 + 克隆 + 不装依赖 + 双击 `index.html` | 导入 DXF、导入 PDF 底图、导出 xlsx 台账、导出多页 PDF 全部成功；控制台无 404 |
| 6 | 商业边界 | `git ls-files` 查 `enterprise/` | 公开仓 0 条；公开 Pages 无企业版功能 |

## 6. 商业化路线

| 收入线 | 内容 | 许可/形态 |
|--------|------|-----------|
| 企业版授权 | **盲区分析、多楼层多底图、设备台账二维码与照片附件、行业符号库、停车场规划模式、集中授权与审计** | 专有 |
| 服务 | 私有部署、定制开发（对接甲方资产/工单系统）、培训、支持 SLA | 合同 |
| 内容 | 行业符号库与模板库 | 单独许可 |

**核心保持 MIT 的部分**（获客渠道）：底图导入与比例尺校准、点设备 / 区域 / 折线标注、编辑与捕捉、
DXF / PDF / 表格导入导出、PNG / SVG / PDF 出图、离线运行。

## 7. 不做的事

- 不把渲染 / 交互 / 面板 / 持久化搬进框架仓。
- 不把企业版功能部署到公开 Pages；不在公开仓出现 `enterprise/**`。
- **不为改许可而删历史**（会失去证据链，收益接近于零）。
- 不在 R7 跑通之前删掉旧实现；不把授权校验做成"未授权就不可用"。
- 不为过标准而放宽判据。

## 8. 交接与待决策

| 方向 | 内容 |
|------|------|
| **我需要 lytjs 提供** | 切片的地址 / `#path=` / 标签；"切片自包含"的说明 |
| **我提供给 lytjs** | ① 8 个 `js/common` 模块的边界样例；② 自测的断言清单与基线数字；③ DXF / PDF 的真实样例文件 |
| **需要 plotpin 自己决策** | ① 企业版放**独立私有仓**还是同仓 `enterprise/`（推荐前者）；② 二线模块（DXF / PDF 读 / 几何）是否也进框架仓；③ 行业符号库与模板的许可与定价 |
