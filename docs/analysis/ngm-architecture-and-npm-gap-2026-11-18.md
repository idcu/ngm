# ngm 全量代码梳理、架构设计与 npm 替代缺口评估（2026-11-18）

> **基线**：`git pull` → Already up to date；HEAD `29f46ae`（2026-11-08）。工作树干净。
> **方法**：只读。四个并行分域逐行阅读实现与关键测试（cmd/ngm、internal/core、支撑子系统、工程健康度），
> 加一次隔离目录黑盒复现；本机实跑 `go build ./...`、`go vet ./...`、`gofmt`（干净 LF 导出）。
> **本轮没有修改任何产品代码或文档**（除本报告本身）。
> **证据约定**：`文件:行` 指向 HEAD 实际行；黑盒结论给出可复现命令。严重度 S0 阻断 / S1 高 / S2 中 / S3 低。

---

## 0. 结论摘要

1. **架构是健康的**：`cmd/ngm → internal/* → internal/core` 单向依赖，没有反向引用；git 进程只有 `spawn.go` 一个构造点、引擎 spawn 只有 `newProcessCommand` 一个构造点、HTTP 出口只有 OSV 一处，且这三条都由机械测试守住。2026-11-07 评估列出的 S0/S1/S2 缺陷在随后的 42 个提交里**已逐项修复并有回归判据**（见 §4.4）。
2. **但本轮发现一个新的 S0**：**没有全局 `config.json` 时，引擎探测闸门完全不注册**，被克隆仓库声明的任意程序会在 `ngm engines list` / `engines validate` 这类"只读诊断"命令里被真实 spawn。已黑盒复现（§4.1 S0-1）。这是"修复 S0-1 时留下的半边"——闸门注册被放进了 `if env.Policy != nil` 块内。
3. **"替代 npm"要分三种项目回答**（§5.3）：
   - 依赖全是 Git 依赖 + 引擎是独立二进制/系统包 → **可以完全不用 npm**（`ngm run` 已于 v0.120 补齐，此前缺的脚本层已落地）；
   - 依赖全是 Git 依赖但用 esbuild/tsc/postcss（主要经 npm 分发）→ **脚本可以，工具链不行**；
   - 有任何 registry 依赖 → **不能**，这是设计边界，不是待办。
4. **缺口的本质不是"少几个命令"，而是"依赖模型不同"**：ngm 的信任根是 Git commit + 内容 digest，npm 的信任根是 registry 版本 + tarball integrity。registry、semver 求解、peer deps、`node_modules` 布局、lifecycle 脚本、publish 六座大山任何一座都不是补丁级工作；把它们搬进来会摧毁"Git 依赖可证明层"的定位（60–120 人日量级，且永远追不上）。
5. **工程纪律是这个项目最扎实的资产**：933 个 Test、3 个 Fuzz、退出码/JSON 单文档/help golden/命令骨架/覆盖率门槛都有机械判据；`verify` 跨包覆盖率按 CI 口径 82.1%。代价是文档量（317 个 md / 2.73 MB）已远超代码量（实现 36.1k 行）。

---

## 1. 范围、方法与证据基线

### 1.1 实测读数

| 项 | 读数 |
|---|---|
| 命令 | `git pull` → Already up to date；HEAD `29f46ae` |
| Go 文件 | 456（实现 143 / 测试 313） |
| Go 行数 | 实现 **36,120** / 测试 **54,543** |
| 测试函数 | 932 Test + 1 `TestMain` / 3 Benchmark / 3 Fuzz / 56 处跳过调用（41 `t.Skip` + 15 `t.Skipf`） |
| Markdown | 报告口径 317 个 / 2,734,042 字节（含 `.workbuddy` 与 CHANGELOG 则为 322 / 2,785,472） |
| 顶层命令 | 28（`cmd/ngm/root.go:38-67`） |
| 外部 Go 依赖 | 2：`wazero v1.9.0`、`gitee.com/idcu-go/modproof v0.2.0` |
| `go build ./...` | 通过 |
| `go vet ./...` | 通过 |
| `gofmt -l .` | 在干净 LF 导出中 **0 个文件**；工作树里 26 个文件是本地 `core.autocrlf=true` 把 LF blob 检出成 CRLF 的假象，仓库 blob 全 LF（已逐字节核对），CI 不受影响 |
| 覆盖率 | `verify` 跨包口径 82.1%（CI 门槛 80%，见 `docs/specs/.../tasks.md:52-62`）；本轮定向子集实测 60.6%，非同一口径 |
| 测试实跑 | `go test -count=1 ./internal/...` 全绿（87s）；完整 `go test ./...` 本机未在预算内跑完（`cmd/ngm` 串行约 40 分钟，CI 显式 `-timeout 40m`） |

### 1.2 阅读覆盖

- **逐实现文件读**：`cmd/ngm` 全部命令入口；`internal/core/{resolve,git,vendor,verify,lock,digest,atomicfile,errs,boundbuf,envutil}`；`internal/{adapter,config,security,supplychain,sbom,attest,adopt,observability,runscript,mappings,integrations}` 的实现主体。
- **抽查测试**：v1xx 回归判据、golden/help、JSON 契约、enforcement 登记表、fuzz 目标、skip 审计。
- **黑盒**：隔离目录 + 自建探针程序，复现"无配置时探测闸门缺席"（§4.1）。
- **未逐行**：`.ps1`/`.sh` 脚本实现细节、远端 release 资产、真实 Deno 跨版本矩阵；这些不影响本报告的架构与缺口结论。

---

## 2. 架构设计

### 2.1 一句话

```
ngm.json 声明(refType)
  → 图解析(root wins / 传递冲突) → ref/commit 解析
  → 内容 digest 锁定(archiveDigest / moduleDigest+subtreeOid)
  → 四层 vendor 落地(mirror / module-cache / content-store / ngm.vendor)
  → verify 四查(ref / binding / digest 重放 / landing) + 漂移三态
  → OSV/白名单门禁 → 可复现构建 / SBOM / in-toto 产物
  → ngm run 执行项目脚本(独立于上述信任链)
```

### 2.2 分层与依赖方向

```
┌─ cmd/ngm ─────────────────────────────────────────────────┐
│ 28 命令 · root.go 唯一命令表 · cmdContext 骨架 · JSON 信封 │
│   │  (仅 config → resolve 一处允许跨层：--infer-ref-type)   │
└──────────────────────┬──────────────────────────────────────┘
                       ▼
┌─ 产品子系统 ───────────────────────────────────────────────┐
│ adapter(引擎)  security(权限/沙箱)  supplychain(OSV)        │
│ sbom/attest    adopt            observability   runscript  │
│ config         mappings         integrations              │
└──────────────────────┬──────────────────────────────────────┘
                       ▼
┌─ internal/core（语言无关、无 JS 运行时）──────────────────┐
│ resolve → git      lock      digest      vendor   verify   │
│ atomicfile  errs   boundbuf  envutil                        │
└──────────────────────────────────────────────────────────────┘
```

- **没有 `internal/* → cmd/ngm` 反向依赖**；`internal/core` 不得 import JS 语言层，由 `TestCoreBoundaryForbidsLanguageSpecificImports` 机械守住（`docs/architecture/core-and-adapters.md`）。
- **进程/网络出口收束**：git 唯一构造点 `git/spawn.go:25-44` 的 `newGitCommand`（先 `CheckRun("git")` 再 spawn，`spawn_gate_test.go:16-51` 守"无第二处"）；引擎唯一构造点 `internal/adapter/subprocess.go:462-467` 的 `newProcessCommand`；HTTP 出口在 `supplychain/osv.go`。
- **cmdContext**（`cmd/ngm/cmd_context.go`）统一了 12+ 命令的"flagset → parse/failEarly → markJSON → newProjectEnv → runErr → 报告序列化"骨架；13 个命令已接入，其余（init/add/build/run/modules 等）因 `--` 拆分或自有错误通道保留手写模板。

### 2.3 核心信任链（逐环节实现）

| 环节 | 实现 | 关键不变量 |
|---|---|---|
| 声明 | `config.ProjectFile`（`internal/config/config.go:87-109`）schema 写 1 读 1..2，`DisallowUnknownFields` | 未知字段即 exit 3，不静默吞 |
| 图解析 | `resolve.ResolveGraph`（`graph.go:246-375`）BFS 分层、层内并发 4 | root wins；传递冲突报错；`requiredBy` 图进 lock |
| 传递依赖 | `readUpstreamDeps`（`graph.go:569-659`）**只读上游 `ngm.json`** | package.json 里的 Git 依赖只 warning，不入图（`:680-716`） |
| ref→commit | `resolve/reftype.go:54-81`，短 hash 用 `git rev-parse` 在 mirror 内补齐；tag/branch 用 `ls-remote` 取 peeled 行 | `refType` 仅完整 40 hex 窄默认；离线只认本地仓（`:271-287`） |
| commit→digest | `git/tree.go:156-168` → `streamedMaterial`（`:317-350`）经 `CatFileBatchEach` 逐 blob 流式哈希 | 清单 `version + NUL + path/mode/blobsha` UTF-8 排序；拒绝 gitlink/LFS 指针；256 MiB 单对象上限 |
| digest→vendor | `ContentStore.Put`（`vendor/content.go:270-392`）从内容重算 digest 自证；`LinkTree.materialize`（`links.go:140-253`）先校验路径再 hardlink→copy | 清单来自 v0.8 起的**逐文件 blob 池 + 树清单**（跨 commit 放大比 20×→0.6×） |
| verify | `verify.Run`（`verify/verify.go:136-205`）并发 8，逐依赖四查 | 退出码 `Report.ExitCode()`：2 完整性 > 4 操作 > 1 漂移，2 不可被缺工具 5 覆盖 |

### 2.4 四层磁盘布局

```
~/.ngm/mirror/       层 1   裸仓库，完整历史（git gc 可达）
~/.ngm/module-cache/ 层 1.5 partial clone(blob:none)，只服务 lock 钉住的模块
~/.ngm/content/      层 2   内容寻址 blob 池 + 树清单（Put 自证 · CheckEntry 区分缺失/截断）
~/.ngm/cache/        层 4   可丢弃缓存（OSV / 元数据）
<project>/ngm.vendor/层 3   逐文件链接到层 2（auto=hardlink 优先，跨卷降级 copy）
```

`CheckEntry`（`vendor/content.go:144-187`）是本层的关键设计：返回 `(present, issues)` 而非 bool——blob 不存在 = 资源缺失（exit 4）；长度不符 = **已确证的篡改**（exit 2）；等长改写只能靠 `verify --content` 的 digest 重放发现。`Has` 只在字节齐全且长度一致时为真（`content.go:190+`）。`ngm gc` 按当前项目 lock 做可达性回收，默认 dry-run，global vendor 一律保留（ADR-041）。

### 2.5 引擎与运行时模型

- **统一接口**（`adapter/engine.go:65-112`）：5 个能力口（Transform/Bundle/TypeCheck/TypeDecl/CSS）+ `Engine{Name,Kind,Version,Available}`。
- **四类实现**：subprocess（通用 builder + `genericInvocation` 支持自定义引擎免改码）、wasm（wazero/WASI，32 MiB 内存上限、只读挂载、无 socket/无写盘）、embed/self（恒 `stubError`，禁止静默降级）、remote（按 ADR-013 直接拒绝）。
- **单一探测路径** `Catalog.Probe`（`catalog.go:421-438`）：`Available()`（不 spawn）→ stub 短路 → `run:<engine>` 闸门 → `Version()`（spawn）。dry-run 走 `Runner.Plans`（`runner.go:199-236`）只调 `Available()`，`NoProbe=true`，不执行。
- **项目本地引擎**：`resolveProgram`（`subprocess.go:116-144`）先 PATH 再 `<dir>/node_modules/.bin`，与 `ngm run` 的 `.bin` 注入意图一致（但执行引擎仍须 `run:<engine>` 放行）。

### 2.6 安全与权限模型

- **权限**：5 命名空间 `read/write/net/run/env`，精确匹配无通配；`DefaultTier`（`security/permissions.go:139-155`）：read/write/env 默认放行，net 一律 Configurable，run 仅 `git` 默认放行，其余 Configurable；deny 优先。nil Policy 走默认档位（fail-closed）。
- **沙箱**：Deno 沙箱恒 `--deny-run/--deny-write/--deny-sys/--deny-ffi/--deny-hrtime` + 授权式 `--allow-read/net/env`，最小环境白名单；依赖脚本默认 deny，allow 也只在沙箱内跑（ADR-009/012）。
- **`ngm run`**：项目脚本在沙箱**外**执行，由独立 `run:script` 闸门控制（默认拒绝），与 install 期 postinstall 策略是两套闸门（ADR-042）；不接进 install/verify 自动流程。
- **构建可离线性**：两个 Go 模块在无 Gitee 凭据的冷构建实测 14.3s 通过；决定不 vendor、不引镜像，保留 CI 探测（tasks.md:71-79）。

### 2.7 输出与退出码契约

- 0 OK / 1 策略失败 / 2 完整性 / 3 配置用法 / 4 资源网络 / 5 引擎不可用 / 6 内部失败；`ngm run` 子进程的码**原样透传**（信号 → 128+signal），不映射到 0–6。
- `--json` 时 stdout **恰好一份** JSON，人读文本与诊断走 stderr；`runErr` + `failEarly` + `mergeCodes`（`errs.go:82-108`）是单一裁决点。
- `contracts/exit-codes.json` 是机器可读契约。**发现一处登记不一致**：其 `description` 指向 `TestV119ExitCodeContractMatchesCode`，实际测试名是 `TestV119ExitCodesJsonContract`（`exit_codes_json_test.go:22`）——承诺没有被它点名的判据守住（S3）。

---

## 3. 全维度逐域评估

### 3.1 `cmd/ngm`（28 命令）

- **健康**：命令表唯一（`root.go:38-67`）；`--json`/退出码/错误信封三通道统一；`install` 的 `materializeVendor`（`install.go:164-217`）在钩子失败时回滚本次新建 vendor，fresh 路径 lock 在钩子通过后写；`verify` 的 `finishVerify`（`verify.go:330-349`）用 `MergeCodes` 取最高严重度；`audit --json --hook` 的 hook 输出全改道 stderr（`audit_hook.go:43-50`）；`tree --offline` 三处落地；`build --dry-run` 不 spawn。这些正是 2026-11-07 评估的 S1 项，均已修复。
- **技术债**：15 个命令仍是 cmdContext 抽取前的手写 flag 模板；多处自建 JSON encoder（`engines.writeJSON`、`integrations`、`gcReport`、`runList`、各 modules writer）与 `c.writeReport` 并存；`install.go` 758 行混合命令/回滚/报告 schema，`modules.go` 约 44 KB。
- **小缺口**：`doctor.go:59` 的 `c.parse(..., nil)` 未标记 JSON 模式，parse 之后的失败不会写错误信封（对比 `verify.go:213`）；`tree.go:231-238` 的 `countVulns` 是死代码。

### 3.2 `internal/core`

- **健康**：信任链每一环都有机械守护（唯一 spawn 点、路径白名单 `IsSafeRelPath`/`IsSafeVendorPath`、图边无幽灵、digest 从内容重算、流式重放）。
- **前次修复复核**：lock 路径白名单（`schema.go:341-362,378-390`）、`CheckEntry` 三态（`content.go:144-187`）、git `buildEnv` 净化（`exec.go:147-232`）、digest 流式（已去掉 blob 驻留）、`resolve.Offline`（`reftype.go:271-287`）**均已真实修好**。
- **独立发现**：见 §4.2（P1 `--deep` 反向集合缺失）、§4.2（P2 `GIT_EXEC_PATH` 未净化）。

### 3.3 支撑子系统

- **adapter**：单一 Probe + 单一 spawn 构造点 + `WaitDelay=2s` + `genericInvocation`；闸门设计在"已配置权限"路径上健全。
- **OSV**：同源重定向（`osv.go:317-326`）、8 MiB 上限、30s 超时、缓存写失败降级 warning、`--offline --no-cache` 明确失败——G1 硬化已落地。
- **SBOM/attest**：CycloneDX 1.5 / SPDX 2.3 / in-toto Statement v1 + SLSA provenance v1，确定性、无时间戳、关 HTML 转义（因为"字节要被签名"）；purl 按 host 分流。
- **adopt**：识别 npm/pnpm 布局（含 scope、嵌套、pnpm symlink 且限制在 node_modules 内），对无 `package.json` 的 scoped 包回退 `@scope/name`；但只解析 npm `package-lock.json` v2/v3，pnpm/yarn lock 报 unknown。
- **semver 双实现**（低）：`outdated.go:17` 要求三段版本，`engines_extra.go:217` 的 `versionAtLeast` 接受任意段数——同一 "版本" 概念两套解析。

### 3.4 工程健康度

- 构建/vet/gofmt（干净导出）全绿；`./internal/...` 全量测试 87s 全绿；三平台 CI 有 `timeout-minutes`、覆盖率门槛（含"退化守卫"）、fuzz smoke、benchmark smoke、credential-free cold build 探测。
- 测试纪律强：跳过调用 56 处（41 `t.Skip` + 15 `t.Skipf`，历史台账从 59 降到 41 口径不同）并有 skip 审计台账；"dry-run 不得 spawn"、"JSON 恰好一个文档"、"权限拒绝零命中"都有负向测试。
- `contracts/exit-codes.json` 与 README / cli.md / ADR-026 / ADR-039 的退出码口径一致，有 `TestV119ExitCodesJsonContract` 守门（唯一瑕疵是它自己在 description 里点错了测试名，见 §2.7）。
- **采用面仍是最大风险**：公开仓库 2026-09-29 创建，无外部用户证据；release 资产覆盖远少于 tag（103 tag / GitHub 63 release / Gitee 3）。这不是代码缺陷，但直接决定"能不能装到 ngm"这个前提。

---

## 4. 问题清单

### 4.1 S0（阻断）——无全局配置时探测闸门缺席

**现象**：没有 `~/.ngm/config.json`（或 `NGM_HOME` 指向的空目录）时，`ngm engines list` / `engines validate` 会真实 spawn 项目 `ngm.engines.json` 声明的任意程序，且环境变量全继承。

**调用链**：`cmd/ngm/security.go:13-15` 的 `newPermissionPolicy(nil)` 返回 nil → `cmd/ngm/engine.go:57` 的 `if env.Policy != nil` 块**不注册** `cat.WithProbeGate` → `internal/adapter/catalog.go:369-370` 的 `probeAllowed` 在 `probeGate==nil` 时返回 true → `catalog.go:434` 的 `eng.Version()` → `subprocess.go` 的 `exec.CommandContext`。`runner.OnEngine`（`engine.go:81`）虽在 if 块外会注册，但那道闸门管的是"执行"，管不到只读命令触发的探测。

**黑盒复现（本轮实跑）**：隔离目录，`ngm.engines.json` 声明一个写 marker 文件的 `probe.cmd`：
| 全局配置 | 结果 |
|---|---|
| 无 config.json | `engines list` 里 probe 显示版本 `9.9.9-probe`，marker **被写入** → 已 spawn |
| `{"permissions":{"deny":["run:probe"]}}` | 显示 `not probed (probe denied)`，marker 未写入 |
| `{"permissions":{"allow":["run:git"]}}`（无 probe 条目） | 同样 not probed，marker 未写入 |

**影响**：克隆一个带恶意 `ngm.engines.json` 的仓库后，用户跑最无害的"列一下引擎"就会执行它；`deny` 配置在"没配过"的用户身上等于不存在。与 `net:` 在 nil Policy 下仍 fail-closed 的语义不一致。

**修复**：把 `cat.WithProbeGate` 的注册**移出** `if env.Policy != nil` 块；让 nil Policy 也走 `Policy.CheckRun`（nil receiver 的 `Allows` 已按 `DefaultTier(run:*)=Configurable` 拒绝）。判据：`TestEnginesListNoConfigDoesNotSpawn`（"零命中"断言，禁止 `t.Skip`）。

### 4.2 S2（中）

| 项 | 现象 | 证据 |
|---|---|---|
| `--deep` 反向集合缺失 | `replayDigestFromEntries` 只遍历 git tree 的条目去 store 取值，**不检查 store 清单里的多余条目**；攻击者若能写 `~/.ngm/content`，往 v2 清单塞一个指向合法 blob 的额外条目，该文件会被落地进 vendor，`--deep` 仍全绿 | `verify/verify.go:1002-1051`；`metaMismatches`（`:1069-1087`）只比 digest/manifestVersion；步骤 3 用同一份 storeEntries 自比 |
| `GIT_EXEC_PATH` 未净化 | `buildEnv` 三张表都没剔 `GIT_EXEC_PATH`（还有 `LD_PRELOAD`/`DYLD_INSERT_LIBRARIES`），可劫持 git 子命令 helper | `git/exec.go:147-183` |
| `ExtraEnv` 绕过过滤 | `ExtraEnv` 在过滤循环之后 append，不过 deny/repoLocating/hijack 表；modproof 路径会透传它 | `git/exec.go:230`；`verify.go:653-661` |
| `--json` 错误信封漏网 | `doctor` parse 未标记 JSON 模式，parse 之后失败不出信封 | `cmd/ngm/doctor.go:59` vs `verify.go:213` |
| 签名"查不了"当作"没签" | `CommitSignature` 出错时 `notSigned++`，诊断误导用户去加密钥而非修 mirror | `cmd/ngm/verify_signatures.go:65-69` |

### 4.3 S3（低）

- `contracts/exit-codes.json` 登记了一个不存在的测试名（§2.7）。
- `ngm run` 的 `ChildEnv` 全继承父环境，未覆盖 `deny:["env:X"]`；对"用户自写脚本"可能是有意信任边界，但 usage 未明示（`runscript/exec.go:66-82`）。
- `probeVersion`（`engines_extra.go:122,212-214`）绕过 `probeGate`，目前只因调用顺序间接被覆盖。
- semver 双实现、多处自建 JSON encoder、若干手写 flag 模板、死代码 `countVulns`。

### 4.4 2026-11-07 评估的修复复核（全部落地）

probe 闸门（部分，见 §4.1）、audit 单 JSON、verify 退出码合成、gc JSON 信封、`tree --offline`、lock 路径白名单、git 环境净化主体、adopt pnpm 布局、ContentStore 三态、install 回滚、OSV 硬化、sandbox deny 集、semver/URL 脱敏统一、cat-file 流式、CI 加固、覆盖率门槛、fuzz、命令骨架抽取——逐项在 `docs/specs/assessment-2026-11-07-followup/tasks.md` 登记为 `[x]`，并抽样在源码中确认了对应实现。**唯一残留是 §4.1 的半边修复。**

---

## 5. 相对 npm：简单 JS 项目的替代缺口

> **读这一节前先看定位（2026-11-19 补）**：[ADR-044](../../docs/adr/adr-044-independent-ecosystem.md)
> 定下「ngm 是独立生态，npm / pnpm 只是迁移来源」。本节因此是一份**迁移成本的测量**，
> 不是待办清单：它回答“一个 npm 项目要付多少代价才能搬过来”，而不回答“ngm 还差哪一格”。
> 两者的区别很具体——按缺口表去补，会把 ngm 推向兼容层；按定位去补，只会补
> “可证明性、离线、审计、安全边界”这一侧。

### 5.1 先立前提

ngm 明确自我定位为 **Git 依赖的证明层**，不是通用包管理器：不解析 registry、不接管 `node_modules`、不求解 peer、不发布（README §边界、capability-matrix §明确不做）。因此下表不是"ngm 做得不好"，而是"要把 ngm 当独立工具替换 npm，还差什么"。

### 5.2 能力对照

| 维度 | npm | ngm 现状 | 判定 |
|---|---|---|---|
| 依赖源 | registry tarball + Git URL | **仅 Git URL**（4 协议归一化） | 硬缺口 |
| 版本模型 | semver range / dist-tag / 求解器 | refType=commit/tag/branch，字面量，无 `^~>` | 硬缺口 |
| 传递依赖 | 嵌套树 + 提升 + peer/optional/dev | 只读上游 `ngm.json`，root wins，无 peer/optional/dev 语义 | 硬缺口 |
| 落地布局 | `node_modules`（扁平 + scope + `.bin` + 嵌套） | `ngm.vendor/<canonical>[/subPath]` + mappings alias | 硬缺口 |
| 锁定 | `package-lock.json` + integrity | `ngm.lock` + commit + 内容 digest + `requiredBy` 图 | **ngm 更强**（Git 侧） |
| 完整性 | registry integrity | archiveDigest/moduleDigest 可本地重放、离线可验 | **ngm 更强**（内容侧） |
| 漂移 | 基本无 | expected/unexpected/critical 三态 | ngm 独有 |
| 脚本 | `npm run`（shell + pre/post + 全 env） | `ngm run`（已实现，pre/post/`--`/透传/最小 env） | 基本对齐，env 最小集 |
| 生命周期 | 默认执行 install 全族 | 默认 deny；allow 也只在 Deno 沙箱跑 JS 钩子 | 有意收窄（安全） |
| 审计 | `npm audit`（registry advisory） | `ngm audit`（OSV，按 commit） | 不同信任源 |
| SBOM/provenance | L3（Sigstore/Rekor，默认附） | SBOM + **未签名** in-toto/SLSA（内容侧 L2） | 互补，非竞争 |
| 发布 | `npm publish` / dist-tag | `publish` 字段只服务模块索引 | 硬缺口 |
| workspace | `workspace:*` + 批量脚本 | `modules` 是集合索引，非 workspace 语义 | 定位差异 |
| lock 互操作 | — | `adopt` 只读 npm lock 出证明，不回写、不消费对端 lock 安装 | 单向 |
| 引擎供给 | registry 本地 `.bin` + npx | 外部预装或独立二进制；已可查项目 `.bin`，无 npx | 部分缺口 |

### 5.3 三种项目类型的可替代性（直接回答）

| 项目类型 | 能否完全不用 npm | 具体还缺什么 |
|---|---|---|
| 依赖全是 Git 依赖；引擎为独立二进制/系统包/Deno；脚本只用 node + 本地工具 | **能**（v0.120 后） | 引擎可得性；`run:script`/`run:<engine>` 首次授权 |
| 依赖全是 Git 依赖；但用 esbuild/tsc/postcss（主分发是 npm） | **不能完全**——脚本可以，工具链不行 | 引擎的非 npm 供给路径（独立二进制/系统包/vendored 工具） |
| 有任意 registry 依赖 | **不能** | registry 安装能力，明确不做 |

### 5.4 缺口分层（按"补它需要什么"）

**A. 设计边界（不建议补，60–120 人日且摧毁定位）**
registry 协议 + 鉴权 + 镜像、semver solver、peer deps 求解、tarball integrity 体系、`publish`/dist-tag、lifecycle 全族、`npm_config_*` 全量模拟、自研 shell。这些是"另一个产品"。

**B. 可补但未做（成本 1–8 人日/项）**
1. `adopt` 支持 pnpm-lock.yaml / yarn.lock（只读解析，5–8 人日）。
2. vendored Git 依赖的 `.bin` shim 生成——纯 Git 项目里被声明为 `bin` 的 CLI 目前只能写全路径调用（2–3 人日）。
3. `ngm gc` 覆盖 module cache（1–2 人日）。
4. `verify --content` 的 module 子路径端到端用例、gc 删除失败用例、并发 install 安全窗测试（各 1–2 人日）。
5. npm 环境变量：只承诺 5 个最小集，不设 `npm_execpath`、不承诺 `npm_config_*`；需要它们的脚本继续用 npm（这是契约，不是 bug）。
6. 发布与分发：每个 tag 有可校验产物；一键安装路径。

**C. 已补上（v0.120 起不再是缺口）**
`ngm run` 脚本层（发现/注入/透传/pre-post/权限/退出码）已在 ADR-042 落地；引擎解析已优先查项目 `.bin`。这使"纯 Git 依赖 + 非 npm 引擎"项目真正可以移除 npm。

### 5.5 一句话判断

> **ngm 不是"更小的 npm"，而是"npm 不管的那段 Git 依赖，做得可证明"。**
> 要替代 npm 的不是全部场景，而是很窄的一类：依赖来自 Git、引擎不靠 npm 分发、你需要离线/审计/SBOM。
> 在这个窄场景里它已经闭环；往外扩一厘米，成本就从"补丁"跳到"另一个产品"。

---

## 6. 优先级建议

| 优先 | 事项 | 理由 |
|---|---|---|
| **P0** | 修 §4.1：把 probe 闸门注册移出 `if env.Policy != nil`，补"零命中"负向测试 | 当前默认的"完全没配置"用户暴露在最无害的诊断命令上；已有黑盒证据 |
| P1 | `--deep` 加 store 清单反向集合校验；`buildEnv` 补 `GIT_EXEC_PATH`/`LD_PRELOAD` | 信任根与进程边界的完整性 |
| P1 | `doctor` 补 markJSON；`contracts/exit-codes.json` 修正测试名 | 对外契约的可验证性 |
| P2 | adopt 支持 pnpm/yarn lock、`.bin` shim、gc 覆盖 module cache | 降低迁移门槛的最后几厘米 |
| P2 | release 资产补齐 + 一键安装路径 | 决定"装得上 ngm"的前提，非技术问题 |
| **不做** | registry 安装、semver 求解、peer、publish、lifecycle 全族、npm_config 全量 | 与定位冲突，成本 60–120 人日且永远追不上 |

---

## 7. 局限

- 黑盒只针对 §4.1；其余结论来自逐行阅读 + 调用链，未逐条构造运行时证据。
- 覆盖率引用 CI 口径的既有读数（82.1%）；本轮定向 verify 子集实测 60.6%，两者口径不同，不能互相比。完整 `go test ./...` 本机未跑完（`cmd/ngm` 串行约 40 分钟），以 `./internal/...` 全量（87s 全绿）与 verify 定向测试替代。
- 发布侧只做静态核对与文档读数比对；未下载 release 资产校验 SHA256SUMS。
- 竞品能力来自 ngm 自己的 `docs/COMPETITIVE-ANALYSIS.md`（事实截止 2026-09），未重新联网复核各工具当前版本。

---

## 下一步

本报告的问题清单已落成执行计划：[开发批次 spec](../specs/assessment-2026-11-18-followup/spec.md) 与 [逐项任务](../specs/assessment-2026-11-18-followup/tasks.md)（A 止血 → B 信任根 → C 契约 → D npm 迁移小缺口）。

**执行状态（2026-11-18 当日）**：A / B / C / D1 / D3 / F3 已实现并通过 build/vet、internal 全量与 cmd/ngm 定向回归；D2（vendored `.bin` shim）按 [ADR-043](../adr/adr-043-vendored-bin-shim.md) 决定暂缓——它越过 ADR-042 的边界并从第三方内容创建可执行面，需先有威胁模型；E（release 资产）需外部凭据，未推进。

## 相关文档

- [2026-11-07 全量评估](./../internals/assessment-2026-11-07-full.md) 与 [修复批次](../specs/assessment-2026-11-07-followup/tasks.md)
- [2026-11-04 脚本/替代评估](./ngm-assessment-2026-11-04.md)（本报告 §5 的直接前文）
- [架构总览](../architecture/overview.md) · [能力矩阵](../internals/capability-matrix.md) · [竞品分析](../COMPETITIVE-ANALYSIS.md)
