# Spec：2026-11-18 全量评估后的开发批次

依据：`docs/analysis/ngm-architecture-and-npm-gap-2026-11-18.md`（发现、证据、问题清单、优先级）。
基线：HEAD `29f46ae`，工作树干净。本 spec 只定义下一轮**做什么、按什么顺序、怎么验收**；
逐项清单见 [tasks.md](./tasks.md)。禁止强推、改历史、改 git config。

## 0. 结论：本轮只做四类事，按 A→B→C→D 推进

1. **A 止血（P0）**：关掉"无全局配置时探测闸门不注册"这个执行面——克隆一个带
   `ngm.engines.json` 的仓库后，`engines list/validate` 当前会真实 spawn 它声明的程序。
2. **B 补信任根（P1）**：`--deep` 的 store 清单反向集合校验；git 子进程环境的
   `GIT_EXEC_PATH` / `LD_PRELOAD` 残留；`ExtraEnv` 绕过过滤。
3. **C 修对外契约（P1）**：`doctor --json` 失败信封；`exit-codes.json` 点错的测试名；
   `Version()` 直接调用绕闸门；两处 semver 实现。
4. **D 补 npm 迁移的最后几厘米（P2）**：adopt 读 pnpm/yarn lock、vendored 依赖的
   `.bin` shim、`gc` 覆盖 module cache。

**不新增 registry 能力、不动 `ngm run` 的既有语义、不改退出码 0–6 的含义。**
release 资产补齐需要外部凭据，单列 E，不占工程排期。

## 1. 不变量（任何批次都不得破坏）

- 退出码 0–6 的单码语义不变；`ngm run` 仍透传子进程码（信号 → 128+signal）。
- `run:<engine>` / `run:git` / `net:<host>` / `run:script` / `postInstallPolicy`
  五套闸门不合并、不互相放行。
- git 只有一个子进程构造点（`internal/core/git/spawn.go`）；引擎 spawn 只有
  `internal/adapter/subprocess.go` 的 `newProcessCommand`；HTTP 出口只在 OSV 一处。
- `--json` 时 stdout 恰好一份 JSON；人类输出与 JSON 同源、同码。
- 完整性失败(2) 不可被资源缺失(4) 或工具缺失(5) 覆盖；"没查成"不写成"没签名"。
- `internal/core` 不 import 语言/JS 层，由边界测试守住。

## 2. 批次与耦合

| 批次 | 内容 | 前置 | 提交粒度 | 工作量 |
|---|---|---|---|---|
| A | A1 闸门注册 + A2 零配置判据 + A3 直接 Version 调用守卫 | — | 1–2 提交 | M |
| B | B1 `--deep` 反向集合 + B2 buildEnv/ExtraEnv | A（同属安全主题，可并行开发、先合 A） | 每提交 1 项 | M |
| C | C1 doctor 信封 + C2 契约测试名 + C3 semver 统一 | — | 每提交 1 项 | S |
| D | D1 adopt lock + D2 `.bin` shim + D3 gc module cache | A/B/C 全合入后再开（先安全后功能） | 每提交 1 项 | M–L |
| E | 发布资产 / 一键安装（需凭据） | 无 | 人工动作 | — |
| F | 技术债：JSON encoder 统一、手写 flag 模板收编、死代码 | D | 按项 | L |

**耦合说明**：
- A3 的"无直接 `.Version()` 调用"守卫要与 A1 同一批合入：否则 A1 修完仍可能有
  新路径绕过 `Catalog.Probe`。
- B1 必须把"多余条目"判为 **critical(2)**，不能沿用 `replayDigestFromEntries`
  返回 error 时被 `checkLanding` 计入 `operational`(4) 的现有分支——否则等于把
  已确证的注入降级成"没查成"。
- D2 依赖 `runscript.BinDirs` 的注入机制（已存在）；不创建 `node_modules/.bin`，
  与 ADR-042 的分工保持一致（新增 ADR-043 或在 ADR-042 加一段）。
- A 与 C 无文件冲突，可并行；B2 与 C 无冲突；D 与 B1 无冲突。

## 3. 批准点与停在原地的条件

- 批准 A + B 即开工；C 可随 A/B 一起；D 的每项开工前再确认一次。
- **A 的兼容性风险**：修好后，没有全局配置的用户跑 `engines list` 会看到所有
  内置引擎变成 `not probed`（版本留空），而不是现在显示真实版本。若 CI 或
  下游 `ngm-repo` 依赖 `engines list` 的版本列，先确认再合入；不要为了兼容
  而保留无闸门的探测。
- **D1 的决策点**：pnpm/yarn lock 解析是否引入 YAML 依赖（`go.mod` 当前只有 2 个
  依赖）。倾向"不引入"：只读提取 Git 来源字段，做窄形状解析并在 notes 里
  如实标注未覆盖部分；若窄解析无法稳定覆盖主流 lock 形态，停下来拍板。
- **D2 的决策点**：shim 只服务 `ngm.vendor/.bin`，不写 `node_modules`；需要
  明确"vendored Git 依赖声明的 `bin` 生成跨平台 shim"是否已超出 ADR-042
  的承诺边界——若是，先补 ADR 再写码。

## 4. 回归门槛（每批合并前）

1. `go build ./... && go vet ./...`。
2. 测试：本机完整 `go test ./...` 的 `cmd/ngm` 串行约 40 分钟，用
   `go test -count=1 ./internal/...`（本机 87s 全绿）+ 受影响命令的定向测试；
   CI 上跑全量 `-timeout 40m`。本机跑不完不是跳过理由，要在 PR 里写清口径。
3. 新增判据必须在默认 `go test` 中执行，不得依赖手工 `-run`；禁止 `t.Skip`。
4. 涉及权限的改动补"拒绝时子进程零命中"断言；涉及 JSON 的改动补
   "stdout 恰好一个顶层对象"的字节级断言。
5. 涉及 `verify` 的改动同时跑 CI 口径：`go test -coverpkg=./internal/core/verify ./...`，
   跨包覆盖率不得低于 80%（当前 82.1%，余量 2.1pp，注意别把口径退回单包）。
6. 涉及 gofmt 的判定用干净 LF 导出（`git archive HEAD | tar -x`），
   不要采信本机 `core.autocrlf=true` 下的 `gofmt -l .`。

## 5. 明确不做（写进下一版复盘，避免反复重启）

| 不做 | 理由 | 重启条件 |
|---|---|---|
| registry 协议/鉴权/镜像、semver solver、peer deps、tarball integrity | 60–120 人日、永远追不上 npm/pnpm，且把信任根从 Git commit 换成 registry 元数据 | 产品定位重写为通用包管理器 |
| `publish` / dist-tag / registry 鉴权 | 发布是 npm 强项，ngm 的 `publish` 字段只服务模块索引 | 出现明确的发布场景 |
| lifecycle 全族、自动执行依赖 `scripts.postinstall` | 安装期任意代码执行是供应链攻击主入口，会直接破坏 ADR-009 | 不重启；需要构建的依赖由用户显式处理 |
| `npm_config_*` 全量、`npx` | npm 内部实现不是稳定契约；模拟得越像越会被当成兼容层 | 不重启 |
| 自研 shell / shim 引号语义 | 平台 shell 差异无穷，与 npm 对齐 `sh -c` / `cmd /d /s /c` 是成本最低路径 | 不重启 |
| `Has` 结果缓存 | 没有可靠的失效信号；缓存会把"检查"变成"猜"，绕过 D1-重做区分篡改/缺失的全部意义 | 出现可用的内容变更通知机制 |
| `remote` adapter | 按 ADR-013/020 决定排除（代码里不存在，非"待实现"） | ADR 里写的触发条件出现 |

## 6. 续批 G（2026-11-19）：关掉 S3 残项与最后一条未验收门槛

A–F 合入后仍有三条**可动**的残项，外加一条纪律缺口；G 只做这四条，其余（F1/F2 重构、
E 发布）留后：

| 项 | 内容 | 为什么现在做 |
|---|---|---|
| G1 | `ngm run` 的子进程环境遵守 `permissions.deny: ["env:X"]` | 这条策略当时有三个施加点（git / 引擎 / 沙箱），脚本是 ngm 启动的**最大**一类子进程却没被覆盖。它不是"文档没写"，而是同一条 deny 在一条真实执行路径上无声失效 |
| G2 | 模块条目"多一条 store 记录"的端到端判据 | B1 只对根条目做了反向集合，模块分支靠一句"会被 moduleDigest 重放抓住"的**注释**支撑。注释不是证据 |
| G3 | 跨包覆盖率按 CI 口径实测并记录 | 验收表里唯一一条 `[x/待]`，此前只有 2026-11-08 的历史读数 |
| G4 | README 回到 600 行以内（删掉重复的发布读数表） | `check-discipline.mjs` 第 11 问是 13 条里唯一的 ❌，而它守的那份内容在 `release-pipeline.md` 已有权威版本 |

**与 §1 不变量的关系（必须显式记一笔）**：G1 **改变了 `ngm run` 的既有环境语义**，
而 §0 写着"不动 `ngm run` 的既有语义"。裁决如下——

- 改的是**施加面**，不是契约变量：五个 lifecycle 变量与注入的 `PATH` 一字不改，
  新增的只有"父环境里被 deny 的名字不再出现"。
- 反方论据（已考虑并否决）：脚本在沙箱外，`deny: ["env:X"]` 挡不住脚本读
  `~/.ssh` 或 `~/.netrc`，所以这条 deny 在脚本侧是"剧场"。否决理由：剧场的前提是
  **两套规则**——同一条 deny 对 git 生效、对脚本不生效，读代码的人无法从配置推断
  行为；而保持一致的成本是一次 `envutil.Without`，收益是"施加点表可以说全"。
- 兼容性影响：拒绝过某个变量、又依赖它在脚本里存在的用户会看到差异。这类配置的
  语义本来就是"不要把它交给子进程"，CHANGELOG 按 **Changed** 记录，`ngm run --help`
  与安全模型的施加点表同步写明。

G2/G3 不改任何行为：一个是判据，一个是读数。

## 7. 续批 H（2026-11-19）：把"命令开头那一段"收成机械不变量

F2 原本记为"风险/收益不匹配"，本轮改判为**值得做**，理由是它已经失效过三次
（v0.54 一条 parse 信封、v0.105 doctor 漏 markJSON、v0.108 why 的位置参数分支、
v0.135 doctor 又写成第三种），而每次都是"某条命令没走骨架"——不是逻辑写错。

| 项 | 内容 | 判定 |
|---|---|---|
| H1 | 14 条命令 / 24 处手写骨架迁 `cmdContext` | 做：纯机械，失败面统一 |
| H2 | 拆出 `newCmdContextNoJSON` | 做：v0.16 规则④要求"不接受 `--json` 的命令 stdout 为空"，与"给信封"直接冲突，必须显式分叉 |
| H3 | 机械守卫 `TestCmdContextIsTheOnlySkeleton` | 做：迁移没有守卫就会退回原状（新命令照抄旧写法就能编译通过） |
| H4 | F1（JSON encoder 收敛） | **不做**：盘点后发现唯一实际差别是 HTML 转义，而那是对外形态问题；见 tasks.md 的决策记录 |

H1/H2 的唯一用户可观察变化：`modules` 的五个子命令在 `--json` 下失败（解析与运行期都算）
现在给错误信封，与 `verify` / `tree` / `install` 等早已如此的命令一致。

**G5–G8（同日追加，来自完整 `go test ./...`）**：首次在本机跑完整套件，抓出四处
"改了代码/文档，但没同步改判据或索引"的尾巴——ADR-043 没登索引、`gc --json` 的新字段
没进形状表、A1 让一条旧的版本比对用例失去前提、C1 让参数维度判据的首行断言失配。
四处的修法与"为什么不能靠定向测试"记在 [tasks.md](./tasks.md) 的 G5–G8 一节。

