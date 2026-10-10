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

### 推荐（自检会提醒）

| # | 建议 | 理由 |
|---|------|------|
| 1 | `"type": "module"` + `exports` 条件导出（`types` / `import` / `require`）🌐 | ESM 是现时的默认；`exports` 让入口**显式**，不用靠猜 |
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

判据共 **23 条**：12 条必做常驻 + 2 条条件触发（入口指向构建产物时才检查产物是否已提交；
提供 `ngm.json` 时才检查它是否合法）+ 9 条推荐。单次运行打印多少行，取决于条件项是否适用。

**"不适用"是第三种结论**：不是 git 仓库时（ngm 的 vendored 副本、导出的 tarball），
tag 检查显示为 `—` 并写明原因，**不计入任何计数**——不适用必须被打印出来，
静默跳过才是这里真正的敌人。ngm 的 vendored 输出本身就能过这份自检（v0.76 实测）。

**牙齿**：把 `package.json` 的 `version` 删掉、加一个 `postinstall` 脚本，
或把 `ngm.json` 的 `runtime` 删掉 ⇒ 自检立刻红并点名那一条。
它不是"建议清单"，是判据。

## 6. 例外

标准之外的例外必须**写下来**（在哪一页、为什么），而不是默默不遵守：
在本文件的"例外"小节追加一行（仓库 + 条款 + 理由 + 何时恢复）。

（暂无例外。）

---

## 相关文档

- [依赖管理](./dependency-management.md)（refType / subPath / 漂移三态）
- [依赖管理](./dependency-management.md)（mappings 与入口推断的使用场景）
- [ADR-008 archiveDigest 的定义](../adr/adr-008-archive-digest.md)（为什么 LFS/CRLF 是硬约束）
- [ADR-009 供应链策略](../adr/adr-009-supply-chain-policy.md)（钩子默认 deny）
- [评估报告 2026-10-09 §4.2 PackageGate](../internals/assessment-2026-10-09.md)（`.npmrc` 那条路径）
