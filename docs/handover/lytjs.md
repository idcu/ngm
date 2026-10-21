# lytjs 整改方案（v3 · 2026-10-17）

> 交给：lytjs 维护者。
> **背景**（一句话）：两个自有项目的通用模块要提取成 lytjs 的新包；同时 lytjs 要以"可被 ngm 消费"的
> 形态对外提供依赖。ngm 是 **Git-first 依赖证明层**：**不替依赖构建**（安装钩子默认不执行）、
> 要求依赖的**入口文件已提交在仓库里**、标签不可变、传递依赖只读仓库根的 `ngm.json`、
> 且**不解析 `workspace:` 依赖**。完整评估见
> [评估文档](../internals/assessment-2026-10-16-lytjs-ngm-adapt.md)（在 ngm 仓库里）。
> **v3 相比 v2**：修正 D3（见下方"一处更正"）、补完成定义与总表、加**阶段门禁**（A 通过才做 B）、
> 占位符换成[跨仓约定](./README.md)、末尾加**验收看板**。

> **一处更正（v2 → v3）**：v2 的 D3 写的是"把 `check-zero-deps` 从 `dependencies` 扩到
> `devDependencies`"——**那是错的**：76 个包的 `devDependencies` 里本来就有 tsup / typescript / vitest，
> 扩过去会当天全红。正确的口径见 v3 的 D3：**零第三方只约束 `dependencies`**；另加一条**许可黑名单**
> 门禁，才同时覆盖两类依赖。

## 0. 完成定义（做到这三条就算完）

1. `ngm-adapt` 分支上至少有**一个**切片能被 ngm 在真实 Gitee 上消费（`verify` 全绿、裸导入可构建）；
2. L0 新增 `zip` / `xml` / `dsv` 三个包，各自带 LICENSE、对照网、vitest 用例，且过 lytjs 全部门禁；
3. 主线**不受影响**：`pnpm build` / `test` / `type-check` / `check-zero-deps` / `gate` 与改造前一致。

## 1. 现状读数（实测，可复读）

| 项 | 读数 |
|----|------|
| 规模 | 76 个包（全部 `6.9.6`），275 个 `*.test.ts` |
| 工程 | pnpm workspace（含嵌套 glob）+ 每包 tsup + vitest + changesets；根 `package.json` 有 `prepare: husky` |
| **入口形态** | 76 个包的入口全部指向 `./dist/index.mjs` / `./dist/index.d.ts`，而根 `.gitignore` 排除 `dist/`，**仓库里没有任何 dist 产物** |
| 库标准自检 | 根：`no-install-hooks`（`prepare`）+ `workspace-manifest-unreadable`（4 条 packages 写法读不出）；**叶子包：`entry-file-exists` 1 条** |
| 依赖边 | 各包之间是 `workspace:*`（抽样 25+ 包全部如此），**ngm 看不见这些边** |
| 零依赖门禁 | `scripts/check-zero-deps.ts` 只扫 `packages/{common,plugins,ecosystem}/packages`，且**只查 `dependencies`** |
| 层级表 | `scripts/check-deps.ts` 的 `LAYER_MAP` **已过期**：仍列着 `@lytjs/lytx`、`@lytjs/ai` 等本仓不存在的包 |
| 标签 | 仓库内可见的标签最新到 `v6.7.0`，而根版本号是 `6.9.6`——两条线并不对齐 |

```
powershell -NoProfile -File <ngm>\scripts\check-library-standard.ps1 -Dir <lytjs>\packages\common\packages\string
```

**开工前置检查**（三条命令；期望值来自 **2026-10-19 复核**）：

| 命令 | 期望 |
|------|------|
| `git log -1 --format='%h %ad' --date=short` | `1d546c89 2026-10-15` |
| `git tag --list` | 17 个，最新 `v6.7.0` |
| `git branch -a` | **没有** `ngm-adapt`（本方案要新建） |

对不上 ⇒ 仓库已经动过：先重跑上面的复读命令、更新本节数字，**再**照方案执行。

**一条对 A8 有用的事实**：现有 17 个标签止于 `v6.7.0`，**`v7.0.0` 从未被打过**——所以
`v7.0.0-ngm.1` 不会与既有标签冲突；仍需先用自检器验证预发布后缀能否过语义化标签判据。

## 2. 整改总表与阶段门禁

| 组 | 做什么 | 估时 | 门禁 |
|----|--------|------|------|
| **A** | ngm 适配分支：切片构建、清单、`.gitattributes`、CI、标签 | 2–3 天 | **A 的验收不过，不要开始 B**（形态未定就铺包，产物要重做） |
| **B** | 首批三个新包（zip / xml / dsv）+ 对照网 | 3–6 天 | **先用 `zip` 一个包跑通全部判据**，再铺开 `xml` / `dsv` |
| **C** | 二批两个新包（ooxml / pdf） | 3–6 天 | B 的对照网全绿后才开始 |
| **D** | 许可、来源与商业化前提 | 0.5 天 | 与 A/B 并行，**不要拖到发布前** |

---

## 3. A · ngm 适配分支

分支：`git switch -c ngm-adapt v7.0.0-dev`。**第一版只做一个切片**，把链路跑通再铺开；
**主线不动**——这是"产物化的消费分支"，不是新的主线形态。

| # | 动作（具体文件） | 判据 |
|---|-----------------|------|
| A1 | 新增 `scripts/build-ngm-slices.ts`：复用该包现有 tsup/esbuild 配置，产出**自包含**切片（`noExternal: [/^@lytjs\//]`），输出 `ngm-dist/`（路径按[约定](./README.md)） | 产物里不再出现 `@lytjs/` 的裸导入（见验收看板第 1 条） |
| A2 | 产物目录用 `ngm-dist/`，**不要用 `dist/`**（根 `.gitignore` 第 7 行排除后者） | 从**仓库根**体检该子包时 `dist-committed` 通过 |
| A3 | 新增 `packages/<pkg>/ngm.json`：四项必填 + `main` / `types` 指向 `ngm-dist/` | 该目录下 `ngm config validate` 通过；`entry-file-exists` 通过 |
| A4 | 新增**根** `ngm.json`（四项必填，`dependencies` **留空**）：传递依赖只读仓库根那份，留空让消费者显式声明 | 根体检不再因缺清单而红 |
| A5 | 根 `package.json` 删掉 `"prepare": "husky"`——**只在分支上删**，主线保留 | `no-install-hooks` 通过 |
| A6 | 新增根 `.gitattributes`（`* text=auto eol=lf` + 二进制白名单） | `gitattributes-lf` 通过，行尾判据跨机器一致 |
| A7 | 补推荐项：根与切片都写 `engines`；切片写 `"type": "module"` 与 `files` | `esm-first` / `engines` / `publish-scope` 通过 |
| A8 | CI 新增 job：标签推送时构建切片并**提交产物**，然后打标签（标签格式见约定；**先用自检器量一次**预发布后缀能否过语义化标签判据） | 标签指向含产物的提交；再推一个提交后标签**一动不动** |
| A9 | 交付给消费方：地址 / `#path=` / 标签 / "自包含"说明 | 消费方能只写一条依赖就装上 |

> **商业化相关性**：两个应用的企业版会放在**私有仓**里，并用 ngm 消费同一批 MIT 切片
> （ngm 不读不存 token、环境透传、输出脱敏）。这条分支同时服务公开版与企业版——**内核只有一份**。

### 3.1 可直接复制的两个清单（A1 / A3 的产物）

切片自己的 `packages/common/packages/<name>/ngm.json`（`name` 用**包名**，不要加版本后缀）：

```json
{
  "schemaVersion": 1,
  "name": "@lytjs/common-zip",
  "version": "7.0.0",
  "runtime": "node",
  "main": "./ngm-dist/index.mjs",
  "types": "./ngm-dist/index.d.ts"
}
```

仓库根的 `ngm.json`（`dependencies` **留空**，让消费者显式声明）：

```json
{
  "schemaVersion": 1,
  "name": "lytjs",
  "version": "7.0.0",
  "runtime": "node"
}
```

**一个会毁掉"自包含"的坑（实测）**：现有 tsup 配置里写着
`external: ['@lytjs/common-constants', '@lytjs/common-security']`——**这个 `external` 会阻止内联**。
切片构建必须显式覆盖它（`external: []`，或改用 `noExternal: [/^@lytjs\//]`），
否则 `ngm-dist/index.mjs` 里仍会留下 `@lytjs/` 的裸导入，而消费方会以"模块找不到"收场。
验收就是看板第 1 条（`@lytjs/` **0 命中**）。

## 4. B · 首批三个新包（zip / xml / dsv）

| # | 动作（具体文件） | 判据 |
|---|-----------------|------|
| B1 | 按 `packages/_templates/` 生成骨架：`packages/common/packages/{zip,xml,dsv}/{package.json,tsup.config.ts,tsconfig.json,src/index.ts,tests/,README.md,LICENSE}` | `pnpm build` 后三个包各自产出 `index.mjs` / `index.cjs` / `index.d.ts` |
| B2 | `scripts/build-order.ts` 的 `BUILD_ORDER` 加入三个包（L0 段、其依赖之前） | `pnpm build` 不报顺序错误 |
| B3 | **先修** `scripts/check-deps.ts` 的 `LAYER_MAP`（删掉已不存在的包），再登记新包层级 | 层级检查不报"新包没有归属" |
| B4 | `PUBLISH-MANIFEST.md` 加三行；`.size-limit.json` 加三条预算 | 发布清单与体积门禁通过 |
| B5 | 移植四步：IIFE → ESM + TS；宿主耦合改能力探测（`atob` / `DecompressionStream` / `Blob`，缺失时报明确错误）；补类型与 JSDoc；**保留原实现的中文注释** | `pnpm type-check` 零错误；`pnpm check-zero-deps` 通过 |
| B6 | **对照网**：`tests/reference/` 放两仓原实现（只读参考，注明来源与许可证）；`tests/parity.test.ts` 用同一批语料跑两边、逐条比结论（CRC32 用标准向量 `0xCBF43926`；XML / ZIP / DSV 用两仓的边界样例） | 对照全绿；**任何一处不一致都必须先解决，不许登记成"等价"** |
| B7 | 断言迁移：plotpin `scripts/selftest.mjs` 的第 2/3/4/5/6 小节、qforge `scripts/xml-lite.test.mjs` 的 37 条，转成 vitest | 用例数不少于原小节 |
| B8 | 每个包带 `LICENSE`（MIT），`files` 里包含它 | 自检的 `license` 项通过；vendor 时 LICENSE 随包进入消费方 |

## 5. C · 二批两个新包（ooxml / pdf）

| # | 动作 | 判据 |
|---|------|------|
| C1 | 建 `@lytjs/common-ooxml`：**合并** plotpin 的 xlsx（读写）与 qforge 的 office-writer（xlsx/docx 写）+ xlsx-importer（多表读）+ docx-importer（docx 读）。合并前**删掉 plotpin 读取路径里的应用域硬编码**（写死的表头关键词） | 两个应用的真实样例文件往返一致 |
| C2 | 定公开 API 并写进包 README：`readXlsx(bytes)` 返回多表、`writeXlsx(sheets)`、`readDocx(bytes)` 返回段落 + 图片字节、`writeDocx(blocks)`。**图片只以字节返回，不绑定任何存储**（IndexedDB 归应用） | 类型与 JSDoc 齐备；应用侧不再各自适配 |
| C3 | 建 `@lytjs/common-pdf`（plotpin 的 44 行图像 PDF 生成） | 多页 PDF 结构断言通过 |
| C4 | 三个新包加进 A1 的切片清单，按 A8 打新标签 | 与 A9 同一套验收 |

## 6. D · 许可、来源与商业化前提

| # | 动作（具体文件） | 判据 |
|---|-----------------|------|
| D1 | 每个新包：`"license": "MIT"` + 包内 `LICENSE`（`files` 别把它排除） | 自检的 `license` 项通过 |
| D2 | **来源追溯**：每个新包的 README 写清"实现来自 `idcu/qforge` 或 `idcu/plotpin`（MIT），由谁移植" | 来源、许可、移植者三者齐备——商业审计与将来重新许可都要靠它 |
| D3 | **许可边界**：① 保持 `dependencies` 零第三方（现状）；② 新增**许可黑名单门禁**：禁止 GPL/AGPL，**同时检查 `dependencies` 与 `devDependencies`**；③ **不要**把"零第三方"扩到 `devDependencies`（tsup / typescript / vitest 会当天全红） | 故意加一个 GPL 依赖 ⇒ 门禁红；现有 76 个包全绿 |
| D4 | **贡献者条款**：新增/更新 `CONTRIBUTING.md`，引入 DCO（提交签名）或 CLA | `CONTRIBUTING.md` 存在且被 README 引用；DCO 在 CI 里可查 |
| D5 | **README 写清 MIT 的商业含义**：允许商业使用与闭源集成；**MIT 不传染**——商业版应用可以闭源 | README 有专门一段（商业化后客户与合作方最常问的一条） |

**不做会怎样**（按失效模式排序）：

| 不做的项 | 最坏会发生什么 |
|---|---|
| A 适配分支 | "可被消费的形态"长期不存在：两个应用只能继续复制粘贴，或在各自仓库里维护一份 lytjs 副本 |
| B 首批三包 | 两个应用各留一份 ZIP / XML / DSV 实现，**行为会漂移**——直到某天同一批测试在两边给出不同结论 |
| C 二批 | 多表读、docx 图片提取只能留在应用里，很快会出现第三份实现 |
| D 许可与来源 | 商业审计时说不清"这段代码哪来的、谁的版权"，MIT 链条无法自证 |

## 7. 不做的事

- **不在主线上删 `prepare`、不在主线上提交 `dist/`**——主线那条 `entry-file-exists` 红是
  "主线不是消费形态"的正确读数，不是缺陷。
- **不为新包引入第三方运行时依赖**；`devDependencies` 里加 tsup/vitest 可以。
- **不把新包放在零依赖门禁扫描范围之外**（`packages/common/packages/*` 是安全的；
  若另建嵌套分组 `kit/packages/*`，要同步扩 `scan-zero-deps.ts` 的 `SCAN_TARGETS`）。
- **不在框架包里放任何商业逻辑**：商业能力属于两个应用的 `enterprise/**`——这条边界也是许可边界。
- 不在对照网全绿之前把旧实现从两个应用里删掉。

## 8. 验收看板（逐行勾）

| # | 判据 | 命令 | 期望 |
|---|------|------|------|
| 1 | 切片自包含 | `Select-String -Path packages\**\ngm-dist\index.mjs -Pattern '@lytjs/'` | **0 命中** |
| 2 | lytjs 全门禁 | `pnpm build` / `pnpm test` / `pnpm type-check` / `pnpm check-zero-deps` / `pnpm gate` | 全绿，且与改造前**同口径** |
| 3 | 对照网 | `pnpm vitest run packages/common/packages/zip/tests/parity.test.ts` | 全绿（不一致的项必须已解决，不许登记等价） |
| 4 | 切片可被消费 | 在临时项目里 `ngm add …#path=…@<标签> --ref-type=tag` → `ngm install` → `ngm verify` | `✓ match`；`ngm.mappings.json` 的 `path` 与 `#path=` 一致 |
| 5 | 裸导入可构建 | `ngm build src/index.ts --outfile=dist/app.js` | 产物里有切片实现；`--dry-run` 的 argv 里有 `--alias:` |
| 6 | 许可门禁有牙齿 | 故意加一个声明 GPL 的依赖 | 门禁红；删掉后恢复绿 |
| 7 | 主线未受影响 | 切回主线跑第 2 条 | 与改造前一致 |

## 9. 交接：谁给谁什么

| 方向 | 内容 |
|------|------|
| **我提供给 qforge / plotpin** | A9 的切片信息（地址 / `#path=` / 标签 / 自包含说明）——公开版与企业版共用 |
| **我需要 qforge / plotpin 提供** | ① 各自模块的边界样例；② 自测的断言清单与**基线数字**；③ 真实文件夹具（xlsx / docx / qbk / dxf / pdf，许可允许范围内） |
| **需要 lytjs 自己决策** | ① 适配分支的名字与语义；② 切片粒度（core / ui / ssr / router 各一个？）；③ 二线模块（几何 / DXF / PDF 读 / FSRS）收进框架仓，还是另立一仓 |
