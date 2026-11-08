# ngm 全量代码评估（2026-11-07）

> 评估对象：`github.com/idcu/ngm`，HEAD `2c16cdb`（2026-11-04），工作树干净。
> 评估方式：只读。逐包阅读实现与测试 + `go build` / `go vet` / `go test ./...` /
> `go test -cover ./internal/...` + 一个隔离目录里的黑盒复现。
> **本轮没有修改任何产品代码。**
>
> 证据约定：`文件:行` 指向 HEAD 的实际行；跨文件结论给出至少一条可复核的调用链。
> 严重度：**S0 阻塞发布 / S1 高（安全或对外契约被破坏）/ S2 中（正确性或可维护性）/
> S3 低（表述、一致性、体验）**。

---

## 0. 结论摘要

1. **最重要的发现是一个安全边界缺口，不是功能缺陷。** 仓库声明里写的引擎程序会在
   `ngm build --dry-run`、`ngm engines list`、`ngm engines validate` 这三条"只读/不执行"
   的路径上被真的 spawn 出来，并且**不经过 `run:<engine>` 闸门**；探测时的环境变量
   也完全继承（`deniedEnv = nil`）。已在临时目录黑盒复现：dry-run 两次执行探针，
   同一工程非 dry-run 反而被 `run:probe` 拒绝（exit 3）。见 **S0-1**。
2. **对外承诺与实际行为有三处硬冲突**：`audit --json --hook` 会向 stdout 写第二个
   JSON 文档；`verify` 的后置层会用后置错误码覆盖基础判定码（2 被 5 覆盖）；
   `tree --offline` 在 lock 有图时仍可能联网。这三处都有文档原句可对照。
3. **lock 是信任根，但它对路径字段几乎不设防。** `vendorPath` / `subPath` 只校验
   "非空"，而 `verify` / `--sandbox` 直接把它拼进 vendor 根做 `os.Stat` / 找脚本。
   恶意 lock 可以把校验脚本指到 vendor 之外。
4. **存储层"命中即信任"**：`ContentStore.Has` 只确认 blob 路径存在，不校验大小/哈希，
   损坏块会被硬链进 vendor，且损坏的 v2 manifest 无法自动重修。
5. **测试数量很大（867 个 Test），但守不住上面这些点**：缺"dry-run 不得执行"的负向
   测试、缺"audit 单 JSON 文档"的字节级断言、缺 lock 路径逃逸的用例；59 个 `t.Skip`
   里有一部分跳过的正是边界本身；0 个 Fuzz。
6. **文档与发布的读数已经分叉**：`docs/guides/release-pipeline.md` 是 2026-11-03 的
   权威读数（101 tag / 63 release / 缺口 38），而 README、`docs/README.md`、
   `installation.md` 分别停在 v0.64 / v0.65 / "缺口 19"。README 甚至写着
   "差的 0 个是未推送的"，语义自相矛盾。
7. 架构本身是健康的：依赖方向单向（`cmd/ngm → internal/core`，`internal/config →
   resolve`），没有反向依赖，包边界基本按"领域 + 副作用出口"划分。技术债主要是
   **重复的命令骨架**与**重复的"探测/执行"路径**，不是结构性腐化。

**批次建议（详见 §8）**：先做 P0 安全闸门（Batch A），再做输出/退出码契约
（Batch B），再做离线与路径（Batch C），最后才是存储与 install 语义（Batch D）。
A 与 B 无耦合，可以并行；C 的 lock 路径校验必须早于 D 的 install 顺序调整，
否则 D 的回滚测试会依赖一个仍可被恶意 lock 绕过的路径。

---

## 1. 方法与证据基线

### 1.1 实际执行过的命令

| 命令 | 结果 |
|---|---|
| `go build ./...` | 通过 |
| `go vet ./...` | 通过 |
| `go test ./...` | 全绿（`cmd/ngm` 约 560s，`internal/*` 0.05–57s） |
| `go test -cover ./internal/...` | 各包覆盖率见 §5.7 |
| `git status --porcelain` | 空 |
| `git tag` | 103 个（101 正式 + `v0.102.0-dev.1` / `v0.102.0-dev.2`） |

### 1.2 清点读数

| 项 | 读数 |
|---|---|
| 模块 | `github.com/idcu/ngm`，`go 1.25.0` |
| 外部 Go 依赖 | 2 个：`github.com/tetratelabs/wazero v1.9.0`、`gitee.com/idcu-go/modproof v0.2.0` |
| 实现（`cmd/ngm`） | 41 文件 / 10,459 行 |
| 实现（`internal`） | 101 文件 / 24,612 行 |
| 测试（`cmd/ngm`） | 162 文件 / 30,077 行 |
| 测试（`internal`） | 127 文件 / 21,158 行 |
| `_test.go` | 289 个；`Test` 函数 867 个；`t.Skip` 59 处；Benchmark 3 个；Fuzz 0 个 |
| Markdown | 315 个 / 2,677,042 字节 |

> "零依赖"的说法不成立：`go.mod` 明确 require 了上面两个模块。其一是 wasm 运行时，
> 其二是托管在 **Gitee** 的 `modproof`——这直接决定了构建的可离线性（见 S2-15）。

### 1.3 复现记录（隔离目录，未触碰仓库）

在 `%TEMP%\ngm-probe-eval` 建了最小工程：

- `probe.exe`：收到 `--version` 时向 `%TEMP%\ngm-probe-marker.txt` 追加一行，
  并打印 `9.9.9-probe`。
- `ngm.engines.json` 声明 `esbuild → probe.exe`；`ngm.json` 为最小工程。
- `NGM_HOME/config.json` 写 `{"permissions":{"deny":["run:probe"]}}`。

观测结果：

| 命令 | 探针是否执行 | 退出码 |
|---|---|---|
| `ngm engines list --dir=<proj>` | **是**（marker 1 行） | 0 |
| `ngm build src/index.ts --dry-run` | **是**（marker 2 行） | 0 |
| `ngm build src/index.ts` | 否（`run:probe` 拒绝） | 3 |

补充复现：`ngm gc --json --dir=<不存在>` → stdout **0 字节**，exit 3。

---

## 2. 项目画像

ngm 是"Git 依赖的证明层"：`ngm.json` 声明 → `ngm.lock` 锁 commit + 内容摘要 →
`ngm verify` 重放 → `sbom` / `attest` 导出审计产物。它明确不做 registry、
不接管 `node_modules`、不自造签名体系。CLI 共 28 条命令（`cmd/ngm/root.go`
的 dispatch 表）。

退出码契约：0 OK / 1 非预期漂移 / 2 完整性（critical）/ 3 配置与权限 /
4 资源缺失与网络 / 5 工具缺失 / 6 内部与写失败；`ngm run` 子进程码透传
（信号 128+sig）。`contracts/exit-codes.json` 与 `internal/core/errs` 一致。

---

## 3. 架构与包划分

### 3.1 依赖方向

```
cmd/ngm (28 命令入口)
 ├── internal/config          // ngm.json / ngm.engines.json / 全局 config
 ├── internal/core
 │    ├── resolve  → git      // 解析 ref / 图
 │    ├── digest              // 规范清单与哈希
 │    ├── verify              // 三层判定 + SARIF/GitHub
 │    ├── lock                // schema + 图
 │    ├── vendor              // mirror / content store / links / materialize
 │    ├── git                 // 唯一的子进程构造点（spawn.go）
 │    ├── errs / atomicfile / boundbuf / envutil
 ├── internal/adapter          // 引擎探测与执行（wazero / subprocess）
 ├── internal/security         // 权限策略 + Deno 沙箱
 ├── internal/supplychain      // OSV
 ├── internal/observability    // tree / outdated
 ├── internal/sbom, attest, adopt, integrations, runscript, ...
```

检查结论：没有 `internal/* → cmd/ngm` 的反向依赖；`internal/config → resolve`
是唯一允许的跨层引用（为 `--infer-ref-type`）。**结构合理。**

### 3.2 划分上的两处模糊

1. **`internal/core` 过大**（24.6k 行、含 `git` / `vendor` / `verify` / `resolve`）。
   它事实上是一个"标准库集合"，不是单一领域包。可拆，但不紧急。
2. **`cmd/ngm` 承担了业务逻辑**：`install.go`（职责：fresh 安装 + lock 重装 +
   hook 编排 + 写 lock）、`modules.go`（约 1,200 行，含 semver、slug、索引、
   changed/plan）、`env.go`（路径与策略解析）。这三处是最主要的技术债来源（§5.10）。

---

## 4. 命令实现审查（按命令）

> 逐条命令的 flag 与文档一致性核对见 §5.8；这里只列"实现正确性与边界"。

| 命令 | 审查结论 | 证据 |
|---|---|---|
| `build` / `typecheck` / `typedecl` / `transform` / `css` | `--dry-run` 仍会 probe 引擎；`Plans` 注释说"只读决定"但实际调用 `Available()`/`Version()` | `cmd/ngm/build.go:25`、`internal/adapter/runner.go:191-227` |
| `engines list/validate` | 对项目 catalog 逐条探测，无 `run:<engine>` 闸门、`deniedEnv=nil`；缓存键 `Program+"\x00"+Args` 可碰撞 | `cmd/ngm/engines.go:275-315`、`internal/adapter/catalog.go:412-432,515-554` |
| `install` | 先跑 hooks 再写 lock，无回滚；`installFromLock` 传 `previous=nil`；`Store.Has` 通过即硬链 | `cmd/ngm/install.go:186-192,361-369,409-414,528` |
| `verify` | 后置层覆盖 baseCode（2 被 5 覆盖）；人类摘要打印 base 码 | `cmd/ngm/verify.go:321-323,326-353,359-377,490` |
| `verify --sandbox` | `VendorPath` 未做形状校验就 `Join` | `cmd/ngm/verify_sandbox.go:30` |
| `verify --require-signed` | 空 lock / 零依赖时 `checked==0` → exit 2 | `cmd/ngm/verify_signatures.go:86-93` |
| `tree` | lock 有图时逐节点 `ResolveRef`，`ResolveOptions` **没有 Offline 字段** → 冷 mirror 时回退 `CloneURL` | `cmd/ngm/tree.go:110-143`、`internal/core/resolve/reftype.go:16-33,230-247` |
| `gc` | `--json` 的配置/lock 失败未 `markJSONIfRequested`；mirror `RemoveAll` 无 age grace / 进程锁 | `cmd/ngm/gc.go:129-195` |
| `adopt` | pnpm 顶层包是 symlink → `Lstat` 判非目录 → `skipped++`；`.pnpm` 点前缀被跳过；零包时提示 "re-run your installer" | `internal/adopt/adopt.go:140-160,255-288` |
| `update` | `--store` / `--digest` 是展示开关，`Put()` 与 digest 恒执行 | `cmd/ngm/update.go:185-230` |
| `audit` | 先写 JSON 报告，再跑 hook；hook 失败仍走 `runErr` → stdout 第二个 JSON | `cmd/ngm/audit.go:132-143`、`audit_hook.go:48-49,57-104` |
| `run` | `--json` 被并进 `--list` 的判定当 list；`--` 后参数裸拼进 shell 但 usage 写 "verbatim" | `cmd/ngm/run.go`、`internal/runscript/runscript.go` |
| `modules` | `validate --strict` 接受 `1.2.3-rc1`，`plan` 只接受 `x.y.z`；slug 解析失败时原样打印 origin URL | `cmd/ngm/modules.go:164-165,811-812` |
| `doctor` | 恒 exit 0、usage 已写明"diagnostic"，**不是缺陷** | `cmd/ngm/doctor.go` |
| `remove` | 只改 `ngm.json`，提示"run ngm update"，是文档化的 frozen 语义 | `cmd/ngm/remove.go` |

---

## 5. 逐维度评估

### 5.1 整体架构与包划分

**评分：好（8/10）。** 单向依赖、副作用出口收束（git 只有 `spawn.go` 一处构造、
HTTP 出口在 `queryRemote`、沙箱在 `security.Deno`）是这个项目最扎实的部分。
`TestSpawn_OnlyOneFileBuildsProcesses` 与 `TestHTTPEgressIsRegistered` 把"没有第二个
出口"写成了机械判据。

扣分点：

- `internal/core` 是"桶"，不是领域包；新增能力继续往里放会让它继续膨胀。
- `cmd/ngm` 里 `install.go` / `modules.go` / `env.go` 内聚了本应属于 `internal` 的
  逻辑（lock 写入顺序、semver、slug），导致它们的测试只能走 CLI 集成路径。

### 5.2 命令实现正确性与边界条件

见 §4 表。三个"输出正常、结论错"的类型仍存在：

1. `verify` 的**退出码合成**（S1-2）：`finishVerify` 注释承诺"只追加、任一层 2"，
   实现却把 `runErr` 的返回值直接当最终码。基础层已判 critical(2)，缺 Deno 时
   返回 5，覆盖成 5——CI 会把"完整性失败"当成"环境缺工具"。
2. `tree --offline` 的**离线语义**（S1-3）：`lock` 有图分支的 `ResolveOptions`
   根本不携带 offline，冷 mirror 时 `pickLSRemoteURL` 回退到 `CloneURL`。
   usage 第 26 行与第 54 行分别承诺 "never touch the network" 与
   "including --offline with a cold mirror"（后者应为 exit 4）。
3. `adopt` 的 **pnpm 兼容**（S1-6）：这是"非法输入被静默降级"而非崩溃——
   pnpm 的顶层包是 symlink，`addPackage` 的 `Lstat` 直接 `skipped++`，
   最后报"0 包，请重跑安装器"，用户会去重装而不是看 lock。

### 5.3 错误处理与退出码契约

契约本身是清楚的，问题在**合成点**：

- `runErr`（`cmd/ngm/main.go:83-95`）在一个进程里被多个层次复用。它既负责
  "打印 + 返回码"，又被 `verifyWithSandbox` 当作"提升后置层码"的手段，
  于是"错误码优先级"没有单一裁决点。
- `verify.go:326-333` 的注释与 `:359-377` 的实现互相否定；`:490` 的人类摘要用的是
  `rep.Summary.ExitCode`（基础码），而进程返回的是合成码——同一份输出里
  两个"退出码"。
- `gc.go:129` 声称"与其他命令同一条通道：提了 --json 的早期失败也要给一份错误信封"，
  但配置/lock 失败分支（`:139-195`）没有 `markJSONIfRequested`，实测 stdout 为空。
- `contracts/exit-codes.json` 的描述里写了守护测试名
  `TestV119ExitCodeContractMatchesCode`，实际测试是 `TestV119ExitCodesJsonContract`——
  登记表指向一个不存在的判据，等于这条承诺没有人守。

### 5.4 CLI 用户体验与输出一致性

- 早期 JSON 信封（`cmd/ngm/root.go:82-131` 的 `failEarly`）只覆盖 13 条命令；
  `gc` 是其中之一，但配置阶段失败仍绕过了它（见上）。
- `audit --json --hook`：`docs/guides/cli.md:67` 承诺"stdout 是单个 JSON 文档"；
  实现是"先写报告，再让 hook 失败往 stdout 写一份错误信封"。两个 JSON 文档
  不能一起解析——而这正是 CI 的用法。
- 人类输出里 `→`/`⚠`/`✗` 的语义整体一致，`observability.md` 有对照表；这块质量高。
- `cli.md` 的命令行缺 `--dir`（`build` / `typecheck` / `typedecl` / `transform` / `css`），
  用户会以为这些命令不接受目录。
- `run` 的 `--json` 与 `--list` 被合并（一个布尔量 `*list || *asJSON`），
  `ngm run --json <script>` 会退化成 list 并报错——文档没有写这条限制。

### 5.5 安全边界

#### 5.5.1 脚本执行权限闸门（S0）

`run:<engine>` 的施加点只有 `adapter.Runner` 的 preflight（`security-model.md`
的施加点表格里也是这么写的）。但探测路径有三条完全不同的入口：

- `Runner.Plans`（`internal/adapter/runner.go:191-227`）：注释说"只读决定，不做任何调用"，
  实际在 `:219-220` 调 `eng.Available()` 与 `eng.Version()`。`subprocessEngine.Version()`
  会执行 `--version`（`internal/adapter/subprocess.go:90-150`）。`Plans` 不经过
  `runChain` 的 `beforeRun` / `OnEngine`（`:384-390` 才判定）。
- `catalog.Validate` / `probeEngine`（`internal/adapter/catalog.go:412-432,515-554`）：
  无条件 `Version()`，且 `probeVersion` 传 `nil` 作为 deniedEnv（注释自述"不接触策略"）。
- `cmd/ngm/engines.go:304-315`：直接 `adapter.NewEngine` + `Available()` + `Version()`，
  共用同一份缓存键。

后果：任何被克隆的仓库都能在 `ngm.engines.json` 里声明一个程序，让
`ngm engines list` / `ngm build --dry-run` 执行它，且**即使配置里写了
`deny:["run:<name>"]` 也不会被拦**。探测进程还拿到完整环境变量
（`subprocess.go` 的 `deniedEnv` 在这条路径上是 nil），token 类变量随之外泄给
一个未被授权的程序。这是本轮唯一的 S0。

附带两点（同一条修复应一起处理）：

- `Available()` 用**进程 CWD** 做 `LookPath`，但执行时 `cmd.Dir=项目目录`
  （`internal/adapter/subprocess.go`）；两者不一致时"探测可用、执行找不到"。
- 引擎 PATH 没有注入项目的 `node_modules/.bin`，而 `ngm run` 注入了。

#### 5.5.2 路径处理

- `lock.Dependency.VendorPath` 只校验非空（`internal/core/lock/schema.go:312-314`），
  `SubPath` 连 `..` / 绝对路径都不校验（`:277-311`）。
- `verify` 的 landing 检查直接 `filepath.Join(opts.VendorRoot,
  vendor.VendorPathFor(d.VendorPath, d.SubPath))` 再 `os.Stat`
  （`internal/core/verify/verify.go:847-849`），`--sandbox` 同形
  （`cmd/ngm/verify_sandbox.go:30`）。
- 写路径目前**没有**被污染：install 用 `env.go:550` 的 `MirrorRelPath()` 落地，
  不读 `lock.VendorPath`；但这只是"另一个字段恰好没用它"，不是防线。
  `cmd/ngm/env.go:443` 的注释自己承认这是"见 lock.VendorPath 未校验的已知边界"。
- `env.go:285-320` 的 `globalPath` 只做词法 `filepath.Rel`，没有 `EvalSymlinks`；
  符号链接可以绕出项目根。

#### 5.5.3 下载与校验

- `ContentStore.Has`（`internal/core/vendor/content.go:125-157`）：v2 分支逐个
  `os.Stat` blob，**不做哈希、不校验大小**（注释明说这是 verify 的职责）。
  `install` 的 `:409-414` 一旦 `Has` 通过就直接硬链——损坏块被"可信"地搬进 vendor。
- `hasManifest` 只 `os.Stat(manifest.json)`（`content_v2.go:138`）；
  `:313-318` 的 rename 失败分支在 final 已存在时返回 `nil`，损坏 manifest 无法自动重修。
- `supplychain/osv.go`：`http.Client` 没设 `CheckRedirect`，响应体没套
  `io.LimitReader`（`:108-150`）；`writeCache` 失败会让查询整体失败（`:143-147`）；
  `--offline --no-cache` 仍读 stale 缓存（`:127-133`）。
- `digest/lfs.go:49-53`：指针超过 1 KiB 就当普通文件哈希，与 ADR-008
  "必须报错，不得静默哈希"相反。

#### 5.5.4 凭据与敏感信息

优点：`internal/core/git` 有统一脱敏出口，`buildEnv` 剔除仓库定位变量，
`GIT_TERMINAL_PROMPT=0`，mirror `.git/config` 不含凭据（有测试）。

缺口：`buildEnv`（`internal/core/git/exec.go:158-201`）**没有**剔除
`GIT_CONFIG_GLOBAL` / `GIT_CONFIG_SYSTEM` / `GIT_CONFIG_PARAMETERS` /
`GIT_SSH_COMMAND` / `GIT_ASKPASS` / `GIT_EXTERNAL_DIFF`，也没有处理
`insteadOf`。一个被污染的父环境（或 hook 脚本）可以让 git 走任意 ssh 命令、
任意 credential helper，或把 host 白名单里的 URL 改写成另一个 host。
`remoteurl` 的 scheme 白名单只在 URL 解析层，`CloneMirror` 不做二次确认。

`sandbox`（`internal/security/sandbox.go:96-160,287`）：

- `cmd.Env = envWithout(os.Environ(), d.DeniedEnv)`——只剔除 deny 列表，其余全继承；
- 未显式 deny `ffi` / `sys` / `hrtime` / `node`；
- `--allow-read` 用逗号拼接（`:115`），含逗号的路径会被拆成两项。

`ngm run` 继承父环境是**文档化设计**（`security-model.md:50-52` 明确写了），
不计为缺陷。

#### 5.5.5 元数据注入

- `modules.go:811-812`：slug 解析失败时原样打印 origin URL，可能带 token。
  脱敏只在 `git` 的统一出口做，`modules` 这条路径没有经过它。
- `sarif.go:245-275`：`sarifURI` 直接返回 `d.VendorPath`，缺
  `ngm.vendor/` 前缀；`sarif_test.go` 的 fixture 自带前缀，掩盖了这一点。
- `github.go:65-76`：`ghEscape` 只转义 `% \r \n`，缺 `:` / `,`——
  GitHub Actions 注解里这两个字符是分隔符。

### 5.6 性能

| 关注点 | 现状 | 判断 |
|---|---|---|
| 哈希计算 | 流式读取，`digest` 包对 20/200/2000 文件有 benchmark；`ContentStore.Has` 有
  `BenchmarkContentStoreHas`（0.93ms / 8.25ms / 77.1ms，全命中页缓存） | 可接受 |
| `Has` 的 O(files) stat | 2000 文件 77ms，install 每次依赖各一次 | 中树可接受；万级文件是明显瓶颈 |
| git 子进程输出 | `bytes.Buffer` 无界（`internal/core/git/exec.go:97-99`） | 大树 `ls-tree` 有内存风险 |
| cat-file | 有 `CatFileBatch`，但 verify 的逐文件重放仍是串行 + 每 blob 一次 | 大依赖树偏慢 |
| 并发 | verify 有 semaphore，默认并发有限 | OK |
| 缓存 | `Has` 不缓存（CHANGELOG 明确"本轮不引入缓存"并给了理由） | 决策合理，但要在 P2 里重新评估 |

### 5.7 测试覆盖与实际薄弱点

`go test -cover ./internal/...` 实测：

| 包 | 覆盖率 |
|---|---|
| verify | **34.7%** |
| observability | 53.5% |
| adapter | 70.6% |
| config | 74.4% |
| atomicfile | 72.4% |
| errs | 76.2% |
| vendor | 78.7% |
| git | 81.3% |
| lock | 83.0% |
| resolve | 87.3% |
| security | 88.1% |
| supplychain | 86.9% |
| sbom | 92.9% |
| attest | 95.3% |
| digest | 99.0% |
| boundbuf / envutil | 100% |
| version | **0%** |
| testutils | 25.7% |

**薄弱点不是"数字低"，而是低在关键路径上**：

- `verify` 34.7% 是**判定核心**（三层判定 + landing + SARIF）的覆盖率；
  上面 S1-2、S1-4、S3 的 SARIF 路径问题都在这个包里。
- 867 个 Test 里没有一条断言"dry-run 不得 spawn"——这正是 S0 能长期存在的原因。
- `audit --json --hook` 没有"stdout 必须恰好一份 JSON"的字节断言。
- lock 的 `VendorPath` / `SubPath` 没有逃逸用例（`..`、绝对路径、盘符）。
- 59 个 `t.Skip`：`TestV105SandboxDeniesModuleLoading` 的注释明确写"禁止 t.Skip"，
  说明团队知道"跳过的就是边界"，但其余 skip 没有同样的纪律。
- 0 Fuzz：`digest` 清单解析、`lock` schema、SARIF/GitHub 转义都是理想的 fuzz 目标，
  且都是"输入驱动 + 纯函数"。

### 5.8 文档与实现一致性

| 文档 | 与实现的冲突 | 证据 |
|---|---|---|
| `docs/guides/cli.md:67` | 承诺 `--json --hook` 单个 JSON；实现写两个 | `audit.go:132-143` |
| `cmd/ngm/build.go:25` | "exit without running anything"；实际 probe 执行 | `runner.go:219-220` |
| `cmd/ngm/tree.go:26` | "never touch the network"；lock 有图分支可联网 | `tree.go:127-132` |
| `security-model.md` 施加点表 | `run:<engine>` 只登记了 preflight，探测路径未登记 | `catalog.go` / `engines.go` |
| `README.md:38-48` | release 覆盖写 v0.64；"差的 0 个是未推送的"自相矛盾 | `release-pipeline.md:9-50` |
| `docs/README.md:8-21` | 写 60 个 release / 最新 v0.64 / v0.66+ 无 release | 同上 |
| `docs/guides/installation.md:24-56` | 四个"最新"版本互相矛盾（v0.65 / v0.64 / v0.12 / v0.4），缺口 19 vs 98 | 同上 |
| `docs/guides/cli.md` | `build`/`typecheck`/`typedecl`/`transform`/`css` 行缺 `--dir` | 命令 usage 都有 `--dir` |

`docs/guides/release-pipeline.md` 本身是高质量、可复核的（给了 API 读数来源与
fixture 验证方式），问题是**另外三份文档没有跟着它更新**。

### 5.9 构建 / CI / 发布链路

- **构建可离线性受损**：`gitee.com/idcu-go/modproof` 需要访问 Gitee；
  CI 需要 `GITEE_TOKEN` 或代理。对一个"证明层"项目，构建依赖第三方私有主机的
  模块是可被质疑的（S2-15）。
- `.github/workflows/*.yml`：4 个 workflow **都没有 `timeout-minutes`**；
  `go test` 的超时是 40m，一次卡死会占满 runner。
- `release.yml:60-63` 的 tag 校验只有 `case v*`，`vfoo` 能通过。
- release 测试只在 Ubuntu 跑；三平台构建验证与测试验证不对称。
- `ci.yml` 的 engine-adapter job 用 `npm install -g esbuild typescript postcss-cli postcss`
  ——**未钉版本**，上游一发新版就可能红。
- `ci.yml` 不监听 tag；release 的"先跑全量测试"是唯一门禁。
- docs links job 跳过 `http/https` 外链，坏链只能靠人工。
- `drift guard` 依赖外部仓库的 `.ngm-version`（`check-release-status.sh`），
  外部仓库不可达时门禁行为需要明确（当前是 exit 3"查不动"）。
- `backfill.yml` 依赖 CHANGELOG 段落；`v0.5–v0.8` 缺章节会让
  `release-notes.sh` exit 3——这正是缺口 38 里那 4 个没有被回填的直接原因。

### 5.10 技术债与重复代码

1. **命令骨架重复**：28 条命令里至少 12 条是同一段
   "parse flags → config load → runErr → markJSON"；`gc.go:129` 的注释说明团队
   已经意识到"通道要一致"，但没有抽成一个 `cmdContext`。
2. **"探测 / 执行"双路径**：`Plans` 与 `runChain` 各自调 `Available()`/`Version()`；
   `catalog.Validate` 与 `engines.go` 又各有一套。S0 就是这套双路径的直接产物。
3. **`modules.go` 的 semver 双标准**（`semverLike` vs `rePlanVersion`）。
4. **`cmd/ngm/install.go` 的三个职责**（fresh / from-lock / hook 编排）没有分文件。
5. **`env.go` 的路径逻辑**（项目根、global 根、vendor 根）散在 400+ 行里，
   符号链接语义只有词法判定。

---

## 6. 问题清单

> `S0` 阻塞；`S1` 高；`S2` 中；`S3` 低。ID 与 §8 的任务一一对应。

### S0

| ID | 问题 | 影响 | 证据 |
|---|---|---|---|
| **S0-1** | `--dry-run` / `engines list` / `engines validate` 执行仓库声明的引擎程序，且绕过 `run:<engine>`；探测环境变量全继承 | 克隆一个仓库就能在本机执行任意声明程序；`deny:["run:X"]` 形同虚设；token 可被探测进程读取 | `internal/adapter/runner.go:191-227`、`catalog.go:412-432,515-554`、`internal/adapter/subprocess.go:90-150`、`cmd/ngm/build.go:25`、`cmd/ngm/engines.go:304-315`、`security-model.md` 施加点表 |

### S1

| ID | 问题 | 影响 | 证据 |
|---|---|---|---|
| **S1-1** | `audit --json --hook` 的 hook 失败会把第二个 JSON 文档写到 stdout | CI 解析失败；与 `cli.md:67` 的承诺直接冲突 | `cmd/ngm/audit.go:132-143`、`audit_hook.go:48-49,57-104`、`docs/guides/cli.md:67`、`cmd/ngm/main.go:83-95` |
| **S1-2** | `verify` 后置层用 `runErr` 的返回码覆盖基础判定码（critical 2 被缺 Deno 5 覆盖） | 完整性失败被误报成环境问题；人类摘要与进程返回码不一致 | `cmd/ngm/verify.go:321-323,326-353,359-377,490` |
| **S1-3** | `tree --offline` 在 lock 有图时仍可联网（`ResolveOptions` 无 Offline） | "离线"承诺失效；CI 禁网场景退错码 | `cmd/ngm/tree.go:110-143`、`internal/core/resolve/reftype.go:16-33,230-247` |
| **S1-4** | lock 的 `vendorPath` / `subPath` 不校验路径形状，verify/sandbox 直接拼进 vendor 根 | 恶意 lock 可让校验脚本指到 vendor 外（读取/执行任意路径下的 `verify.js`） | `internal/core/lock/schema.go:277-315`、`internal/core/verify/verify.go:847-849`、`cmd/ngm/verify_sandbox.go:30`、`cmd/ngm/env.go:443` |
| **S1-5** | `adopt` 无法处理 pnpm 符号链接布局，零包 + "re-run your installer" 误导 | pnpm 用户 adopt 恒失败且提示指向错误动作 | `internal/adopt/adopt.go:140-160,255-288` |
| **S1-6** | `verify --require-signed` 在空 lock / 零依赖时 exit 2 | 无依赖项目无法通过安全门槛 | `cmd/ngm/verify_signatures.go:86-93` |

### S2

| ID | 问题 | 影响 | 证据 |
|---|---|---|---|
| **S2-1** | `ContentStore.Has` 只验存在性；损坏 blob 被硬链；损坏 v2 manifest 无法自动重修 | 损坏内容被当作"已存在"，直到 verify 才暴露 | `internal/core/vendor/content.go:125-157`、`content_v2.go:138,313-318`、`cmd/ngm/install.go:409-414` |
| **S2-2** | install hooks 先于写 lock，无回滚；`installFromLock` 传 `previous=nil` | hook 失败会留下"内容已落、lock 未写"的中间态 | `cmd/ngm/install.go:186-192,361-369,528` |
| **S2-3** | `gc --json` 的配置/lock 失败无错误信封；mirror `RemoveAll` 无 age grace / 进程锁 | 与同文件注释的"同一条通道"承诺不符；并发 gc 有删正在使用的 mirror 的风险 | `cmd/ngm/gc.go:129-195` |
| **S2-4** | OSV 无 redirect 策略、无 body 上限、cache 写失败整体失败、`--offline --no-cache` 读 stale | 恶意/故障端点可喂超大响应；离线语义不严 | `internal/supplychain/osv.go:108-150,143-147,127-133,473` |
| **S2-5** | git `buildEnv` 未净化 `GIT_CONFIG_*` / `GIT_SSH_COMMAND` / `GIT_ASKPASS` / `insteadOf` | host 白名单可被父环境绕过 | `internal/core/git/exec.go:158-201` |
| **S2-6** | sandbox 继承未 deny 的全部环境、未显式 deny `ffi/sys/hrtime/node`、`--allow-read` 逗号拼接 | 沙箱边界弱于文档描述 | `internal/security/sandbox.go:96-160,287` |
| **S2-7** | LFS 指针 > 1 KiB 当普通文件哈希 | 违反 ADR-008"必须报错" | `internal/core/digest/lfs.go:49-53` |
| **S2-8** | `integrations` 在 `ngm.json` 读失败时跳过 `EscapesVendor` 守卫 | 配置损坏时少一道路径守卫 | `cmd/ngm/integrations.go:90-110` |
| **S2-9** | adapter `Available()` 用进程 CWD、引擎 PATH 不注入项目 `.bin`、产物只判存在、options 可覆盖 `--outfile` | 探测与执行不一致；产物可能是旧文件 | `internal/adapter/subprocess.go`、`engines_extra.go:28-75` |
| **S2-10** | tsc 的 TypeDecl 注释说 `--emitDeclarationOnly`，实际只传 `--declaration` | 会额外产出 JS，污染产物目录 | `internal/adapter/engines_extra.go:55-67` |
| **S2-11** | `modules validate` 与 `plan` 的 semver 双标准；origin URL 原样打印 | 合法预发布版本无法 plan；token 可能泄漏 | `cmd/ngm/modules.go:164-165,487-491,811-812` |
| **S2-12** | `outdated` 单个 tag 检查失败中止整份报告 | 与"could not check → unknown"契约不符 | `internal/observability/outdated.go:88-112` |
| **S2-13** | README / docs/README / installation 的发布读数与 `release-pipeline.md` 冲突 | 用户按旧读数找二进制会找不到 | `README.md:38-48`、`docs/README.md:8-21`、`docs/guides/installation.md:24-56`、`docs/guides/release-pipeline.md:9-50` |
| **S2-14** | CI 无 `timeout-minutes`、npm 引擎未钉版本、release tag 校验只 `case v*`、release 测试仅 Ubuntu、ci 不监听 tag | 卡死/上游漂移/非法 tag 都能通过 | `.github/workflows/ci.yml`、`release.yml:60-63`、`backfill.yml` |
| **S2-15** | 构建依赖 Gitee 私有模块 `gitee.com/idcu-go/modproof` | 无 Gitee 凭据/网络时无法构建 | `go.mod`、`go.sum` |
| **S2-16** | `verify` 包覆盖率 34.7% | 判定核心的回归成本高 | `go test -cover ./internal/core/verify` |
| **S2-17** | 867 个 Test 中没有"dry-run 不得 spawn"的负向断言 | S0 能长期存在 | 全量 `_test.go` 检索 |

### S3

| ID | 问题 | 证据 |
|---|---|---|
| S3-1 | SARIF 的 `sarifURI` 缺 `ngm.vendor/` 前缀；fixture 自带前缀掩盖问题 | `internal/core/verify/sarif.go:245-275`、`sarif_test.go` |
| S3-2 | GitHub 注解转义缺 `:` / `,` | `internal/core/verify/github.go:65-76` |
| S3-3 | `ngm run --json <script>` 被当 list | `cmd/ngm/run.go` |
| S3-4 | `--` 后参数 "verbatim" 与 shell owns semantics 的措辞冲突 | `cmd/ngm/run.go` usage |
| S3-5 | lock commit 强制 40 hex，而 `subtreeOid` 支持 64 hex（算法不对称） | `internal/core/lock/schema.go` |
| S3-6 | `errs` 包注释漏 6 号码 | `internal/core/errs` |
| S3-7 | `contracts/exit-codes.json` 指向不存在的测试名 | `contracts/exit-codes.json`、`errs` 测试 |
| S3-8 | `cli.md` 缺 `--dir` | `docs/guides/cli.md` |
| S3-9 | `profiles`/`probeVersion` 缓存键用 `Program+Args` 拼接可碰撞 | `cmd/ngm/engines.go:275` |
| S3-10 | `atomicfile` 把所有 I/O 失败映射为 `CodeConfigInvalid`(3) | `internal/core/atomicfile/atomicfile.go:35-90` |

---

## 7. 上一轮问题的回归核对

| 上轮结论 | 本轮核对 |
|---|---|
| gitlink（mode 160000）被拒 | **已修复**：tree / manifest / link 三层都有显式拒绝，有测试 |
| install 增加 root 级失配判定 | **已修复**：`lockManifestMismatch` 覆盖 `requiredBy(root)` 条目 |
| 空 `RequiredBy` 的旧 lock 不检测 | **仍存在**，但属文档化的"图未知"降级，维持 S3/已知限制 |
| `remove` 后 install 会重装陈旧依赖 | **降级**：这是 frozen 语义，文档已写，不是缺陷 |
| SBOM module 组件 hash 恒为 archiveDigest | **不是缺陷**：`lock.ContentDigest()` 返回 moduleDigest |
| `verify --json` 也双 JSON | **不是缺陷**：`extraOut=stderr` 传给了 `verifyWithSandbox`；只有 audit 真双文档 |
| `modules` 漏 changed/plan | **不是缺陷**：`cli.md` 行内已包含 |
| `doctor` 恒 exit 0 | **不是缺陷**：usage 明确 diagnostic |

---

## 8. 下一步开发计划

> 工作量：**S** ≤ 0.5 人日，**M** 1–2 人日，**L** 3–5 人日。
> 每项都给出"验证方式"——没有判据的项不进入批次。

### 8.1 P0（必修，阻断发布）

#### P0-1 把"探测"纳入权限闸门，并让 `--dry-run` 真的不执行

- **目标**：任何 spawn 引擎的路径都经过 `run:<engine>`；`--dry-run` 只渲染命令，
  不 spawn；探测使用与执行同一份 `deniedEnv`。
- **改动范围**：
  - `internal/adapter/runner.go`：`Plans` 增加 `probe bool`（或拆成
    `PlansDry` / `Plans`），dry-run 传 false；`Available()`/`Version()` 调用点
    统一收进一个 `probeEngine` 函数，函数内先判定策略。
  - `internal/adapter/catalog.go:412-432,515-554`：`Validate` 传入 policy 与
    `deniedEnv`，判定失败返回 `CodePermission`(3) 而不是"跳过"。
  - `internal/adapter/subprocess.go`：`Available()` 增加 `dir` 参数，用
    `<dir>/.bin` + 进程 PATH 的合并解析；`Version()` 使用 `DeniedEnv`。
  - `cmd/ngm/engines.go:275-315`：缓存键改为 `Program + "\x00" + Join(Args) +
    "\x00" + resolvedPath`；探测前调策略判定。
  - `cmd/ngm/build.go:25`：usage 改成"render only, never spawn"。
- **验证**：
  1. 新负向测试 `TestPlansDryRunSpawnsNothing`（子进程计数器 + 探针脚本，
     `-run` 必须真跑，不允许 `t.Skip`）。
  2. `TestEnginesListHonorsRunDeny`：`deny:["run:probe"]` → exit 3 且探针零命中。
  3. 现有 `TestSpawn_OnlyOneFileBuildsProcesses` 仍绿。
- **依赖**：无。**工作量 M。**

> 本项与 P0-2 无代码耦合，但两者都改 `cmd/ngm` 的输出路径，建议同一批次提交
> 但分开 review。

#### P0-2 修对外输出契约：audit 单文档、verify 单一退出码裁决、gc JSON 信封

- **目标**：`--json` 时 stdout 恰好一份 JSON；退出码由单一裁决点产生。
- **改动范围**：
  - `cmd/ngm/audit_hook.go:48-104`：hook 失败改为把错误信封写 `out`（jsonMode 时
    是 stderr），或返回 `(code, error)` 由 `audit.go` 在已写报告之外只更新退出码。
  - `cmd/ngm/verify.go:326-377`：`finishVerify` 改为"取最大严重度"而非"后写覆盖"：
    `2` 恒高于 `5`；`--json` 的 `summary.exitCode` 与进程返回码同源。
  - `cmd/ngm/verify.go:490`：摘要打印合成码。
  - `cmd/ngm/gc.go:129-195`：所有 `runErr` 前确保 `markJSONIfRequested` 已生效
    （或在 `runErr` 内按 `--json` 统一决定信封目标）。
  - `internal/core/errs`：增加 `MergeCodes(base, extra int) int`，明确优先级
    （2 > 3 > 5 > 4 > 6 > 1 > 0，或按现有语义固定为"2 不可被覆盖"）。
- **验证**：
  1. `TestAuditJSONHookIsSingleDocument`：解析 stdout，断言只有一个顶层对象。
  2. `TestVerifyCriticalBeatsMissingDeno`：critical + 无 Deno → 2，且
     `summary.exitCode == 2`。
  3. `TestGCJSONConfigFailureEnvelope`：`--dir=<不存在>` → stdout 一份信封、exit 3。
- **依赖**：无。**工作量 M。**

### 8.2 P1（该做）

#### P1-1 离线语义贯穿解析层

- **目标**：`--offline` 在 `tree`（lock 有图分支）与所有 `ResolveRef` 调用上生效。
- **改动范围**：`resolve.ResolveOptions` 增加 `Offline bool`；
  `pickLSRemoteURL` 在 offline 且 mirror 缺失时返回错误（`CodeResourceMissing` 4），
  不回退 `CloneURL`；`cmd/ngm/tree.go:127-132` 传 `Offline: *offline`；
  `internal/core/resolve/graph.go` 的嵌套调用一并传递。
- **验证**：`TestTreeOfflineColdMirrorExits4`（真 git 夹具，禁网），
  以及"offline 下 `CloneURL` 零调用"的断言。
- **依赖**：无。**工作量 S。**

#### P1-2 lock 路径字段白名单校验

- **目标**：`vendorPath` / `subPath` 只能是相对、无 `..`、无盘符、无 NUL 的
  slash 路径；读取端再做一次 `filepath.Rel` 越界检查。
- **改动范围**：`internal/core/lock/schema.go:277-315`；
  `internal/core/verify/verify.go:847`；`cmd/ngm/verify_sandbox.go:30`；
  `cmd/ngm/env.go:285-320` 加 `EvalSymlinks` 后比较。
- **验证**：表驱动用例（`..` / `/abs` / `C:\` / `a/../../b` / 空 / NUL），
  以及 `TestVerifySandboxRejectsEscapingVendorPath`。
- **依赖**：与 P1-4、P1-5 有耦合（都会改 verify 的路径构造）。**工作量 S。**

#### P1-3 git 子进程环境净化

- **目标**：`buildEnv` 剔除 `GIT_CONFIG_*` / `GIT_SSH_COMMAND` / `GIT_ASKPASS` /
  `GIT_EXTERNAL_DIFF` / `GIT_PAGER`；`CloneMirror` 对 scheme 做白名单。
- **改动范围**：`internal/core/git/exec.go:158-201`、`clone.go`、`remoteurl.go`。
- **验证**：`TestBuildEnvDropsGitConfigOverrides`、`TestCloneRejectsNonGitScheme`。
- **依赖**：无。**工作量 S。**

#### P1-4 adopt 支持 pnpm 布局

- **目标**：识别 `.pnpm/<name>@<version>/node_modules/<name>` 的真实目录；
  顶层 symlink 解析到真实目录后按其内容证明；零包不再提示"重跑安装器"。
- **改动范围**：`internal/adopt/adopt.go:140-160,255-288`：`EvalSymlinks` +
  `.pnpm` 识别；错误提示改为"未发现可证明的包（pnpm 布局请…）"。
- **验证**：pnpm 夹具（`node_modules/.pnpm/...` + 顶层 symlink），断言包数 > 0
  且 digest 稳定。
- **依赖**：无。**工作量 M。**

#### P1-5 存储层"命中"必须可证明

- **目标**：`Has` 至少校验 size 与 manifest 记录的 blob 数；损坏 manifest 可自修；
  install 在硬链前对 `Has` 命中的条目做一次廉价校验。
- **改动范围**：`vendor/content.go:125-157`、`content_v2.go:313-318`、
  `cmd/ngm/install.go:409-414`。
- **验证**：删除一个 blob / 截断 manifest 后 `install` 必须重修而不是硬链；
  `TestHasDetectsMissingBlob` 已有，补 `TestHasDetectsWrongSize`。
- **依赖**：P0-2（错误码）之后。**工作量 M。**

#### P1-6 install 顺序与回滚

- **目标**：hooks 失败不得留下半写状态；`installFromLock` 传入 previous lock。
- **改动范围**：`cmd/ngm/install.go:186-192,361-369,528`：先写 tmp lock →
  materialize → hooks → rename lock；或在 hooks 失败时清理本次新建的 vendor 条目。
- **验证**：hook 失败注入 → vendor 恢复到调用前状态；lock 未变。
- **依赖**：P1-2（路径校验）。**工作量 M。**

#### P1-7 文档读数统一

- **目标**：README / `docs/README.md` / `installation.md` 的发布读数与
  `release-pipeline.md` 同源；去掉"差的 0 个是未推送的"。
- **改动范围**：三份文档 + `scripts/check-docs-links.sh` 增加"读数一致性"检查
  （或加一个 `scripts/check-release-readings.sh` 判据）。
- **验证**：判据脚本在 CI 里跑；手工核对四个数字（tag / release / 缺口 / 最新）。
- **依赖**：无。**工作量 S。**

### 8.3 P2（可以做）

| ID | 目标 | 改动范围 | 验证 | 工作量 |
|---|---|---|---|---|
| P2-1 | CI 加固 | 4 个 workflow 加 `timeout-minutes`；npm 引擎钉版本；release tag 用正则；release 测试矩阵补 macOS/Windows；ci 监听 tag | workflow lint + 一次 dry-run | S |
| P2-2 | 探测/执行单一路径 | 合并 `Plans` / `runChain` / `catalog.Validate` / `engines` 四处的 probe | 子进程计数断言 | M |
| P2-3 | 命令骨架抽取 | `cmdContext`（flags/config/JSON/exit）替换 12 条命令的重复段 | 现有 CLI 测试全绿 | L |
| P2-4 | modules semver 统一 | `semverLike` 与 `rePlanVersion` 共用解析器；origin URL 走统一脱敏 | 表驱动 | S |
| P2-5 | OSV 硬化 | `CheckRedirect` 限同源、`LimitReader`、cache 写失败降级为 warning、`--offline --no-cache` 不读 stale | 端点替身 | S |
| P2-6 | 沙箱补 deny | 显式 deny `ffi/sys/hrtime/node`；`--allow-read` 用重复 flag 而非逗号拼接；最小环境变量集 | argv 断言 + 真 Deno | M |
| P2-7 | SARIF/注解修正 | 补 `ngm.vendor/` 前缀；`ghEscape` 补 `:`/`,`；fixture 去掉自带前缀 | 快照测试 | S |
| P2-8 | 测试基建 | 覆盖率门槛（verify ≥ 60%；实际落地为跨包口径 ≥ 80%，见 [tasks.md](../specs/assessment-2026-11-07-followup/tasks.md)）；3 个 fuzz（digest 清单 / lock schema / ghEscape）；审计 59 个 skip，禁止跳过边界类 | `go test -fuzz` 短跑 + CI 门槛 | M |
| P2-9 | 性能 | cat-file 流式化；`Has` 结果缓存（带失效信号）；大依赖树基准进 CI | benchmark 对比 | L |
| P2-10 | 构建可离线性 | 评估 `modproof` 的 vendoring 或镜像；在 CI 里加"无 Gitee 凭据构建"探测 | 冷构建 | M |

### 8.4 批次与耦合

| 批次 | 内容 | 前置 | 理由 |
|---|---|---|---|
| **A** | P0-1 | — | 安全闸门，必须先合入 |
| **B** | P0-2 | — | 与 A 无代码耦合，可并行 |
| **C** | P1-1 / P1-2 / P1-3 / P1-4 | B（错误码语义） | 三个"路径 / 离线 / 环境"都是边界类，一起补测试夹具 |
| **D** | P1-5 / P1-6 | C（P1-2） | install 的回滚测试依赖 lock 路径已可信 |
| **E** | P1-7 / P2-1 / P2-7 | — | 文档与 CI 可随时做 |
| **F** | P2-2 / P2-3 / P2-4 / P2-6 | A | 需要 A 的 probe 统一作为基础 |
| **G** | P2-5 / P2-8 / P2-9 / P2-10 | 各自独立 | — |

**耦合说明**：

- P1-2（lock 路径）→ P1-5（Has）→ P1-6（install 顺序）是同一条"信任根"链，
  必须按序。
- P0-1 与 P2-2 有重叠：P0-1 是止血（加闸门），P2-2 是重构（合并路径）。
  **不要合并做**，否则 review 面积过大、闸门缺陷会被重构掩盖。
- P1-1（ResolveOptions 加 Offline）会触及 `resolve` 的所有调用点，
  与 P2-3（命令骨架）有文件级冲突，建议 P1-1 先合。

---

## 9. 附录 A：文件覆盖清单

### 9.1 完整读过

**入口与命令**（`cmd/ngm`）：

`main.go`、`root.go`、`build.go`、`engines.go`、`tree.go`、`audit.go`、
`audit_hook.go`、`verify.go`、`verify_sandbox.go`、`verify_signatures.go`、
`install.go`、`remove.go`、`adopt.go`、`gc.go`、`update.go`、`run.go`、`doctor.go`、
`env.go`（关键段）。

**核心包**（`internal`）：

- `core/resolve/reftype.go`（关键段）、`core/resolve/graph.go`（抽样）
- `core/verify/verify.go`、`core/verify/sarif.go`（关键段）、`core/verify/github.go`（关键段）
- `core/lock/schema.go`（关键段）
- `core/vendor/content.go`（关键段）、`content_v2.go`（关键段）、`links.go`（关键段）
- `core/git/{exec,spawn,clone,lsremote,commit,tree,blob,catfile,auth,remoteurl,ancestry,runallow,signature,gc}.go`
- `core/digest/{manifest,validate,lfs}.go`
- `core/atomicfile/atomicfile.go`（关键段）
- `adapter/runner.go`（关键段）、`catalog.go`（关键段）、`subprocess.go`（关键段）、
  `engines_extra.go`（关键段）
- `security/{permissions,sandbox}.go`
- `supplychain/osv.go`（关键段）
- `observability/outdated.go`（关键段）
- `runscript/{runscript,exec,exitcode_unix,exitcode_windows}.go`
- `adopt/adopt.go`（关键段）

**配置 / 文档 / 构建**：

`go.mod`、`contracts/exit-codes.json`、`README.md`、`CHANGELOG.md`、
`docs/README.md`、`docs/guides/cli.md`、`docs/guides/installation.md`、
`docs/guides/release-pipeline.md`、`docs/architecture/security-model.md`、
`.github/workflows/ci.yml`（抽样）、`release.yml`（抽样）、`backfill.yml`（抽样）。

### 9.2 只抽样

- `cmd/ngm/modules.go`（约 1–230 / 460–540 / 790–830 / 1183 附近）
- `cmd/ngm/integrations.go`（90–150）
- `cmd/ngm/sbom.go` / `attest.go`（按被调用的导出函数抽样）
- `internal/core/resolve/graph.go`（只看离线路径）
- `internal/config/*`（只看 `ProjectFile.Validate` 与 schemaVersion 1..2）
- `internal/observability/tree.go`（预算与截断）
- `internal/attest/*`、`internal/sbom/*`（导出形状与 purl 分流）
- 约 30 个 `_test.go`：`run_test.go`、`runscript_test.go`、`adapter_test.go`、
  `waitdelay_test.go`、`early_failure_envelope_test.go`、
  `permissions_acceptance_test.go`、`v102_gc_test.go`、`v107_test.go`、
  `sarif_test.go`、`archive_test.go`、`adopt/*_test.go` 等。

### 9.3 未覆盖

- 所有 `scripts/*.ps1` 的逐行实现（只读入口与调用关系）
- Gitee / GitHub 远端 release 的资产（无凭据、无 `gh`，只做静态核对）
- 真实 Deno 跨版本矩阵（本机只有单版本）

---

## 10. 附录 B：评估局限

1. 发布侧只做了静态核对与文档读数比对；**没有**实际下载 release 资产验证
   `SHA256SUMS`。`release-pipeline.md` 的读数被当作权威，因为它给出了可复核的
   API 来源与 fixture 判据。
2. 黑盒复现只针对 S0-1 与 `gc --json` 的失败信封；其余结论来自代码阅读 +
   调用链，未逐条构造运行时证据。
3. 覆盖率是单次 `go test -cover ./internal/...` 的结果，未做多次采样；
   `cmd/ngm` 的覆盖率因运行时长（560s）未纳入。
4. `t.Skip` 的 59 处只做了分类抽样，未逐条判断"该不该 skip"。
