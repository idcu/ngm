# ngm 全维度评估与推进方向（2026-11-04）

**评估基线**：`main@de7ce82`（工作树干净，与 `origin/main` 同步）；Windows + Node `v24.21.0` + npm `12.0.2` + Git `2.56.0`；ngm 由本仓源码构建，`ngm --version` 输出 `ngm 0.0.0-dev (git:dev, built: unknown)`。
本文所有“实测”都来自本轮真实运行（命令与复现步骤见 §8）；凡属推断或外部行为，均显式标注。

---

## 0. 结论

**要接管，但只接管“任务编排”，不接管“安装器”。**

1. **建议新增 `ngm run <script>`（P1，MVP 7–10 人日；P1 全部约 11–16 人日）**：读脚本、注入 `node_modules/.bin` 与 `ngm.vendor/.bin` 到 PATH、`--` 参数透传、子进程退出码/信号透传、`pre/post` 约定、`--if-present`、`--list`、`--ignore-scripts`。同时把引擎解析（`ngm build` 等）也改为先看项目本地 `.bin`。
2. **痛点直答**：`ngm run` 能让“**跑脚本**”不再依赖 npm，但**不能让典型 JS/TS 项目“删掉 npm”**。只要依赖或工具链（esbuild / tsc / postcss / eslint / jest）来自 npm registry，安装环节仍然需要 npm 或 pnpm。能彻底移除 npm 的只有一类项目：**依赖全部是 Git 依赖，且引擎走非 npm 渠道**（独立二进制、Deno、系统级安装）。
3. **不要做 npm 替代品**：registry 安装与 semver 求解、peer dependency 求解、`publish` / dist-tag / registry 鉴权、`npm_config_*` 全量模拟，都不该做。成本 60–120 人日，且会把“可证明的 Git 依赖层”这一定位稀释成“一个功能不全的 npm”。
4. **当前真正的 P0 不是 run，而是“装得上、跑得起来”**：103 个 tag 下 release 资产覆盖不全，`installation.md` 里的 npm 分发仍是“规划中”，`ngm-repo` 固定的 `0.102.0-dev.1` 没有对应可下载产物（README §已知问题 1）。这直接决定“装完 ngm”这个前提能否成立。

一句话决策：**把 `ngm run` 当作 P1 的“最后一段路”补上；把 npm 重新定义为“工具链与 registry 的供给方”，而不是“任务运行器”。**

---

## 1. 事实基线：ngm 现在与“脚本/任务执行”的关系

### 1.1 命令面：27 个命令，没有 `run` / `exec`

命令表来自 `cmd/ngm/root.go` 的 `commands`（本轮 `ngm --help` 实跑核对）：

```
init  adopt  add  install  update  remove  modules  doctor  verify  audit
why   tree   outdated  typecheck  typedecl  build  transform  css
mappings  integrations  cache  store  gc  config  engines  sbom  attest
```

其中名字看起来像“跑东西”的，实际都是**固定能力维度**，不是任意命令执行：

| 命令 | 实际行为 | 是否接受任意命令 |
|---|---|---|
| `build` | 用 catalog 里的 bundle 引擎（默认 esbuild）产出 bundle | 否，只拼固定 argv |
| `transform` | 用 transform 引擎转单文件 | 否 |
| `typecheck` / `typedecl` | 用 `tsc`（或自定义引擎） | 否 |
| `css` | 用 `postcss` | 否 |
| `integrations` | 生成外部构建工具配置（vite/webpack/esbuild/deno 的 `ngm.*` 文件） | 否，只写配置文件 |
| `audit --hook` / `verify --sandbox` | 在 Deno 沙箱里跑依赖自带的 `verify.js` | 否，固定文件名 |

实测（E1）：

```text
$ ngm run build --dir <proj>
unknown command: run
EXIT=3
```

### 1.2 `ngm.json` 里没有放脚本的位置

`ProjectFile`（[config.go](../../internal/config/config.go#L87-L103)）只有 11 个字段：

```go
SchemaVersion, Name, Version, Runtime, Main, Types, Publish,
Dependencies, Engines, Vendor, SupplyChain
```

`json.Unmarshal` 使用了未知字段拒绝。实测（E2）在 `ngm.json` 里加 `"scripts"`：

```text
ConfigInvalid: validate ngm.json
  cause: parse ...\ngm.json: json: unknown field "scripts"
  hint:  fix the reported field or remove unknown fields
EXIT=3
```

也就是说：**今天既没有 `scripts` 的存放位置，也没有 `run` 的入口**。

### 1.3 `engines` 是怎么被调用的

内置 catalog（[catalog.go](../../internal/adapter/catalog.go#L177-L226)）：

| 引擎名 | 能力 | 程序 / 固定参数 | 是否内置 |
|---|---|---|---|
| esbuild | bundle、transform | `esbuild` | 是（非 Optional） |
| typescript | typeCheck | `tsc` | 是（Optional） |
| typescript | typeDecl | `tsc --emitDeclarationOnly` | 是（Optional） |
| postcss | css | `postcss` | 是（Optional） |
| self | 所有能力 | 无产物 stub | 是 |
| deno | 需在 `ngm.engines.json` 显式声明 | `deno` | 否（刻意不入内置，避免抢 tsc） |

调用方式是 **subprocess + `exec.LookPath(程序名)`，完全不经过 shell**（[catalog.go](../../internal/adapter/catalog.go#L103-L119)）。这意味着：

- 程序只在**进程 PATH** 上查找；
- **不会**插入 `node_modules/.bin`；
- argv 由 ngm 自己拼，`ngm.engines.json` 的 `command` 也不做管道/重定向/变量展开。

每个引擎调用都要过 `run:<程序名>` 权限。实测（E7）：

```text
ConfigInvalid: permission denied: run:esbuild
  hint: add "run:esbuild" to `permissions.allow` in ~/.ngm/config.json;
        paste {"permissions":{"allow":["run:esbuild"]}}
EXIT=3
```

实测（E8/E9）：项目里有 `node_modules/.bin/esbuild.cmd`，但 PATH 上没有 esbuild 时：

```text
EngineNotFound: bundle: esbuild: engine `esbuild` is not available (`esbuild` was not found on PATH)
  hint: install one of the listed engines, or declare your own in ngm.engines.json
EXIT=5
```

`node_modules/.bin` 里的 shim **完全没有被 ngm 看见**。这是本轮最关键的证据之一。

### 1.4 生命周期钩子：只有 postinstall，而且只跑 JS

[postinstall.go](../../cmd/ngm/postinstall.go#L19-L35) 的约定：

- 只认依赖根目录的 **`postinstall.js`**；
- `package.json` 的 `scripts.postinstall` 会被**检测到并打印**，但**从不执行**（理由写在代码里：派生 shell 会绕过 Deno 沙箱）；
- 默认 `supplyChain.postInstallPolicy=deny`；`allow` 时也只在 **Deno 沙箱**里跑：可读该依赖自己的子树，`net`/`env` 需显式授权，**不派生进程、不写任何文件**；
- **没有** `preinstall` / `prepare` / `prepublish` / `postprepare`，也没有“项目自己的 pre/post”。

所以“生命周期钩子”在 ngm 里是**依赖安装期的一个安全例外**，不是任务系统的一部分。

### 1.5 `adopt` 从 `package-lock.json` 吃了哪些字段

`adopt` 只解析 npm `package-lock.json`（v2/v3 的 `packages` 映射）。实测（E12）用一份含 Git 来源与 registry 来源的锁文件：

```json
{
  "version": 1,
  "tool": {"name": "ngm", "version": "0.0.0-dev"},
  "source": {...},
  "coverage": {...},
  "packages": [
    {"name": "reg", "version": "", "path": "node_modules/@scope/reg", "files": 1, "bytes": 17,
     "provenance": {"kind": "registry",
                    "resolved": "https://registry.npmjs.org/@scope/reg/-/reg-2.0.0.tgz",
                    "integrity": "sha512-AAA"}},
    {"name": "fromgit", "version": "0.1.0", "path": "node_modules/fromgit", "files": 2, "bytes": 76,
     "provenance": {"kind": "git", "url": "https://github.com/x/fromgit.git",
                    "commit": "1111111111111111111111111111111111111111"}}
  ],
  "notes": [...]
}
```

消费的字段：`packages[*].version`、`resolved`、`integrity`；`resolved` 里的 `git+…@<sha>` 被拆成 `url` + `commit`。

不消费：`dependencies` 树（v1 形态）、`engines`、`peerDependencies`、`optionalDependencies`、`bin`、`hasInstallScript`。
文件扫描会跳过 `node_modules/.bin` 与符号链接（计数里也不含它们）。因此 `adopt` 的证明**不覆盖 shim，也不覆盖“该包声称自己有 bin”**。

一个低危观察：当包目录缺少 `package.json` 时，名字回退到路径最后一段，`node_modules/@scope/reg` 会显示成 `reg`（丢 scope）。这是 [adopt.go](../../internal/adopt/adopt.go#L376-L384) 的 `readPackageName(dir, filepath.Base(dir))` 造成的。是否算缺陷取决于“无 package.json 的包”是否算合法输入；本轮只登记，不判定。

### 1.6 `node_modules/.bin` 是谁生成的

- **npm / pnpm / yarn 生成**（npm 用 `cmd-shim` 写三份：无扩展名、`.cmd`、`.ps1`）。
- **ngm 从不写 `node_modules`**：全仓 `exec.Command` 的生产站点只有 git、deno、引擎程序；没有任何代码创建 `node_modules` 或 `.bin`。
- ngm 也**不能直接调用** `.bin` 里的东西，因为它不做 PATH 注入（E9）。

这意味着：**只要项目依赖 npm 装的工具，今天的 `ngm build` 就必须要求这些工具在全局 PATH 上**；项目本地装的 esbuild 对 ngm 不可见。

### 1.7 无 npm 时的真实可用性矩阵（E11）

环境：`PATH` 只放 `node.exe` + `ngm.exe` + `Git\cmd`（**npm 不在 PATH**），项目用 ngm 从本地 mirror 安装 1 个 Git 依赖。

| 操作 | 结果 | 退出码 | 说明 |
|---|---|---|---|
| `ngm install --frozen-lockfile --offline` | 成功 | 0 | 输出 `all 1 served from the content store (no git access)` |
| `ngm verify` | 成功 | 0 | 93 ms（暖缓存） |
| `ngm verify --content` | 成功 | 0 | 115 ms；`content: 2 file(s), 0 symlink(s) checked` |
| `ngm tree` | 成功 | 0 | 只读 lock |
| `ngm sbom` / `ngm attest` | 成功 | 0 | 只读 lock |
| `ngm doctor` | 成功 | 0 | 报告 git / project ok |
| `ngm gc` | 成功 | 0 | dry-run，无可回收 |
| `ngm run build` | **失败** | 3 | `unknown command: run` |
| `ngm build`（无 esbuild） | **失败** | 5 | `EngineNotFound`，提示装引擎 |
| `npm run build` | **失败** | 命令不存在 | PowerShell 抛 `CommandNotFoundException`；cmd 下为 9009 |

结论：**npm 缺席不会影响 ngm 的安装/验证/审计链路**；断点集中在“脚本入口”“引擎供给”“项目本地工具解析”三处。

### 1.8 无 npm 也无法掩盖的结构性依赖

ngm 自己的 CI 在 **用 npm 装引擎**（[.github/workflows/ci.yml:365](../../.github/workflows/ci.yml#L362-L365)）：

```yaml
- name: install real engines
  run: npm install --global esbuild typescript postcss-cli postcss
```

这不算 ngm 的运行时依赖，但它说明一个事实：**ngm 的“构建”能力依赖外部引擎，而这些引擎的主流分发渠道就是 npm**。`installation.md` 也没有把“如何在不使用 npm 的情况下获得 esbuild/tsc/postcss”写成一条路径。

---

## 2. 核心判断：脚本执行该不该 ngm 自己实现

### 2.1 应该自己实现（编排层，小而完整）

| 能力 | 理由 | 成本（估算） | 风险 | 不做会怎样 |
|---|---|---|---|---|
| `ngm run <script>` | 这是“两个工具并存”痛点的唯一直接答案；脚本是项目自己的代码，执行意图由用户显式表达 | 3–4 人日（含 `--list`、错误提示） | 与“安装期不执行任意代码”的承诺需要区分清楚；必须写 ADR | 用户永远需要第二个 runner（npm/pnpm/make） |
| 脚本来源：`package.json.scripts` 为权威，`ngm.json.scripts` 为回退 | 单一事实源，避免同一脚本两份定义漂移；`package.json` 不是 npm 专属文件 | 1 人日（含 schema 变更与配置契约网登记） | 两处同名必须报错（exit 3），不能静默 last-wins | 若只放 `ngm.json`，迁移期会要求用户把脚本抄一遍，必然漂移 |
| PATH 注入 `node_modules/.bin` + `ngm.vendor/.bin` | 这是 npm 脚本能跑 `esbuild`/`vite`/`jest` 的根本机制；也是 `ngm build` 用项目本地引擎的前提 | 0.5–1 人日 | Windows 需要 `PATHEXT` 正确解析 `.cmd`；不能污染子进程以外环境 | 项目本地装的工具对 ngm 永久不可见（E9 已实测） |
| `--` 参数透传 | `ngm run test -- --watch` 是日常刚需 | 0.5 人日 | 参数原样传递，不做二次解析；`pre/post` 不收参数（与 npm 对齐） | 用户只能写死在脚本里，失去灵活性 |
| 子进程退出码透传 | CI 依赖它；npm 的语义是“子进程码即 npm 码” | 0.5 人日 | 需要为“任意码透传”单独设计退出码契约，不能套 0–6 单码表 | CI 会把失败读成成功，或反之 |
| 信号转发（SIGINT/SIGTERM → 进程组，返回 128+signal） | `Ctrl-C` 必须停掉整棵进程树，否则留下孤儿 node | 1–1.5 人日 | Windows 无 POSIX 信号；需复用沙箱的进程组终止路径并单独测 | 开发服务器/长任务会留下孤儿进程，端口占用 |
| `pre`/`post` 后缀约定、`--if-present`、`--ignore-scripts` | 与 npm 行为对齐，迁移期两边结果一致 | 1 人日 | `pre` 失败必须中止主脚本；`--ignore-scripts` 只跳过 pre/post，不跳过显式脚本 | 迁移期行为不一致，用户不敢切 |
| 最小 `npm_*` 环境变量（见 §4.1） | 大量脚本读 `npm_lifecycle_event` / `npm_package_version` | 0.5–1 人日 | 不能承诺 `npm_config_*` 全量；文档要写明差异 | 部分脚本能跑但行为不对，排查成本高 |
| 引擎解析也走 `.bin`（`ngm build` 优先项目本地 esbuild） | 让“npm 只装一次工具链，之后用 ngm 跑”成立 | 0.5–1 人日 | 与权限模型叠加：仍要 `run:esbuild` 放行 | 用户即使本地装了 esbuild，仍必须再装一份全局的 |
| 为 vendored Git 依赖生成 `.bin` shim（P2） | Git 依赖若声明 `bin`，没有 shim 就无法作为命令使用 | 2–3 人日 | 跨平台 shim（`.cmd`/`.ps1`/无扩展名）、权限位、与 npm 的 `.bin` 冲突 | `ngm.vendor` 里的 CLI 只能写全路径调用；纯 Git 项目无法完全脱离 npm |
| 串行编排（`ngm run a b`） | 低成本、低歧义 | 1 人日 | 语义要定死：串行、遇错即停（P2） | 用户自己写 `&&`，跨平台又回到 shell 差异 |
| 并行编排 | 只在确实需要时做 | 2–3 人日 | 输出交错、失败聚合、资源争抢（P3） | 大型 monorepo 会觉得慢；但不影响单项目 |

### 2.2 可以并存，但不必自己实现

| 能力 | 定位 | 理由 | 成本/风险 | 不做会怎样 |
|---|---|---|---|---|
| `npm run` 本身 | 并存 | 需要 `npm_config_*`、npm 内部变量、或 `npx` 的脚本，继续用 npm 最省事 | 0 | 少数脚本仍需 npm；文档明说即可 |
| `npm audit` 与 `ngm audit` | 并存 | ngm 已用 OSV；不复制 registry 私有漏洞库 | 0 | 与 npm 的 advisory 结果可能有差异（推断，未联网比对） |
| workspaces（`npm -w`） | 并存 | ngm 的 `modules` 是“模块集合/发布索引”，不是 npm workspace 语义 | 4–6 人日（若要做） | monorepo 的批量脚本仍归 pnpm/npm；不影响单包项目 |
| `npm pack` / `npm publish` | 并存 | 发布到 registry 是 npm 的核心领域，ngm 没有 registry 客户端 | 15–25 人日（若要做） | 库发布继续用 npm；ngm 只负责来源与证明 |
| `npx` 临时执行远端包 | 并存 | 需要 registry 拉取与缓存语义 | 大 | 需要“临时试一个包”时仍用 npx |
| `adopt` 支持 pnpm-lock / yarn.lock | ngm 可做，但不是 npm 脚本问题 | 降低迁移门槛；只读解析，不改安装 | 5–8 人日 | 非 npm 用户无法用 adopt 出证明（P2） |
| 通过 npm 分发 ngm 二进制 | 并存（已规划） | `installation.md:128` 已写“npm（规划中，仅分发二进制）” | 1–2 人日（打包与发布） | 用户少一个安装渠道；与本报告结论不冲突 |

### 2.3 明确不该实现

| 能力 | 为什么不做 | 成本（若做） | 风险 | 不做会怎样 |
|---|---|---|---|---|
| registry 包安装 / semver 求解 / tarball 校验 | 这是另一个产品：需要 registry 协议、镜像、鉴权、依赖求解、缓存与完整性体系；与“Git 依赖溯源”定位冲突 | 60–120 人日 | 永远追不上 npm/pnpm，且把安全模型从“Git commit 可证明”变成“registry 元数据信任” | 使用 registry 依赖的项目继续用 npm；这是设计边界，不是缺陷 |
| peer dependency 求解 | npm 自身语义复杂且有历史包袱；ngm 无 registry 图 | 20+ 人日 | 求解结果与 npm 不一致会制造“看起来能装、跑不起来” | 无影响（ngm 不装 registry 包） |
| `publish` / dist-tag / registry 鉴权 | 发布是 npm 的强项；ngm 的 `publish` 字段只服务模块索引 | 15–25 人日 | 凭证、2FA、org 权限、registry 兼容性 | 发布继续用 npm；ngm 不碰凭证面 |
| `npm_config_*` 全量模拟 | 这些是 npm 的内部实现细节，不是稳定契约 | 3–5 人日（也只能部分） | 模拟得越像，越容易被误认为“npm 兼容层” | 个别脚本失败；文档列出最小集即可 |
| 依赖 lifecycle 全族（`preinstall`/`prepare`/`prepack`…） | 安装期任意代码执行是供应链攻击的主要入口；ngm 已经用“只跑沙箱内 JS”刻意收窄 | 5–10 人日 | 直接破坏 ADR-009 的安全承诺 | 少数“从 Git 装、安装时构建”的包需要用户手工构建（已知限制） |
| 自动执行依赖的 `scripts.postinstall` | 同上；派生 shell 会绕过 Deno 权限（代码里已有实测结论） | 0 | 一次 install 就可能 RCE | 安全承诺不变；需要构建的依赖由用户显式处理 |
| 自研 shell / 自研引号语义 | 平台 shell 的差异是无穷的；npm 也是交给 `cmd`/`sh` | — | 自研 = 制造第三套语义 | 直接用平台 shell，差异与 npm 一致，用户已有认知 |

---

## 3. 迁移路径（三阶段）

### 阶段 0：并存（现状，0 人日）

今天就能做的分工：

- 依赖安装：Git 依赖用 `ngm install`；registry 依赖用 `npm install`。
- 完整性：`ngm verify --content`（依赖树）+ `ngm adopt --check`（npm 装的树）。
- 任务：全部 `npm run ...`。

**验收判据**

```bash
ngm install --frozen-lockfile --offline   # exit 0（暖缓存）
ngm verify --content                      # exit 0
ngm adopt --check=ngm.proof.json          # exit 0
npm run build                             # exit 0，产物与今天一致
```

**用户可见变化**：无。

### 阶段 1：ngm 接管脚本（P1，7–10 人日）

交付：

1. `ngm run <script>`（`package.json.scripts` 权威，`ngm.json.scripts` 回退，同名报错）；
2. PATH 注入 + 参数透传 + 退出码/信号；
3. `pre`/`post`、`--if-present`、`--ignore-scripts`、`--list`；
4. 最小 `npm_*` 环境变量；
5. 引擎解析先看 `.bin`；
6. `run:script` 单一权限闸门（默认拒绝，首次运行给可粘贴提示）；
7. ADR-042 + 帮助/文档/契约网登记。

**验收判据**（必须同时过 Windows 与 Linux）

```bash
ngm run build                       # exit 0；产物 sha256 与 npm run build 相同
ngm run test -- --watch             # 参数到达子进程
ngm run build                       # pre/post 顺序可见；pre 失败 ⇒ 主脚本不跑
ngm run missing                     # exit 3，列出可用脚本
ngm run missing --if-present        # exit 0
# Ctrl-C：exit 130，且 `ps` 无孤儿 node
ngm run build                       # 项目本地 node_modules/.bin/esbuild 可被找到
```

**用户可见变化**：日常从 `npm run build` 改为 `ngm run build`；npm 只在“装/升级工具链”时出现。

### 阶段 2：npm 只用于发布，或彻底移除（按项目类型分叉）

- **纯 Git 依赖 + 非 npm 引擎**：可以移除 `node_modules`、`package-lock.json`，甚至 `package.json`（脚本迁到 `ngm.json`）。验收：在**没有 npm 的 PATH** 下跑完 build/test/verify（本报告 E11 已证明安装/验证链路可行，缺口只剩脚本与引擎）。
- **使用 registry 依赖或 npm 工具链**：npm 保留，但退化为“安装器 + 发布器”。验收：CI 的 build/test job 不再调用 `npm run`，只有 install/publish job 调用 npm。

**用户可见变化**：CI 里 `npm run` 消失；`npm` 出现次数从“每个 job”降到“安装/发布 job”。

> 说明：阶段 2 的“彻底移除”不是普遍可达状态。`tsc`、`postcss`、`esbuild` 的官方分发以 npm 为主；没有 npm 时用户必须自行获得独立二进制或系统包。这一点必须写进文档，否则会制造“装上 ngm 就什么都不用装”的错误期待。

---

## 4. 正面回答痛点：能不能“装完 ngm 就能跑 `ngm run build` 且不用 npm”

**分项目类型回答：**

| 项目类型 | 能否完全不用 npm | 缺什么 |
|---|---|---|
| 依赖全是 Git 依赖；引擎是独立二进制/系统包；脚本只用 node 与本地工具 | **能** | `ngm run` + `.bin` 注入 + `run:script` 权限 + 引擎可得 |
| 依赖全是 Git 依赖；但用 esbuild/tsc/postcss（npm 分发） | **不能完全**（脚本可以，工具链不行） | 引擎供给路径；除非改用独立二进制或 vendored 工具 |
| 有 registry 依赖 | **不能** | registry 安装能力（明确不做） |

**具体缺件清单（按今天实测）：**

1. `ngm run` 不存在（E1，exit 3）。
2. `ngm.json` 拒绝 `scripts` 字段（E2，exit 3）；`package.json` 只被 `adopt` / postinstall 读取。
3. 没有 PATH 注入（E9：`.bin/esbuild.cmd` 存在仍 `EngineNotFound`，exit 5）。
4. 引擎必须全局可达；`ngm build` 不认项目本地 `.bin`（同上）。
5. 权限模型是**逐程序** `run:esbuild`；接管脚本后不能要求用户为每个工具逐条放行，需要一个 `run:script` 闸门（否则不可用；完全无闸门又会破坏“默认拒绝”的承诺）。
6. 跨平台 shell 未实现：npm 在 POSIX 用 `sh -c`，Windows 用 `cmd /d /s /c`；ngm 今天完全不经过 shell（这是安全选择，但 `run` 必须经过 shell）。
7. `npm_*` 环境变量、pre/post、`--if-present` 均无。
8. 退出码契约：`ngm run` 透传子进程码，不能照抄仓库现有的 0–6 声明表；需要单独决定并在契约网里登记。

### 4.1 关键语义建议（拍板项）

| 议题 | 建议 | 理由 |
|---|---|---|
| shell 选择 | POSIX：`$SHELL`（默认 `sh`）+ `-c`；Windows：`cmd.exe /d /s /c` | 与 npm 对齐，用户已有认知；不引入第三套语义 |
| 引号/转义 | 不做二次处理，整串交给平台 shell | 任何“智能处理”都会和平台行为分叉 |
| `pre`/`post` | 默认执行；`--ignore-scripts` 全部跳过 | npm 兼容；显式 `ngm run prebuild` 仍可单独跑 |
| `--if-present` | 脚本不存在时 exit 0 且静默 | CI 常用 |
| `--` 参数 | 只附加到主脚本；pre/post 不收 | 与 npm 一致 |
| 信号 | 转发到进程组；SIGINT ⇒ 130 | CI 与交互都要可预期 |
| 环境变量 | 最小集：`INIT_CWD`、`npm_lifecycle_event`、`npm_lifecycle_script`、`npm_package_name`、`npm_package_version`；**不设** `npm_execpath` | 设成 ngm 会让工具误把 ngm 当 npm 执行；`npm_config_*` 明确不承诺 |
| 权限 | 单一 `run:script`（默认拒绝，首次给可粘贴 JSON）；`--dry-run` 打印完整 argv 与注入的 PATH | 保持“显式放行”的安全模型，同时不把可用性打死 |
| 沙箱 | **不沙箱**项目脚本，文档明说 | 沙箱需要完整读写/网络，脚本本来就要做这些；假装沙箱比不沙箱更危险 |
| 脚本来源 | `package.json` 优先；`ngm.json` 回退；同名 ⇒ exit 3 | 单一事实源，禁止静默覆盖 |

---

## 5. 其他维度

### 5.1 功能矩阵与缺口

**现状**：27 个命令，安装/验证/审计/证明链路完整；Git 依赖、monorepo 子路径（module）、GC、SBOM/attest 都有。缺口集中在“执行面”和“registry 面”。

**建议**：
- P1：`ngm run`（唯一新增功能，避免同时开多个战线）。
- P2：`ngm gc` 覆盖 module cache；`adopt` 支持 pnpm/yarn。
- P3：workspace 级批量脚本（与 `modules` 语义对齐后再做）。

**依据**：命令表与本轮实测；`ngm gc` 默认 dry-run/`--apply`（README §已知限制 4）；`adopt` 只读 npm lock（README:558）。

### 5.2 质量与测试覆盖

**现状**：287 个 `*_test.go`、842 个 `Test*/Benchmark*` 函数；最近一次全量 `go test -count=1 -timeout 40m ./...` 在 `cmd/ngm` 上 564.337s 全绿。仓库有独特的“契约网”：配置矩阵（27 命令 × 配置错误）、文档示例可执行、JSON 形状正反向、退出码声明实测、用户可见错误必须有 hint、help golden 与文档命令表对齐。

**还没 E2E 的关键路径**：

1. `verify --content` 的 **module 子路径**：代码支持（`replayModuleDigestFromEntries` 等），但黑盒测试只有根依赖（`v102_content_verify_test.go` 只用 `m5`）。
2. `gc` 的**删除失败**：只有 `gcExitCode` 单元判据，没有跨平台可稳定构造真实删除失败的 E2E。
3. `gc` 与并发 `install` 的 1 分钟 blob 安全窗：仅代码注释，无竞态测试。
4. 真实网络 fixture：CI 有 gated job，但默认全量测试不触网（这是优点，也意味着网络路径覆盖依赖 CI job）。
5. `ngm run`：不存在，因此跨平台 shell/信号/引号**完全没有测试**——这是新增功能最大的质量风险点。

**建议**：P1 的 `ngm run` 必须在 Windows + Linux 双平台跑同一组 E2E（参数透传、pre/post、130、孤儿进程、`.bin` 解析）；P2 补 1、2、3。

**依据**：测试文件/函数计数（本轮 `Select-String` 实测）；`v102_content_verify_test.go`；`v102_gc_test.go`；上一轮全量结果。

### 5.3 文档与实现一致性

**现状**：305 个 md、2,494 KiB、42 个 ADR；`TestV15`（文档示例可执行）、help golden、`installation.md` 命令表比对在守。总体上“文档即判据”的纪律很强，但体量已经超过 31,402 行生产代码的文档密度。

**本轮实测发现的不一致（详见 §7 缺陷清单）**：
- `README:166` 与 `README:459` 说 `NGM_HOME` 可覆盖 `~/.ngm`（包括 `config.json`），`env.go:129` 的注释也说“设置过 NGM_HOME 的人看到的是另一个文件”；实际 `config.GlobalPath` 永远基于 `os.UserHomeDir()`。实测把配置放在 `NGM_HOME/config.json` 与 `NGM_HOME/.ngm/config.json` 都不会被读取，权限仍然被拒。
- `README:250/549` 的 “101 个 tag” 已落后（实测 103 个 tag）。

**建议**：P1 修正 D1（实现 `NGM_HOME` 读取或改文档/注释二选一）；P2 把硬编码 tag 数改成读数脚本（仓库自己已在 `milestones-b.md` 写过“硬编码计数是下一个新增的地雷”）。

**依据**：`config.go:251-258`、`env.go:127-137`、`env.go:579-586`、README 上述行号。

### 5.4 发布与分发

**现状**：103 个 tag，最新 `v0.102.0-dev.2`；README 承认 GitHub/Gitee release 资产覆盖不全，`ngm-repo` 的 `.ngm-version` 固定 `0.102.0-dev.1` 但没有可下载产物，只能本地构建。分发渠道：预编译二进制、源码编译、包管理器（Homebrew/Scoop/npm 均“规划中”）。`action.yml` 是 composite action，不自行下载 ngm。

**建议**：
- **P0**：把“每个发布版本都有可校验产物”变成机械判据（`scripts/check-release-status.sh --drift-guard` 已有基础）；至少让 `0.102.x` 有一个真实可下载 release。
- P1：提供一条真正的一键安装路径（install script 或 npm 二进制包）。
- P2：Gitee 附件补齐（已有幂等脚本）。

**依据**：README:250/549-554；`installation.md:119-130`；`action.yml`；`git tag` 实测 103。

### 5.5 性能

**已知线性成本点**：

| 路径 | 实测/依据 | 性质 |
|---|---|---|
| `ContentStore.Has` | 20 文件 929 µs/op；200 文件 8.25 ms/op；2000 文件 77.1 ms/op（37 KiB / 316 KiB / 3.01 MiB allocs） | 随文件数线性；**只留 benchmark，不加缓存**（清单 mtime 不随 blob 删除变化，没有可靠失效信号） |
| `verify --content` | 2 文件 115 ms（暖缓存，含进程启动）；按字节哈希 | O(总字节)，只建议用于发布门/夜跑 |
| `install`（暖 mirror/暖 store） | 1 依赖 295 ms；`--frozen-lockfile --offline` 直接命中 store（`all 1 served from the content store`） | 每依赖若干次 git 子进程（tag 型约 4 次，`metrics.md`）；离线时退化为读 store |
| 文档声称的规模 | 100 依赖离线 2.167 s / 在线 2.556 s（`docs/internals/metrics.md`） | 在线部分受网络支配 |
| mirror | 整仓库 bare mirror（非 shallow）；module 走 `blob:none` partial clone（ADR-034） | 大仓库首次 clone 成本高；`ngm gc` 尚未覆盖 module cache（P2-5） |

**建议**：P2 只评估“落地（copy/hardlink）是否可并行”——解析阶段已有并发（`resolve/graph.go`、`tree.go` 用了 `WaitGroup`）；`Has` 不加缓存；`verify --content` 不设为默认。

**依据**：`content_has_bench_test.go` 注释与本轮 benchmark；E3/E5/E6 计时；`metrics.md`。

### 5.6 安全边界

**现状**：默认拒绝 `net:` 与 `run:`；`run:git` 是唯一放行点；postinstall 默认 deny，allow 也只在 Deno 沙箱；沙箱外的 shell 钩子**检测但不执行**。这套模型的代价是“第一次用引擎/第一次联网都要配置”，收益是“安装不执行任意代码”。

**新增 `ngm run` 的影响**：它必须有意识地**打开一个沙箱外的任意代码执行面**（脚本本来就要读写文件、联网、起服务）。建议：
- 单一 `run:script` 权限（默认拒绝），首次运行给可粘贴 JSON；
- 文档明说“项目脚本不在沙箱内”；
- 绝不把 `ngm run` 接到 `install` / `verify` 的自动流程里；
- `--dry-run` 打印完整 argv、注入的 PATH 与工作目录；
- 不新增对依赖 lifecycle 的执行。

**优先级**：P1（与 `ngm run` 同批交付，不能事后补）。

**依据**：`permissions.go` 默认策略；`postinstall.go:19-35`；`docs/adr/adr-009`。

### 5.7 产品定位是否需要重述

**现状**：README 的定位是“Git-first 依赖溯源层”，边界明确（不支持 registry 包、workspace、submodule、LFS）。`ngm run` 会把产品从“安装/证明层”扩展到“任务执行层”，如果不重述，会同时得罪两类用户：想要 npm 替代的人会失望，想要最小证明层的人会担心膨胀。

**建议**：P1 重述为三层，并固定措辞：

> ngm = **Git 依赖的安装与内容证明** + **可选的项目脚本执行（仅项目自己声明的脚本）**。
> 它不是 npm 替代品：不装 registry 包、不求解 peer、不发布、不执行依赖 lifecycle。

**依据**：README:545-566 的边界；`installation.md` 的前置要求；本报告 §2.3。

---

## 6. 优先级清单

成本均为**量级估算**（依据本仓同类改动：`ngm gc` 约 420 行 + 4 处契约网登记 + E2E，实际约 2–3 人日）。

| 优先级 | 事项 | 成本 | 依据（为什么现在做） |
|---|---|---|---|
| **P0-1** | 发布产物与 tag 对齐：至少让一个 `0.102.x` 有可下载、可校验的 release；把 drift-guard 接入发布前 | 3–5 人日（多为流程/脚本） | README:549-554 自认；`ngm-repo` 没有对应产物；这是“装完 ngm”的前提 |
| **P0-2** | 明确“引擎从哪来”：文档写清无 npm 时如何获得 esbuild/tsc/postcss（独立二进制/系统包），或提供 `ngm doctor` 可执行的检查 | 1–2 人日 | E8/E19：今天引擎缺失只给一句 hint；用户会以为 ngm 自带 |
| **P1-1** | `ngm run` MVP（脚本来源、PATH、参数、退出码、信号、pre/post、`--if-present`、`--list`） | 7–10 人日 | 本报告核心结论；E1/E2/E9 |
| **P1-2** | 引擎解析优先项目本地 `.bin` | 0.5–1 人日 | E9：本地 esbuild 对 ngm 不可见 |
| **P1-3** | `run:script` 权限闸门 + ADR-042 + 定位重述 | 1–2 人日 | §5.6；不能事后补安全模型 |
| **P1-4** | 双平台 E2E（Windows/Linux：参数、pre/post、130、孤儿进程、`.bin`） | 2–3 人日（含契约网登记） | §5.2 第 5 条：新功能最大风险 |
| **P1-5** | 修正 `NGM_HOME` 与 `config.json` 的口径（实现或改文档） | 0.5–1 人日 | §7 D1：安全配置指错文件 |
| **P2-1** | vendored Git 依赖的 `.bin` shim 生成 | 2–3 人日 | 纯 Git 项目脱离 npm 的最后一块 |
| **P2-2** | `verify --content` 的 module 子路径 E2E | 1 人日 | §5.2 第 1 条 |
| **P2-3** | `gc` 删除失败 E2E + 并发 install 安全窗测试 | 1–2 人日 | §5.2 第 2、3 条 |
| **P2-4** | `adopt` 支持 pnpm-lock / yarn.lock | 5–8 人日 | 迁移门槛；只读解析 |
| **P2-5** | `ngm gc` 覆盖 module cache | 1–2 人日 | 当前局部 GC 的已知空洞 |
| **P2-6** | 文档读数化（tag 数、命令数、测试数不再硬编码） | 1 人日 | README:250 已漂移（101 vs 103） |
| **P3-1** | 串行/并行编排（`ngm run a b`、`--parallel`） | 1 / 2–3 人日 | 单项目不紧迫；并行先量需求 |
| **P3-2** | workspace 级脚本编排 | 4–6 人日 | 需先与 `modules` 语义对齐 |
| **P3-3** | registry 安装 / peer 求解 / publish | 60–120 人日 | **不建议**（见 §2.3） |

---

## 7. 缺陷与观察清单（本轮实测发现，均未修改代码）

> **2026-11-04 追记（v0.120）**：D1 / D2 / D3 已修复，D4 由 `ngm run` 的 `.bin` 注入覆盖；
> 修复内容与判据见 [CHANGELOG](../../CHANGELOG.md) 的 Unreleased 段与
> [ADR-042](../adr/adr-042-run-outside-sandbox.md)。下文保留当时的实测原文，不改写历史读数。

| 编号 | 严重度 | 现象 | 证据 | 建议 |
|---|---|---|---|---|
| D1 | 中 | `NGM_HOME` 只改变 layout（mirror/content/cache/vendor），**不改变全局 `config.json` 的读取路径**；README 与代码注释都声称可改 | 实测：`NGM_HOME=<tmp>` 且把 `config.json` 放在 `<tmp>` 或 `<tmp>/.ngm` 均不生效，`run:esbuild` 仍被拒（exit 3）。代码：[config.go:251-258](../../internal/config/config.go#L251-L258)、[env.go:127-137](../../cmd/ngm/env.go#L127-L137)、[env.go:579-586](../../cmd/ngm/env.go#L579-L586)；文档 README:166/459 | 二选一：让 `homeDirOrEmpty()` 走 `NGM_HOME`，或把 README/注释改成“仅影响 layout”；不能继续两边都写 |
| D2 | 低 | README 的“101 个 tag”已落后（实测 103） | 标签计数实测 = 103；README:250/549 | 改成读数脚本或删掉具体数字 |
| D3 | 低（观察） | `adopt` 对缺少 `package.json` 的 scoped 包丢 scope：`node_modules/@scope/reg` 显示为 `reg`、`version` 为空 | E12 JSON；[adopt.go:376-384](../../internal/adopt/adopt.go#L376-L384) | 若“无 package.json 的包”算合法输入，回退时保留 scope；否则在 notes 明说 |
| D4 | 观察（非缺陷） | `ngm build` 不认项目本地 `node_modules/.bin`，只认全局 PATH | E9，exit 5 | 归入 P1-2 的功能补强，不是 bug |

---

## 8. 实测附录（可复现）

**环境**：Windows；Node `v24.21.0`；npm `12.0.2`；Git `2.56.0.windows.4`；ngm 由 `go build -o ngm.exe ./cmd/ngm` 构建（`0.0.0-dev`）。

**夹具**：

1. 本地 Git 上游 → `git clone --mirror` 到 `<home>/.ngm/mirror/github.com/assess/lib.git`；
2. 项目 `ngm.json` 声明 `github:assess/lib`，`refType=commit`，`ref=<sha>`；
3. 隔离 `USERPROFILE`，权限只放 `net:github.com` 与 `run:esbuild`。

**关键运行记录**：

```text
# E1
ngm run build --dir <proj>            → unknown command: run                    exit 3

# E2
ngm config validate --dir <proj>      → ConfigInvalid: validate ngm.json
                                        cause: json: unknown field "scripts"   exit 3

# E3 首次安装（暖 mirror，本地）
ngm install --dir <proj> --digest     → exit 0，295 ms，1 dependency；
                                        <proj>/ngm.vendor 2 files；无 node_modules

# E4 无 npm 的 PATH，冻结离线安装
PATH=<node.exe+ngm.exe+Git\cmd>
ngm install --frozen-lockfile --offline --dir <proj>
                                      → installed 1 dependencies ...
                                        all 1 served from the content store
                                        (no git access)；exit 0；store=835 B
# E5
ngm verify                            → exit 0，93 ms
ngm verify --content                  → exit 0，115 ms
                                        content: 2 file(s), 0 symlink(s) checked;
                                        digest sha256:e92bd
# E6
ngm build                             → exit 0，156 ms（bundle 写 stdout）

# E7 未放行 run:esbuild
ngm build                             → ConfigInvalid: permission denied: run:esbuild
                                        hint: add "run:esbuild" ...；exit 3

# E8/E9 PATH 无 esbuild（即使 node_modules/.bin/esbuild.cmd 存在）
ngm build                             → EngineNotFound: ... not found on PATH；exit 5

# E10 Node 解析
node -e "require('assess/lib')"       → MODULE_NOT_FOUND
node -e "require('./ngm.vendor/github.com/assess/lib')" → 42

# E11 无 npm 矩阵
install --frozen-lockfile --offline   0
verify                                0
verify --content                      0
tree                                  0
sbom                                  0
doctor                                0
gc                                    0
run build                             3
npm run build                         CommandNotFoundException

# E13 benchmark（internal/core/vendor）
BenchmarkContentStoreHas/files=20     929 µs/op   37 KiB   177 allocs
BenchmarkContentStoreHas/files=200    8.25 ms/op  316 KiB  1,568 allocs
BenchmarkContentStoreHas/files=2000   77.1 ms/op  3.01 MiB 16,072 allocs
```

**局限**：

- 计时是 Windows 上单次运行，含进程启动，不代表稳定分布；`install` 使用暖 mirror + 暖 store。
- “npm 的 shell/env/退出码 130 行为”属于 npm 公开行为与既有认知，**本轮未逐项复测**。
- “esbuild/tsc/postcss 主要经 npm 分发”是生态现状判断（**推断**）；可核实的事实是 ngm 自己的 CI 用 `npm install --global` 装它们（`ci.yml:365`）。
- 完整 `ngm` 命令矩阵中，真实网络路径（首次 clone、`ls-remote`、OSV 查询）未在本轮逐一实测；相关结论来自代码与既有测试。

---

## 9. 不建议做的事（及理由）

1. **把 ngm 做成 npm 的完整替代**：registry 安装 + semver + peer 求解是 60–120 人日级别的工程，且会把“Git commit 可证明”变成“registry 元数据信任”，定位与安全模型同时被摧毁。
2. **为兼容 npm 而执行依赖的 lifecycle 脚本**：安装期任意代码执行是供应链攻击的主要入口；ngm 已经用 Deno 沙箱把这条线收窄，不应为了兼容性重开。
3. **模拟 `npm_config_*` 全量**：这是 npm 的内部实现，不是契约；越像越容易被误认为兼容层。
4. **在没有需求证据前做并行编排 / workspace 脚本**：单项目场景收益低，输出交错与失败聚合会引入新的复杂度。
5. **把 `ngm run` 接入 install/verify 的自动流程**：那会把“用户显式执行”变成“隐藏执行”，直接违背默认拒绝的设计。
6. **自研 shell 或自研 shim 语义**：平台 shell 的差异无法穷尽；与 npm 对齐（`sh -c` / `cmd /d /s /c`）是成本最低、预期最一致的选择。
