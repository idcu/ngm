# JS Git 库的标准（供自家库文件统一）

> 这一页回答一个问题：**一个准备被当作 Git 依赖 vendored 进来的 JS/TS 库，
> 仓库里必须有什么、最好有什么、绝对不能有什么。**
>
> 判据全部可跑：`scripts/check-library-standard.sh`（Windows 入口 `.ps1` 转调同一实现）。
> **必做**项不满足 ⇒ 红 + 退出 1；**推荐**项只是提醒（除非 `--strict`）。

---

## 0. 这份标准的三条事实源（不是凭印象写的）

| # | 事实（代码里可核对） | 出处 |
|---|---------------------|------|
| 1 | 入口推断是**三层**：依赖自己的 `ngm.json`（`main`/`types`）→ `package.json`（`exports` → `main`/`types`）→ `./index.ts|js` 约定。三层都没有 ⇒ 该依赖降级为"一个目录"，构建工具找不到入口 | `internal/mappings/generate.go` 的 `InferEntry` |
| 2 | 传递依赖**只有一个来源**：上游仓库根目录的 `ngm.json`。上游把 Git 依赖写在 `package.json` 里，ngm **不纳入**、只提示 | `internal/resolve/graph.go`：`UpstreamFile = "ngm.json"`，`checkUpstreamPackageJSON` 只警告 |
| 3 | 依赖的安装钩子**默认不执行**（`postInstallPolicy` 默认 `deny`；`allow` 时只在沙箱内执行 JS 钩子） | `internal/supplychain/policy.go` · [ADR-009](../adr/adr-009-supply-chain-policy.md) · [ADR-012](../adr/adr-012-sandbox.md) |

**诚实边界**：本页的"生态现状"部分（ESM 优先、`exports` 条件导出等）来自
Node/npm 生态的既定事实，而**本机离线，没有联网核对最新工具链的行为**；
凡属这一类的条目都标了 🌐。凡属"ngm 会因此报错"的条目，都标了 🔒，
并且能在代码里找到唯一的报错位置。

---

## 1. 一张总表

### 必做（不满足 ⇒ 自检红 ⇒ 退出 1）

| # | 要求 | 为什么（不是"最佳实践"，是失效模式） |
|---|------|--------------------------------|
| 1 | 根目录有合法的 `package.json`，且含 `name` 与 `version`（`x.y.z`） | 没有它，入口推断只剩"index 约定"，mappings 会退化成一个目录 🔒 |
| 2 | **入口可解析**：`ngm.json` 的 `main`/`types`，或 `package.json` 的 `exports`/`main`，或根 `index.ts|js` | 与 `InferEntry` 三层同口径 🔒 |
| 3 | 若入口指向构建产物（如 `dist/`），**产物必须被提交**（不在 `.gitignore` 里） | ngm **不替依赖构建**（钩子默认 deny），指向一个不存在的文件等于没有入口 🔒 |
| 4 | **没有**安装期钩子：`preinstall` / `install` / `postinstall` / `prepare` | 它们在 ngm 下默认不执行；"靠钩子把库变得可用"的库，vendored 之后是坏的 🔒 |
| 5 | **没有** `.npmrc` | ngm 全仓库零处读取它；而它正是 PackageGate 那条"覆盖 git 二进制路径"的载体（[竞品对比 §4.2](../COMPETITIVE-ANALYSIS.md)）🔒 |
| 6 | **没有** Git LFS 指针 | ngm 对指针求哈希没有意义，直接拒绝（ADR-008）🔒 |
| 7 | **没有**子模块（`.gitmodules` / gitlink） | gitlink 无法被忠实地重放，放行等于让 verify 对着假内容树通过 🔒 |
| 8 | symlink 的目标**不逃逸**包目录 | 逃逸的 symlink 在读内容时会被拒绝，verify 报 critical（v0.66）🔒 |
| 9 | 文本文件统一 **LF**（`.gitattributes` 声明 + 实际字节） | digest 对**原始字节**求哈希，CRLF 会被如实计入；混用会让同一份代码在不同机器上算出不同 digest 🔒。自检看的是**工作树字节**：Windows 上请保持 LF 检出（`core.autocrlf=false`，或让 `.gitattributes` 生效），否则同一份提交在两台机器上会给出不同结论 |
| 10 | 有 `LICENSE` 文件或 `package.json` 的 `license` 字段 | 没有许可证的 vendored 代码在法律上不可分发（合规场景的第一个红灯） |
| 11 | 若提供 `ngm.json`，它必须**能过 ngm 自己的校验**（`schemaVersion` / `name` / `version` / `runtime`，`runtime ∈ {node, deno}`） | 同一个文件既是"上游入口声明"又可能被当项目清单用；一个自身不合法（或缺 `runtime`）的清单，会让 `ngm config validate` 在那个仓库里直接报错 🔒 |

| 12 | monorepo 的 `workspaces` 里，每个目录都要有自己的 `package.json` | 子包是**独立的包**：没有清单的目录既不会被发布，也不会被 ngm 当子路径消费（自检会点名它，而不是跳过） |

### 推荐（自检会提醒）

| # | 建议 | 理由 |
|---|------|------|
| 1 | `"type": "module"`（ESM 优先）**或显式写明的 `"type": "commonjs"`** + `exports` 条件导出（`types` / `import` / `require`）🌐 | ESM 是现时的默认；但规范要的是"模块形态**别靠猜**"——写明的 CJS 选择同样合格（见 §6 的形态覆盖）。`exports` 让入口显式，不用靠猜 |
| 2 | 提供类型：`types` 字段或随包的 `.d.ts` 🌐 | TS 消费者否则只能靠 `index.d.ts` 约定 |
| 3 | `files` 或 `.npmignore` 声明发布范围 🌐 | 不声明会把测试、CI 配置一起带进消费者的 vendor 目录 |
| 4 | 根目录 `README.md` | 唯一会被人读的说明 |
| 5 | `engines` 声明 Node 版本 🌐 | 让消费者的运行期约束可见 |
| 6 | semver tag（`v1.2.3`）与不可变的 tag | ngm 的 `tag` 型依赖要求 tag 存在；漂移 tag 会让 `verify` 天天红 |
| 7 | 有传递 Git 依赖时，写在**根目录 `ngm.json`** 里（`dependencies` 带 `refType` + `ref`，子路径用 `path`） | 这是 ngm 唯一会递归的来源；写在 `package.json` 里只会得到一条提示 🔒 |
| 8 | monorepo 的子包各自带 `ngm.json`/`package.json` | 子路径（`subPath`）被当作**同一个仓库的不同切片**，各自要能自证入口 |

### 禁止

- 依赖安装期钩子（见必做 4）
- 把 Git 依赖写进 `package.json` 并指望它被装（见推荐 7）
- LFS 指针、子模块、逃逸 symlink（见必做 6/7/8）
- 提交会随构建变化的文件（时间戳、构建号、机器相关路径）——它们让 digest 不稳定，
  而 ngm 的全部价值建立在"同一 commit ⇒ 同一 digest"上
- 以 drift 的方式移动 tag（同一个 tag 指向不同 commit）

---

## 2. 最小模板

```
my-lib/
├── ngm.json          # 可选但推荐：传递 Git 依赖 + main/types（最高优先级入口）
├── package.json      # 必做：name / version / type / exports / license
├── LICENSE
├── README.md
├── .gitattributes    # * text=auto eol=lf —— 让 LF 成为事实，不只是约定
├── src/
│   └── index.ts
└── dist/             # 若入口指向这里，就必须提交（不在 .gitignore 里）
    ├── index.js
    └── index.d.ts
```

`package.json`（ESM 优先）🌐：

```json
{
  "name": "@me/my-lib",
  "version": "1.2.3",
  "type": "module",
  "license": "MIT",
  "exports": {
    ".": {
      "types": "./dist/index.d.ts",
      "import": "./dist/index.js"
    }
  },
  "files": ["dist", "src"],
  "engines": { "node": ">=20" }
}
```

`ngm.json`（只在有传递 Git 依赖、或想覆盖入口推断时需要）：

```json
{
  "schemaVersion": 1,
  "name": "my-lib",
  "version": "1.2.3",
  "runtime": "node",
  "main": "./dist/index.js",
  "types": "./dist/index.d.ts",
  "dependencies": [
    { "name": "github:me/base", "ref": "v0.4.1", "refType": "tag" }
  ]
}
```

> `name` / `version` / `runtime` 是 ngm 清单的**必填**三件（`runtime ∈ {node, deno}`，见
> `internal/config/validate.go`）。同一个文件既是"上游入口声明"、又可能被当项目清单用，
> 因此它必须**能过 ngm 自己的校验**——这是自检里的一条（`ngm-json-valid`）。

> `ngm.json` 的 `main`/`types` **压制** `package.json`（第 1 层）。两者不一致时，
> 入口以 `ngm.json` 为准——这是刻意的：它让"Git 依赖形态下的入口"有一个明确的说话处。

---

## 3. 版本与 tag

- 版本走 semver，`version` 与 tag 一致（`1.2.3` ↔ `v1.2.3`）。
- tag **不可变**：一旦被别人锁定，移动它就会让 `ngm verify` 判为漂移（三态之一）。
- ngm 的 `refType` 必填：`tag` / `branch` / `commit`。给自家库选 `tag`：它是唯一
  "人类可读 + 不可变"的组合；`branch` 会漂，`commit` 需要人工同步。

## 4. 确定性纪律（digest 的地基）

| 项 | 要求 |
|----|------|
| 行尾 | 统一 LF（`.gitattributes`：`* text=auto eol=lf`）；digest 计算的是**原始字节** |
| 文件 mode | 可执行位会被如实计入清单；Windows 上不可表示——别让"是否可执行"成为构建的前提 |
| 生成物 | 不提交随构建变化的内容（时间戳、构建号、绝对路径） |
| 大文件 | 不用 LFS（指针不是内容） |
| 子仓库 | 不用 submodule（gitlink 无法重放） |

## 5. 自检（唯一判据）

```bash
scripts/check-library-standard.sh /path/to/my-lib          # 必做项不满足 ⇒ 退出 1
scripts/check-library-standard.sh /path/to/my-lib --strict # 推荐项也计入退出码
scripts/check-library-standard.sh /path/to/my-lib --json   # 机器读（同一套事实）
```

Windows：

```powershell
.\scripts\check-library-standard.ps1 -Dir C:\src\my-lib
```

判据共 **25 条**：12 条必做常驻 + 4 条条件触发（入口指向构建产物时才检查产物是否已提交；
提供 `ngm.json` 时才检查它是否合法；monorepo 模式下才检查每个 workspace 目录；
`packages` 里有读不出的写法时才报 `workspace-manifest-unreadable`）+ 9 条推荐。
单次运行打印多少行，取决于条件项是否适用。

**"不适用"是第三种结论**：不是 git 仓库时（ngm 的 vendored 副本、导出的 tarball），
tag 检查显示为 `—` 并写明原因，**不计入任何计数**——不适用必须被打印出来，
静默跳过才是这里真正的敌人。ngm 的 vendored 输出本身就能过这份自检（v0.76 实测）。

**牙齿**：把 `package.json` 的 `version` 删掉、加一个 `postinstall` 脚本、
把 `ngm.json` 的 `runtime` 删掉，或让入口指向被 `.gitignore` 排除的 `dist/`
⇒ 自检立刻红并点名那一条。它不是"建议清单"，是判据。

### 5.1 形态覆盖（自检器实测过的六种形态）

标准只有在**别的形态**上跑过才算成立。自检器已在六种形态上验证，并因此改掉了四处误伤：

| 形态 | 判据行为 |
|------|---------|
| 单包 · ESM · 免构建（仓库即产物） | 全绿；`dist-committed` 不触发（入口不在产物目录） |
| 单包 · **显式 CJS** · 有构建步骤（产物已提交） | 全绿。**显式写明的 `"type": "commonjs"` 记为合格**——规范要的是"别让模块形态靠猜"，不是"必须 ESM"（v0.77 修掉的误伤：此前它被判"未声明 type=module"，而判据自己的措辞就是"CJS 需显式说明"） |
| monorepo · **npm**（根 `workspaces`） | **根不是包**：入口 / 产物 / 模块形态 / exports / 类型 / 发布范围 / `engines` 一律 `—`；每个子包**用同一条判据**逐个体检，结论带路径前缀（`packages/a/…`）。私有根（`private: true`）可以没有 `version` |
| monorepo · **pnpm**（根 `pnpm-workspace.yaml`） | 同上；**根可以没有 `package.json`**（v0.78 修掉的误伤：此前"没有根清单"是必做红，而 pnpm 形态里它是合法的） |
| monorepo · **lerna**（根 `lerna.json`） | 同上；信号来自 `lerna.json` 的 `packages`（根有清单但没有 npm `workspaces`） |
| **vendored 副本 / 子路径**（ngm 的 `ngm.vendor/`、导出的 tarball、`subPath` 消费到的子包） | 没有 `.git` ⇒ **两条**记 `—`：`semver-tag`（看不到 tag）与 `gitattributes-lf`（看不到仓库级声明——它可能在仓库根，而 vendored 的只有子目录）；内容侧的 LF 仍由 `lf-only` 守（那条看文件本身）。其余判据照常 |

子包清单的三种信号（npm `workspaces` · pnpm `pnpm-workspace.yaml` 的 `packages:` ·
lerna `lerna.json` 的 `packages`）合并且去重；写法上只读**简单形态**
（YAML 列表 / 内联字符串数组 / JSON 数组），并支持 `dir/*` 与花括号 `{a,b}/*` 两种 glob。
**读不出来的写法不许猜**：`packages` 里出现 YAML 映射或内联对象
⇒ `workspace-manifest-unreadable` **必做失败**——静默少体检一个子包，比红一次坏得多。

三条与 monorepo 有关的"仓库级"事实：`.gitattributes`（LF 声明）与 `.gitignore`
（决定"产物是否被忽略"）**看仓库根**——子包不必自带，也不该被要求自带；
子包的**入口与类型**可以从 `exports` 里读（见下）。
**嵌套 monorepo 明确不做**：那是一个尚未被验证的形态，静默地"反正也查了"比明确不做更坏。

### 5.2 自检器与产品必须读同一份事实

自检器**比产品严**就是误判：一个库会为"标准推荐它做的事"而红。
v0.78 对齐的一处：**入口推断读 `exports`**。

| 入口层次 | 产品（`internal/mappings.InferEntry`） | 自检器 |
|---------|--------------------------------------|--------|
| ① 依赖自己的 `ngm.json` | `main` / `types` | 同 |
| ② `package.json` | **`exports`**（字符串 / `"."` 字符串 / 条件映射，`types` → `import` / `module` / `default` / `require` 先到先得）→ 再 `main` / `types` 字段 | 同（v0.78 起逐字对齐） |
| ③ index 约定 | `index.ts` / `index.js` / `index.mjs` / `index.cjs`（`index.d.ts`/`d.mts`/`d.cts` 作类型） | 同 |

### 5.3 扫你 **vendored 的产物**（`scripts/check-vendored-standard.sh`）

标准的前一半是"库应该长什么样"，后一半是"**你拿到的依赖长什么样**"。
后者由一条命令回答（v0.79；v0.76 的候选，两版落空后补上）：

```bash
scripts/check-vendored-standard.sh <project-dir> [--strict] [--json]
# Windows: powershell -NoProfile -File scripts\check-vendored-standard.ps1 -Dir C:\src\my-app
```

它做的事只有一件：对 `ngm.mappings.json` 里的**每一个依赖**，用 §5 那支自检器
（**同一份实现**）体检一遍，然后把结论按依赖名归属回去。

三条纪律写在这里，因为它们是这条命令的全部价值：

| # | 纪律 | 为什么 |
|---|------|--------|
| 1 | 事实源是 **`ngm.mappings.json`**，不是"遍历 `ngm.vendor/`" | mappings 是"我 vendor 了什么"的权威（ngm 生成、可校验）。遍历目录会把 prune 残留、手工塞进去的东西也算成依赖——**同一份产物，结论能飘** |
| 2 | 结论必须**归属**回依赖名（`--json` 的 id 前缀 = `from`） | 否则"谁不合格"要人去数；而"数"正是项目里出过错的地方 |
| 3 | 退出码 **2** 表示"没有查过"（没有 mappings / 用法不对） | **"一个依赖都没查"与"全查过且都合格"必须能分开**——否则这条命令会在最需要它的项目上安静地成功 |

它扫的对象与 §5 的自检器**条数一致**（有判据对账，防止包装器少跑判据）；
映射指向一个不存在的目录时，报 `vendored-dir-missing` 的**必做失败**，
而不是跳过——跳过会让"删掉一个依赖的目录"看起来像"它合格"。

## 6. 例外

标准之外的例外必须**写下来**（在哪一页、为什么），而不是默默不遵守：
在本文件的"例外"小节追加一行（仓库 + 条款 + 理由 + 何时恢复）。

（暂无例外。）

---

## 相关文档

- [依赖管理](./dependency-management.md)（refType / subPath / 漂移三态 · mappings 与入口推断的使用场景）
- [ADR-008 archiveDigest 的定义](../adr/adr-008-archive-digest.md)（为什么 LFS/CRLF 是硬约束）
- [ADR-009 供应链策略](../adr/adr-009-supply-chain-policy.md)（钩子默认 deny）
- [评估报告 2026-10-09 §4.2 PackageGate](../internals/assessment-2026-10-09.md)（`.npmrc` 那条路径）
