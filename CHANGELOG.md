# Changelog

本文件是**发布说明的来源**：`release.yml` 只从对应版本的段落取文本，不再让
`--generate-notes` 按提交标题猜。段落标题必须写成 `## [<version>]`，`<version>`
是不带 `v` 的版本号（与 `ngm --version` 的第二列一致）。

只记录**用户可观察的行为**；逐版工程细节见 `docs/development/` 的计划与复盘。

## [Unreleased] - 2026-11-04

### Fixed（2026-11-21 追加，逐行审计 Batch D）

- **verify 的错误分类**：被篡改的模块内容清单归完整性失败（exit 2）；文件读不出来仍归
  操作性失败（exit 4）；上游模块 `ngm.json` 语法错误不再被当成版本漂移。
- **SARIF / GitHub 注解级别**：随 `--strict` / `--allow-drift` 调整，与进程退出码一致。
- **`tree` 与 `why`**：对含自环的依赖图给出一致结论，不再一个显示 cycle 节点、另一个给出零路径。
- **`mergeProject` 的所有引用字段**：`SupplyChain` / `Engines` / `Scripts` / `Dependencies` /
  `Publish` 现在都深拷贝，改有效视图不会回写来源。

### Fixed（2026-11-21 追加，逐行审计 Batch C）

- **沙箱权限不再被逗号放大**：含逗号的 `net:` / `env:` target 现在直接拒绝，
  不再出现"ngm 判定 a,b 为一条无效 target、Deno 却授权 a 与 b 两个主机"的偏差。
  旧配置若依赖逗号写多条主机/变量需改为逐条授权（[ADR-012 补记](docs/adr/adr-012-sandbox.md)）。
- **`ngm integrations add` 的 vendor 逃逸校验**：`ngm.json` 读不到时不再静默跳过，
  与 `mappings validate` 同口径回退默认 vendor 根继续校验。
- **`git ls-remote` 的 URL 与解析**：入口做 URL 形状校验（`ext::` helper 不再有执行面）；
  解析接受 40/64 位对象 ID，畸形行 fail-closed 报错而不是静默丢弃。
- **模块 cache 首次 clone**：成功后校验 lock 钉住的 commit 可解析；落地改为临时目录 +
  原子 rename，失败清理残骸；触网门禁改判缓存记录的真实 origin。

### Fixed（2026-11-21 追加，逐行审计 Batch B）

- **`install --json` / `update --json` 的中途失败**：错误信封此前被写进 stderr，
  stdout 仍有一份成功形状的报告；现在 stdout 恰好一份错误信封（报告在信封已写时抑制）。
  `verifyOnLock` 的漂移退出码 1 仍按结论写报告（[ADR-045 补记](docs/adr/adr-045-json-output-contract.md)）。
- **`modules` / `integrations` 的参数级失败**：所有接受 `--json` 的命令现在统一给机器可读信封，
  不再出现"同类失败两个答案"（`install --json extra` 有信封、`modules list --json extra` 没有）。
- **`modules index --dry-run --json`**：输出与普通成功路径同为 `modulesIndexReport`（`dryRun:true`），
  不再是裸索引对象——按 `version`/`index` 解析的消费者不会在 dry-run 上失败。
- **`ngm config validate` 假通过**：全局配置里写错的权限命名空间此前会先打印 `ngm.json OK`
  再退出 3；现在成功文本只在全部校验通过后打印。

### Added（2026-11-21 追加，竞品差距 followup）

- `ngm sbom` 导出 **CycloneDX 1.7**（此前 1.7 是 pnpm 12 的默认，ngm 停在 1.5）；
  输出仍字节确定性，`TestV138CycloneDX17SchemaShape` 守关键字段与依赖图引用。
- `ngm attest --with-timestamps`：显式开启才写 SLSA v1 的 `runDetails.metadata.startedOn` /
  `finishedOn`；**默认仍无时间字段**（同 lock 同字节），`invocationId` 恒为 lock 的字节哈希。
  开启后该产物放弃字节确定性——适合要在签名里体现产出时刻的场景（[ADR-027 补记](docs/adr/adr-027-attestation.md)）。
- **`supplyChain.minimumReleaseAge` 默认 P1D（24h）**：写了 `supplyChain` 段但没写冷却时生效，
  对齐 pnpm/Deno 的 2026 默认，防"新版本投毒在数小时内被撤回"的窗口（独立安全理由，非兼容 npm）。
  **完全不配 `supplyChain` 段仍不门禁**（[ADR-009](docs/adr/adr-009-supply-chain-policy.md) 底线不变）。
  新增 `ngm install --allow-fresh` / `ngm update --allow-fresh` 单命令逃生口（只关晾晒期，白名单等其他门禁不变）
  （[ADR-050](docs/adr/adr-050-default-release-age.md)）。

### Changed（2026-11-21）

- GitHub composite action 的 `verify-level` 默认从 `structure` 改为 **`content`**：CI 是供应链
  关键路径，默认做逐文件字节校验（CLI 的 `ngm verify` 仍默认 structure，本地循环保持轻量）。
  原来 structure 通过的项目在 action 默认下可能因字节不符而 exit 2——这是发现真问题；
  需要旧行为时显式 `verify-level: structure`（[ADR-051](docs/adr/adr-051-verify-level-defaults.md)）。
- **破坏性变更**：既有项目若写了 `supplyChain` 段但未显式配置 `minimumReleaseAge`，新解析的依赖
  现在要满足 24h 晾晒期。需要关闭时：删除整个 `supplyChain` 段（项目级），或对单次命令用 `--allow-fresh`。
  已锁在 `ngm.lock` 的 commit 走 lock 路径不被追溯重判。

### Added（2026-11-20 追加）

- `ngm sbom --allow-stale` / `ngm attest --allow-stale`：**lock 过期时默认拒绝导出**
  （exit 3，不写产物），显式给出该 flag 才放行（事实仍写 stderr，不进产物字节以保确定性）。
  此前只写一条 stderr warning 就照常产出——而 `attest` 的产物是要被签名的，签下一份
  "结构合法、哈希正确、却描述过期状态"的语句最难在事后发现（[ADR-046](docs/adr/adr-046-stale-export-gate.md)）。

### Fixed（2026-11-20 追加）

- **安全**：`module cache` 的首次 clone 现在与增量 fetch 一样过 `net:` 门禁——
  `deny net:<host>` 下不再真实触网；已缓存且 commit 命中的离线路径仍不需要 net 权限。
- **完整性**：安装期现在拒绝清单无法承载的路径（含 `\`、CR/LF、NUL、`.`/`..` 段，exit 3），
  与 verify 的模块重放共用同一套规则；`git ls-tree -z` 的畸形记录改为报错而非静默跳过
  ——此前两者可造出"装得出、验不过"或"看起来合理的错误 digest"（[ADR-008 补记](docs/adr/adr-008-archive-digest.md)）。
- `ngm verify --content --json` 的 `content.mismatches` 顺序现在可复现（细节切片已排序）。
- `ngm gc` 的 module cache 逐对象删除失败现在会正确折成 exit 4——此前报告里有 error 行，退出码却是 0。
- **WASM 引擎**的输出改为有界捕获（stdout 64 MiB 产物上界、stderr 1 MiB），撞上界时拒绝写出产物
  而不是静默截断——此前无界 `bytes.Buffer` 可被一个循环打印的模块 OOM 打死。
- 接受 `--json` 的命令在"多余位置参数 / 未知子命令 / verify 的 `--format`/`--level` 非法值"
  这些早期失败路径上，现在也写机器可读错误信封到 stdout——此前 `install` / `update` /
  `outdated` / `engines` / `verify` 在这些路径 stdout 为 0 字节。
- CI：build-test / acceptance / engine 三个 job 的超时从 30 分钟提到 45 分钟，
  大于 `go test -timeout 40m`（此前 job 先杀进程，失败细节看不到）；docs 链接检查不再重复跑两遍。
- **子路径跨平台一致**：`\` 在所有平台都等价于 `/`，`a/b` 与 `a\b` 不再被当成两条依赖
  （此前 Linux 上绕过了"重复根声明"判重），`#path=packages\core` 也能正确解析到模块。
- **沙箱脚本输出过滤控制字符**：ANSI/CSI 转义不再能伪造终端或 CI 日志里的报告行。
- `ParseISODuration` 拒绝会溢出的极大值——此前一条很长的 `minimumReleaseAge` 会静默变成一个很短的门禁。
- OSV 端点只接受 http(s) URL。`ngm modules index` 从"每个 tag 一次 git"改为一次批量解析。
- yarn v1 的 scoped 包 + `#ref`（`"@scope/pkg#v1.0.0"`）不再被误判为无来源。
- `ngm run build -- --help` / `-- --dir=`：`--` 之后的参数现在原样交给脚本，不再被
  ngm 截获成帮助请求或"空目录"错误。此前与 `runUsage` 的透传承诺矛盾。

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

- 原子写目标已经是**目录**时，现在明确报 `it is a directory` 并建议传文件路径；
  此前 rename 失败只给 `close any editor holding the file`，指错方向。
- `ngm adopt --out` 也会自动创建缺失的父目录（相对路径仍按 `--dir` 解析），
  与 `sbom` / `attest` / `transform` 一致。
- `ngm sbom --out` / `ngm attest --out` 的相对路径现在与 `--dir` 同根（与
  `ngm transform --outfile` 一致），并自动创建缺失的父目录。此前相对调用方 cwd，
  `--dir=subproj --out=dist/x.json` 会写到两个不同的地方；父目录缺失时还会以
  “check directory permissions”收场。
- `ngm init` 在 `src/index.ts` 已存在但**不是普通文件**时现在 exit 3；此前会打印
  `kept ... (already exists)` 并退 0，留下一个 `main` 指向目录的 `ngm.json`。
  预检发生在写 `ngm.json` 之前。
- `ngm install` 在大小写折叠的文件系统上拒绝“仅大小写不同”的依赖 vendor 路径
  （如 `github:Org/Repo` 与 `github:org/repo`）：此前第二个依赖会静默覆盖第一个，
  install 仍报成功。大小写敏感的文件系统上两个真实目录仍被允许。
- `ngm mappings validate` 现在要求 `to` 是目录、`main` / `types` 是普通文件：
  一个被误建成文件的 vendor 条目、或指向目录的入口，不再被判成健康。
- `ngm.mappings.json` 现在拒绝重复的导入标识符（同一 `from` + `path` 出现两次），
  不再让两个 `to` 在 `build` / `integrations` 的别名表里静默互相覆盖。v1 遗留的
  “同一 monorepo 多条子路径都没有 `path`”仍按原契约可读并给出警告。
- `ngm modules validate --index` 现在会读取 `--shards` 布局的
  `releases/<module>.json`：分片截断/写坏、指向未知模块、schemaVersion 不符
  都会报错。此前它只看根索引里的 inline releases，分片布局下等于没校验。
- `ngm modules index --shards` 现在先写 release 分片、最后写根索引：分片写失败
  时不再留下一份“索引列出了这份 release、文件却不存在”的 collection。
- `ngm init ""`（或全空白名字）现在直接以 exit 3 拒绝，不再写出一份 `name` 为空、
  随后所有命令都拒绝读取的 `ngm.json`。合法的 init 输出逐字节不变。
- `ngm install` 在 postinstall / 沙箱探针失败回滚时，现在也会把 `ngm.mappings.json`
  恢复到调用前的内容。此前只回滚新建的 vendor 目录，mappings 却留在失败这次写出的
  版本、指向已被删除的目录，而 lock 仍是旧的——项目从“能构建”变成“找不到文件”。
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
