# Changelog

本文件是**发布说明的来源**：`release.yml` 只从对应版本的段落取文本，不再让
`--generate-notes` 按提交标题猜。段落标题必须写成 `## [<version>]`，`<version>`
是不带 `v` 的版本号（与 `ngm --version` 的第二列一致）。

只记录**用户可观察的行为**；逐版工程细节见 `docs/development/` 的计划与复盘。

## [Unreleased] - 2026-11-04

### Added

- `ngm run <script> [--if-present] [-- <args>]` / `ngm run --list [--json]`：不加 npm
  也能跑项目脚本。`package.json.scripts` 是权威来源，`ngm.json.scripts` 是回退来源；
  同名脚本同时出现在两处是配置错误（exit 3，报错点名两处），不静默覆盖。
  pre/post 按 npm 约定（`pre<name>` / `post<name>`），首个失败步即停并返回它的码。
  POSIX 用 `sh -c`、Windows 用 `cmd.exe /d /s /c`，不做二次引号；项目目录及祖先的
  `node_modules/.bin` 注入子进程 PATH 前端。环境变量只承诺最小集（`INIT_CWD`、
  `npm_lifecycle_event`、`npm_lifecycle_script`、`npm_package_name`、
  `npm_package_version`），**不设** `npm_execpath`、不承诺 `npm_config_*`。
  退出码原样透传（信号终止见 128+signal），不映射到 ngm 的 0–6。
  单一权限闸门 `run:script`（默认拒绝，提示里给可直接粘贴的 JSON），
  **与 install 期 `supplyChain.postInstallPolicy` 是两套闸门**（ADR-042）。
  项目脚本在 ngm 沙箱外执行；`run` 不接进 install / verify / CI 自动流程。
- `ngm.json` 新增可选字段 `scripts`（`ngm run` 的回退来源）。
- `ngm run` 的 `--list` 人类输出与 `--json` 报告（`version` · `project` · `scripts`，
  元素含 `name` / `command` / `source` / `pre` / `post`）。
- `ngm verify --content`（等价写法 `--level=content`；`--deep` 保留为别名）：重放
  lock 的 `archiveDigest` / `moduleDigest`，并逐文件比对 vendor 与 content store 的
  字节。报告给出依赖、文件路径与期望/实际哈希（终端截断到 12 位，`--json` 里是完整
  sha256）。资源缺失（mirror/store 不全、无法重算）仍退 **4**，只有确证内容不符才退
  **2**。默认仍是 structure，既有 CI 的成本不变。
- `ngm gc [--apply] [--json]`：按**当前项目的 lock** 回收 store / mirror / vendor 里
  不可达的对象。**默认 dry-run**，只有显式 `--apply` 才删除；输出按层分类给出候选
  数与可回收字节。global vendor 条目无法判定是否被其它项目引用，一律保留并 warning；
  逐对象删除失败汇总为 exit 4，不中途退出。
- `action.yml` 新增 `verify-level` 输入（默认 `structure`）；`args` 里已经写了级别
  flag 时不再注入默认值。
- `ngm doctor` 新增 `vendor` 读数：`present` / `ignored-by-git` / `absent`。**只读、
  不改变退出码语义**——它回答的是"配置意图与 Git 状态是否错配"（打算随仓库分发、却被
  `.gitignore` 排除），而那种错配在冷克隆里表现为 `install --offline` 的 exit 4，
  错误信息却指向"缺内容"。不执行任何 Git 写操作。
- `scripts/check-docs-links.sh` 新增**本机绝对路径告警**：`.md` 里出现盘符路径或
  `/home/<name>/`、`/Users/<name>/`、`/root/` 时打印 `LOCAL PATH in …` 并计数，
  **不阻断**（引用外部环境有时是必要的，但它对任何其它机器都不可复现）。
  `https://` 前缀不会误命中；本机路径作为链接目标时也不再被判成坏链。

### Changed

- `ngm transform` 建输出目录失败的提示不再指向**不存在的** `--outdir`，改为指向
  命令真正接受的 `--outfile`。
- `ngm init`（`ngm.json` 与入口文件）、`ngm transform --outfile`、`adapter=wasm` 的
  `--outfile` 改为**原子写**（与 `sbom` / `attest` 的 `--out`、`ngm.json` / `ngm.lock`
  走同一条实现）：中断或磁盘满不会再留下一个看着像成功产物的截断文件；写出的
  文件权限固定为 0644（`--force` 覆盖一个 0600 的 `ngm.json` 也会变成 0644）。
  目标是符号链接时替换的是链接本身而非它指向的文件——这是"产物"的语义。
- `ngm run --json --bogus` 现在给错误信封（此前 stdout 为空）：`run` 是唯一手写骨架，
  解析失败发生在 `--json` 生效之前；同一条命令的另一种早期失败（空 `--dir`）却给信封。
- 不接受 `--json` 的命令（`add` / `sbom` / `attest` / `init` 等）在**分流层**被拒时
  （如 `--dir=`）现在也不再往 stdout 写信封——此前这里与命令自己的解析失败不一致。
- `ngm gc --json` 的字节与其余报告对齐（HTML 转义，Go 默认）：**解码后不变**。
  ADR-045 把 `--json` 的契约定在**解码语义**上，字节不是契约；唯一按字节比对的是
  `sbom` / `attest` 的产物（它们保持不转义，理由在那两处注释里）。
- `ngm run` 不再把父环境里的 `npm_config_*` 透传给脚本（此前只保证"不设置"）：
  否则同一份脚本从 shell 启动与从 npm 脚本启动会因为这段看不见的环境而行为不同。
  与 [ADR-044](docs/adr/adr-044-independent-ecosystem.md) 一致——ngm 不是兼容层，
  没有理由继承 npm 的配置命名空间；需要 npm 自己的变量的脚本应当用 npm。
- `ngm run` 的子进程环境现在也遵守 `permissions.deny: ["env:<NAME>"]`：被拒绝的
  变量不再出现在脚本里。此前这条策略只施加于 git 子进程、Deno 沙箱与引擎子进程，
  而脚本是 ngm 启动的**最大**一类子进程——同一条 deny 在它身上无声失效。
  治理范围是**透传**那一段：ngm 自己的 lifecycle 契约变量（`PATH`、`INIT_CWD`、
  `npm_lifecycle_*`、`npm_package_*`）照旧注入，因此 `deny: ["env:PATH"]` 不会让
  脚本失去 PATH（那不是有意义的拒绝）。安全模型的施加点表与 `ngm run --help`
  同步更新。
- `ngm.lock` 的路径校验：`vendorPath` 拒绝 scheme / scp / slug 形状（`https://…`、
  `ssh://…`、`git@host:org/repo`、`github:org/repo`）与盘符（`C:` 前缀；盘符此前
  就已拒绝），判据是"冒号落在第一段"或含 `//`。**`subPath` 可以含冒号**：`dir:name`
  只是相对路径分量，拼进 `filepath.Join` 仍在 vendor 根之内；由它派生的
  `vendorPath`（如 `github.com/acme/a/dir:name`）冒号在后续段，同样放行。
  兼容性影响：正常 `ngm install` / `ngm update` 生成的 lock 不受影响；历史 lock
  中 `subPath` 含冒号（仅 POSIX 仓库可能出现这种目录名）仍可验证；仍会 exit 3 的
  只有手写或篡改的 slug / scp / URL / 盘符形状。
- `NGM_HOME` 现在**真的**决定全局配置的位置：设置后全局配置读
  `<NGM_HOME>/config.json`（与 mirror / content store / global vendor 同一根）。
  此前只有目录布局跟随 `NGM_HOME`，全局配置仍固定读 `<home>/.ngm/config.json`——
  两条路径不同源，用户以为隔离了、实际没有。权限提示里的路径也同步指向生效的文件。
- `store usage` / `store prune` 的说明更新：自动/全局的可达性删除仍然没有，显式、
  项目范围的那类由 `ngm gc` 提供（ADR-041）。
- `verify` 的结构级汇总提示改为指向 `--content`（`--deep` 仍可用）。
- `ngm mappings validate` 的两条失败提示补充"生成后要提交"：mappings 是 `ngm.lock`
  的确定性投影，只修本机不提交，冷克隆的 CI 会在同一处再失败一次。
- 文档：`docs/practice/pivoise-methodology.md` 重写为通用的
  [工程纪律方法论](./docs/practice/engineering-discipline.md)（枢衡降为一个样本）；
  `observability.md` 补上一直缺失的 `ngm doctor` 小节；
  `vendor-layers.md` 增"层的下游：二次分发与证明边界"；
  `configuration.md` 给 `ngm.mappings.json` 补上"必须提交 + 理由"。

### Fixed

- `ngm install` 在 **copy** 落地模式下覆盖既有 vendor 文件时，现在会应用清单里的
  权限位（此前 `os.OpenFile` 的 perm 只在创建时生效：上游把 0644 改成 0755 后
  重新 install，内容更新了、可执行位仍是旧的）；落盘改为临时文件 + rename，
  中途中断不再把旧文件截断成半个。
- `ngm modules list|validate|changed|plan|index` 在 `--json` 下**失败**时现在也给
  错误信封（此前无论参数解析失败还是运行期错误，stdout 都是空的，只有人读文本落在
  stderr）——它们早就声称支持 `--json`，却在最该给机器读的那条路上什么都不给。
  其余不接受 `--json` 的命令（`add` / `sbom` / `attest` / `remove` 等）保持
  stdout 为空，这是 v0.16 规则④，未被这次统一改动波及。
- **安全（S0）**：`~/.ngm/config.json` 完全不存在时，引擎探测闸门此前不注册，
  `ngm engines list` / `engines validate` 会真实 spawn 被克隆仓库
  `ngm.engines.json` 声明的程序（环境变量全继承）。现在探测闸门无条件注册，
  nil Policy 按默认档位拒绝 `run:<engine>`。新增零配置"探针零命中"负向判据。
- `verify --content` 现在做 store 清单的**反向集合**校验：多出一条不在锁定 Git
  tree 里的条目会判 critical(2)（此前重放只按 tree 正向取值，注入的文件会被
  materialize 落地而校验全绿）。模块条目的形状由 `moduleDigest` 绑定，而那个 digest
  是**按 store 清单重放**的，所以多出的条目会改变重放结果、同样判 critical(2)
  （kind 是 `module-digest`；v0.135 补了端到端判据，免得这条推理只活在注释里）。
- git 子进程的 `GIT_EXEC_PATH` / `LD_PRELOAD` / `DYLD_INSERT_LIBRARIES` 现在与
  `GIT_SSH_COMMAND` 同等待遇被剔除；`ExtraEnv` 也走同一套 deny/hijack 过滤，
  "追加"不再能绕过权限。
- `ngm doctor --json` 在位置参数错误时现在也返回机器可读错误信封（此前 stdout
  为空）。`contracts/exit-codes.json` 修正了点错的守门测试名，并新增一条
  "契约点名的测试真实存在"判据。
- 版本解析收进 `internal/version`：`outdated` 的严格 semver 与引擎版本门槛的
  宽松解析并列保留、各有测试，不再两处各写一份正则。
- `ngm adopt` 现在能从 `pnpm-lock.yaml` / `yarn.lock` 里提取 **Git 来源**的
  依赖（url + commit）写进 provenance；registry 条目仍按设计保持 unknown，
  notes 会说明覆盖范围。不引入 YAML 依赖，只做窄形状解析。这条边界补记在
  [ADR-040](docs/adr/adr-040-adopt-proof-layer.md) 的补记里——“不为 registry 条目
  解析”这条非目标没变，补的是“Git 来源的条目本来就是 ngm 的轴”。
- `ngm gc` 现在覆盖 module cache（层 1.5）：与 mirror 同一可达性判据，
  默认 dry-run，删除失败汇总为 exit 4。`--json` 报告相应多出 `moduleCache` 段
  （与 `store` / `mirror` / `vendor` 同形状），`docs/guides/cli.md` 的形状表已补。
- vendored 依赖的 `.bin` shim 按 [ADR-043](docs/adr/adr-043-vendored-bin-shim.md)
  决定暂缓：它越过 ADR-042 的边界并从第三方内容创建可执行面，需先有威胁模型。
- `ngm adopt`：缺少 `package.json` 的**作用域包**（`node_modules/@scope/name`）
  现在回退为 `@scope/name`，不再丢掉 scope（此前只剩 `name`）。
- README 不再硬编码 tag 数量（此前写 101，实测已漂移）；改为“以 `git tag` 读数为准”。

### Notes

- `ContentStore.Has` 增加 benchmark（`BenchmarkContentStoreHas`）。实测 20 / 200 /
  2000 文件的单次代价为 0.93ms / 8.25ms / 77.1ms（本机 Windows + NTFS，全部命中页
  缓存）。本轮**不引入缓存**：没有能在 blob 被外部删除时可靠失效的廉价信号。

## [0.102.0-dev.2] - 2026-11-03

开发版本。它与 `0.102.0-dev.1` 是同一批功能，差别只在**发布链路本身**：
dev.1 的发布 run 在当时的 CI 环境上失败，而 tag 指向的提交不可变；这一版把
发布前必须先绿的测试与本项目自己的跨平台判据修好，作为可下载的固定点。

### Fixed

- 预发布 tag（`vX.Y.Z-<suffix>`）不再被 tag ⇄ 复盘门禁要求配对复盘；
  附注 tag 与 `--published`（真的发布）仍然适用。
- `release-dryrun.sh` / `release-notes.sh` 增加 Windows 入口；dry-run 进入 CI
  （ubuntu 跑 `.sh`，windows 跑 `.ps1`），每次推送都真构建、真校验和、真读版本。
- 十个 `.ps1` 入口在 POSIX 上不再因 `Join-Path $null` 直接失败；macOS 自带
  bash 3.2 与 BSD `sort` 的兼容问题修掉（`globstar` → `find`，`sort -V` → 数值排序）。
- `go test` 在 Windows 显式走 bash：默认 PowerShell 下 `set -o pipefail` 是
  找不到的命令，测试根本没跑却被记成失败。
- 反斜杠形态的 symlink 目标在所有平台都按分隔符判定；夹具的
  `core.quotePath` 固定为 false，非 ASCII 文件名不再是平台相关的读数。
- 回填工作流只做编译冒烟（历史 tag 当年已过 CI），发布前仍强制校验
  `SHA256SUMS`、版本注入与版本化别名。

### Notes

- 其余历史 tag 不回填，见 `docs/guides/release-pipeline.md` 的范围与理由。

## [0.102.0-dev.1] - 2026-11-03

开发版本，供 `ngm-repo` 的集合校验固定使用（`.ngm-version`）。

### Added

- `ngm verify` 的报告新增 `notes`：选择性 verify 裁剪 `requiredBy` 时，
  人读与 `--json` 都会说明丢掉了多少条边。
- `ngm tree` 优先读取 `ngm.lock` 的 `requiredBy` 图，并在输出与 `--json` 的
  `source` 里标明来源；旧 lock 才回退重新解析。
- 发布流水线：`v*` tag 触发一次构建，产出六个平台 × 两个架构的二进制、
  版本化别名与 `SHA256SUMS`；发布说明来自本文件的对应段落。

### Changed

- `ngm.json` / `ngm.engines.json` / `~/.ngm/config.json` 的读取器接受
  `schemaVersion` 1..2（写入仍为 1）；`ngm modules validate` 复用
  `ProjectFile.Validate`，生产方与消费方使用同一套判据。
- 默认 Git 协议统一为 `ssh`，与内置默认和 `ngm config show` 一致。
- SBOM / attestation 的 purl 按 host 分流为 `pkg:github` / `pkg:gitlab` /
  `pkg:gitee`，自建 host 使用 `pkg:generic?vcs_url=...`；CycloneDX 的
  `bom-ref` 保留稳定的旧形态。
- 资源缺失（未安装、冷 mirror、content store 缺条目）在 `verify` 中记为
  operational，退出码 4；只有已确证的内容不符才是 critical（退出码 2）。

### Fixed

- 选择性 `ngm verify <dep>` 不再对子集外的父节点报“幽灵边”。
- 重复根声明在 `config validate` 与解析阶段都被拒绝，并点名两条声明。
- `verify --json` 的 `summary` 不再出现 `critical: 0` 而 `exitCode: 2` 的自相矛盾。
- `tree`、`typecheck` 的帮助文本与实现对齐；文档中的 lock 版本、`--json`
  命令数、Go 版本、Action 退出码列表同步更新。
- 9 项输入/边界硬化：cat-file 对象上限、`ContentStore.Has` 验 blob、
  `css --outfile`、`transform` stdin 上限、严重度顺序、literal pathspec、
  integrations 临时文件、`modules index` 当前分支、`collectSource` 警告。

## [0.101.0] - 2026-10-30

### Changed

- 把三条对外承诺实测为**不成立**，并把它们写成可判别的状态，而不是继续
  用一句更强的结论掩盖：沙箱的模块加载、依赖对沙箱配置的影响、安装时的
  大小写冲突。
- 五个“输出正常、结论错”的缺陷被修正：`attest` 的双读、Git 签名把执行失败
  说成“不可信”、`mappings` 入口可逃出 vendor、`why` 的搜索上界、`atomicfile`
  的目录同步。
- `verify` 把“权限被拒”归为 operational，而不是 `unexpected` 漂移。
- 新增三个可匿名访问的 Gitee 夹具，并把生成器固定为确定性输出。

## [0.100.0] - 2026-10-21

### Added

- `ngm store usage` 报告层 1（mirror）：总体积、packed history、残骸，并点名
  “一个 ref 都没有”的目录。
- `ngm store gc [--dry-run]`：对每个可用 mirror 执行 `git gc`，不触网、不删内容。
- `store prune` 追加层 1 的临时包清理，年龄门槛 1 小时。

### Changed

- ADR-030 把“mirror 仍是全量历史”写成决定，并拒绝 `--single-branch` /
  部分克隆，理由是 refType 解析与离线 digest 重放。

## [0.99.0] - 2026-10-14

### Added

- 承诺账本：把仓库里 14 处“必须一致 / 逐字镜像 / 两份实现”一类承诺逐条登记，
  每条必须指向一个真实存在的判据；未登记的新承诺会让判据变红。

### Fixed

- adapter 的 `entry.Args` 被前置两次（注释还声称“两者必须一致”）。
- 两处以“由 CLI 的集成测试保证”为理由、实际上没有任何判据的承诺，补上了机械检查。
