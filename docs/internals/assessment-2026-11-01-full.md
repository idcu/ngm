# ngm 全维度评估报告

> 评估日期：2026-11-01  
> 评估对象：本仓库（HEAD 未打 tag，最新提交 `c156cf5`）  
> 评估方式：全量源码/文档清点，生产代码逐文件审查，核心链路逐行核对，全量构建与测试，外加 8 组黑盒复现场景，最后对照 2026 年主流竞品公开事实。  
> 本报告是**独立评估**，不复述项目自评；每条结论尽量给出文件、行号或可复现命令。

---

## 0. 结论摘要

**一句话判断：ngm 是一个工程质量很高、设计诚实、但产品完成度与市场成熟度明显不足的“Git 依赖证明层”。**

| 视角 | 评分（10 分制） | 结论 |
|---|---:|---|
| 作为 Git 依赖证明/审计工具 | **7.0** | 核心闭环真实存在：`refType` + commit + `archiveDigest` + 本地重放 + 漂移三态 + SBOM/attestation。 |
| 作为通用包管理器 | **3.0** | 不支持 registry 包，不支持 workspace 依赖解析，生态兼容性远不及 npm/pnpm/Yarn/Bun/Deno。 |
| 工程实现质量 | **8.0** | 分层清晰、错误模型统一、测试与 CI 罕见地完整；但存在若干核心路径的正确性缺陷。 |
| 安全设计 | **7.0** | 默认拒绝、沙箱、单点 git 子进程、路径/符号链接防护做得扎实；但权限边界仍有可绕过处，且“token 永不读取”的公开表述与代码不符。 |
| 交付/市场成熟度 | **4.5** | 32 天 101 个 tag，GitHub 0 star/0 fork，60/101 个 release，最新可下载 release 停在 v0.64.0。 |
| **综合（加权）** | **6.3** | 优秀的“安全审计型基础设施原型”，还不是可直接大规模采用的产品。 |

**最重要的五个判断：**

1. **差异化仍然成立，但窗口在收窄。** npm 的 provenance/SLSA L3、pnpm 11 的默认 24h 发布年龄、Deno 2.9 的默认 24h、Yarn 4.12 的默认 1d，已经补上了“默认闭环”的大部分外围。ngm 仍然独有的，是 **Git 依赖的 refType 自描述、内容级 archiveDigest、离线重放、漂移三态、模块级证明** 这一组组合。
2. **ngm 不是 npm/pnpm 的替代品。** 它没有 registry、没有 workspace 依赖解析、没有通用 monorepo 安装语义；它的正确生态位是“Git 依赖证明层 + 审计交付物 + CI 门禁”，其 `ngm adopt` 是降低切换门槛的正确方向，但目前只解析 npm 的 `package-lock.json`。
3. **核心链路存在可复现的正确性缺陷。** `ngm remove` 之后的 `ngm install` 不会移除 lock/vendor 中的旧依赖；模块依赖中的 gitlink 会被当成普通文件落地，随后 `ngm verify` 无法完成；短 commit 会被 verify 误报为“非预期漂移”；带 `/` 的分支名无法通过 `ngm add` 表达。
4. **安全边界有两处值得优先修。** 项目自己的 `ngm.json` 可以设置 `vendor.mode=global` + `vendor.globalPath`，把 vendor 写到项目之外；`ngm.mappings.json` 的 `to` 路径校验可被 `../../` 绕过，`mappings validate` 与 `integrations` 的防逃逸检查都拦不住。
5. **工程/文档的“自证”密度很高，但测试没有覆盖真实工作流。** 801 个测试函数、三平台 CI、race、验收、真实引擎集成全部通过，却漏掉了上述黑盒场景。这说明测试更擅长守住“已写下的规则”，而不是发现“规则之间的空洞”。

---

## 1. 方法与证据基线

### 1.1 本轮实际做了什么

| 项目 | 结果 |
|---|---|
| 源码清点 | 409 个 `.go` 文件；23 个 Go package；73,390 行 Go（非测试 29,632，测试 43,758） |
| 文档清点 | 300 个 Markdown，约 2.49 MB；README 约 107 KB；41 个 ADR |
| 构建 | `go build ./...` 通过 |
| 静态检查 | `go vet ./...` 通过 |
| 全量测试 | `go test ./... -count=1` 全部通过；`cmd/ngm` 约 481–492 秒，整轮约 9 分钟（Windows / Go 1.27.1） |
| 测试规模 | 274 个 `_test.go`，801 个 `Test/Benchmark/Fuzz` 函数 |
| 二进制 | 15.62 MB（`ngm-test.exe`，Windows/amd64） |
| 版本/交付 | 本地 101 个 tag，361 个 commit，全部由单一作者在 2026-09-29 至 2026-10-31 间完成；HEAD 无 tag |
| 远端事实 | GitHub API：60 个 release；最新 release `v0.64.0`（7 个资产：三平台 amd64/arm64 + SHA256SUMS）；0 star / 0 fork |
| 黑盒复现 | 8 组（remove/install、模块 gitlink、短 commit、带 `/` 分支名、mappings `../`、重复根声明、quickstart build、globalPath 外写） |

### 1.2 评估边界（必须说清）

- 性能数字多数来自项目自己的 `docs/internals/metrics.md`，本轮没有独立重跑 100 依赖基准；本轮复测的是构建/测试/黑盒行为。
- 竞品事实来自 2026 年官方文档/发布说明与公开搜索结果，具体版本能力以各官方文档为准。
- 本轮在 Windows 上执行；Linux/macOS 只通过 CI 配置与代码静态审查，没有本机实跑。
- 没有做外部安全审计、模糊测试、供应链投毒演练；`PackageGate` 类结论来自代码路径审查与项目自带 demo，不是第三方渗透测试。
- 没有验证所有 101 个 tag 的产物可下载；只核对了 GitHub release 数量、最新 release 资产与 README 自述。

---

## 2. 项目画像

### 2.1 它是什么

ngm 的实质是：

> **一个以 Git 仓库为依赖源、以 commit 为锚点、以规范化内容清单哈希为证据、以 vendor 为落地层的 Node.js/Deno 依赖证明与审计工具。**

它同时提供：

- Git 依赖解析与锁定（`ngm.json` / `ngm.lock`）；
- 内容寻址的 mirror/content store/vendor 四层；
- `verify` 的 ref/digest/landing 三层校验；
- OSV 审计、白名单、晾晒期、postinstall 沙箱；
- 外部引擎 adapter（esbuild/tsc/postcss/deno/wasm）；
- SBOM（CycloneDX/SPDX）与 in-toto/SLSA 语句；
- `adopt`（对已有 `node_modules` 出具内容证明）。

### 2.2 它不是什么

- 不是 registry 包管理器：不解析 `package.json` 的 `dependencies` 版本范围，不管理 npm/JSR 包。
- 不是 workspace 管理器：`modules` 只是“一仓多模块”的发现/索引/发布辅助，不做 workspace 依赖解析。
- 不是 bundler/typechecker/test runner：这些全部委托外部引擎。
- 不是完整的供应链安全产品：没有恶意包扫描、许可证识别、秘密扫描、SIEM/告警集成。

### 2.3 版本与市场信号

| 指标 | 读数 | 解读 |
|---|---|---|
| tag 数 | 101（v0.1.0–v0.101.0） | 32 天内 101 个版本，版本号膨胀严重；对用户没有稳定语义 |
| commit 数 | 361，单一作者 | 高产出，但同时是 bus factor = 1 |
| GitHub release | 60/101 | 41 个 tag 无 release；最新 release 停在 `v0.64.0` |
| 最新 tag | v0.101.0 | 源码在 tag 之后仍有未发布提交（HEAD 无 tag） |
| GitHub star/fork | 0 / 0 | 尚无外部采用证据 |
| 测试函数 | 801 | 测试密度高，但存在“规则自证”而非“工作流发现”的偏向 |

> 结论：**101 个 tag 不是成熟度，而是内部迭代速度。** 对外可消费的版本、文档、发布节奏才是成熟度；这三项目前都不足。

---

## 3. 架构与核心机制

### 3.1 分层

```
ngm.json（声明：name/ref/refType/path）
   ↓ resolve：URL 归一化 + refType→commit + 上游 ngm.json 展开 + 冲突裁决
ngm.lock（commit + archiveDigest/moduleDigest + subtreeOid + requiredBy）
   ↓ vendor：mirror（裸仓）→ module cache（partial clone）→ content store（blob 池 + 清单）→ link tree（hardlink/copy/symlink）
vendor/ngm.vendor（可审计、可提交、可离线）
   ↓ verify：ref 漂移分类 + digest 本地重放 + landing 结构/字节校验 + 可选签名/沙箱
CI/审计：OSV、SBOM、attestation、SARIF/GitHub annotations
```

### 3.2 三个真正有价值的设计

**（1）refType 自描述 + commit 锚点 + 内容 digest 三件套**

- `ref` 记录“人类意图”，`refType` 记录意图类型，`commit` 是不可变锚点，`archiveDigest` 是内容证据。
- 这与 npm `package-lock` 对 Git 依赖只记录 commit（其 `integrity` 对 Git 依赖就是 commit SHA）有本质区别；ngm 额外证明了“这个 commit 下的字节集合”。
- 证据：`internal/core/digest/manifest.go`（NUL 分隔的规范化清单）、`internal/core/lock/schema.go`、`docs/architecture/trust-model.md`。

**（2）漂移三态，而不是“lock 一致/不一致”**

- `expected`（branch 快进）、`unexpected`（tag 重打/force push）、`critical`（digest 不匹配）被分开，并映射到 0/1/2。
- 这是比 Yarn `--immutable`、pnpm lockfile 一致性更细的一层；也是 ngm 最有辨识度的输出契约。
- 证据：`internal/core/verify/report.go`、`internal/core/verify/verify.go`。

**（3）内容寻址 store 的“清单 + blob 池”**

- v2 布局将整树拆成 `blobs/<sha>` 与 `trees/<digest>/manifest.json`，同一文件跨 commit 去重；项目实测 12 个 commit 的放大比从 20.0× 降到 0.6×。
- 这解决的是“vendor 全量副本跨项目重复”的痛点，且保持 `mode` 由 Git 权威、跨平台 digest 一致。
- 证据：`internal/core/vendor/content_v2.go`、`docs/adr/adr-019-content-addressed-blobs.md`。

### 3.3 一个关键架构判断

ngm 的 `core` 用 Go，宿主运行时 Node/Deno；构建/类型/CSS 通过 adapter 调用外部工具。这条路线是**正确**的：它避免自研 bundler/typechecker，把精力集中在“证明”这一条窄轴。

但也因此产生一个结构性局限：**ngm 的能力上限取决于外部工具与 Git host**。当用户问“能不能替代 pnpm”时，答案是否定的；当用户问“能不能证明 Git 依赖”时，答案才是肯定的。

---

## 4. 命令面全量审查（26 个顶层命令）

| 命令 | 作用 | 评估 |
|---|---|---|
| `init` | 生成 `ngm.json` + `src/index.ts` | 可用；生成默认引擎 esbuild，但不生成 `run:esbuild` 权限，quickstart 第 5 步会失败 |
| `adopt` | 扫描已有 `node_modules` 出具内容证明 | 设计聪明，是降低采用门槛的最佳入口；只解析 npm lock，pnpm/yarn 只“说明读不了” |
| `add` | 添加依赖声明 | 基本可用；**无法表达带 `/` 的 branch/tag**（见 F4） |
| `install` | 从 manifest/lock 解析并落地 | 主链路完整；**不清理 lock 中的多余条目**（见 F1） |
| `update` | 重新解析并重建 lock | 完整；空依赖时提前返回，**不清理旧 vendor/mappings**（见 F8） |
| `remove` | 只改 `ngm.json` | 语义与文档一致；但文档说“install 刷新 lock”，实际 install 不会移除旧条目 |
| `modules` | 一仓多模块的 list/validate/index/changed/plan | 可用；模块 gitlink 处理有缺陷（F2）；`source.repo` 基本为空（F9） |
| `doctor` | 诊断环境/项目 | 可用；诊断型命令失败仍 exit 0，符合其定位 |
| `verify` | ref/digest/landing + 签名/沙箱 | 核心能力；选择性 verify 有 ghost edge（F7），短 commit 误报（F3） |
| `audit` | 按 commit 查 OSV | 可用；覆盖局限说明做得好；没有恶意包扫描 |
| `why` | 依赖来源路径 | 从 lock 图读取，0 次 git，离线可用；路径上限/截断可见 |
| `tree` | 依赖树 | 可用；**仍重新解析，且帮助文本说 lock 没有拓扑**（已过时） |
| `outdated` | 检查更新 | 可用；区分 unknown 与 no update，离线正确 |
| `typecheck` | 外部类型检查 | 可用；帮助文本“本 build 未适配类型引擎”已过时（内置 tsc） |
| `typedecl` | 生成 .d.ts | 可用；必须 `--outdir`，语义清楚 |
| `build` | 外部 bundler | 可用；`run:<engine>` 默认需授权，首次体验有摩擦 |
| `transform` | 单文件转换 | 可用；stdin/loader 规则清楚 |
| `css` | 外部 CSS 编译 | 可用；`--outfile` 未校验产物存在（与 build 的修复不对称） |
| `mappings` | 校验 mappings | 可用；`to` 路径校验可被 `../` 绕过（F5） |
| `integrations` | Vite/esbuild/Deno/Webpack 脚手架 | 可用；“全有或全无”写盘做得好；同样受 F5 影响 |
| `cache` | 清缓存层 | 可用 |
| `store` | usage/prune/gc | 非常扎实：年龄门槛、只删残骸、孤儿回收拒绝读不出清单 |
| `config` | validate/show | 可用；但 `builtin.GitProtocol=ssh` 与实际默认 HTTPS 不一致（F10） |
| `engines` | list/info/validate | 可用；权限默认需配置，提示可复制 |
| `sbom` | CycloneDX/SPDX | 可用；确定性、无许可证臆测；purl 使用非标准 `pkg:git`（F11） |
| `attest` | in-toto/SLSA v1 | 可用；无签名、无时间戳、输入字节被哈希；purl 同上 |

**命令面总体评价：** 功能覆盖广，错误信息质量远高于一般开源 CLI；但“首次可用性”被默认拒绝权限与文档脱节拖累，且 remove/module/verify 三条路径有实质缺陷。

---

## 5. 子系统逐项评估

### 5.1 `internal/core/resolve`（依赖解析与图）

**实现要点：** URL 归一化（https/ssh/scp/git/shorthand/loopback）、refType 解析、短 commit 补齐、BFS 图解析、同层并发、root wins、传递冲突报错、上游 `ngm.json` 展开。

**优点：**

- `Normalize` 对 userinfo、端口、控制字符、`..` 路径段做了细致拒绝；`hasControlChar` 防参数注入。
- 图解析按 Key 去重，层内错误按索引确定性返回；`RequiredBy` 存节点 Key，monorepo 子路径不会指错。
- 上游 `path` 与根声明共用 `ValidateSubPath`，避免两套规则漂移。

**缺陷/风险：**

- **短 commit 解析的上下文不一致**：`expandShortCommit` 在没有本地 GitURL 时直接返回短 hash（`internal/core/resolve/reftype.go:112-121`）。在线 verify 的 commit 型走的是 `RemoteRefResolver`，它调用 `ResolveRef` 时没有传 GitURL（`cmd/ngm/env.go:172-174`），于是 verify 永远拿短 hash 与 lock 的完整 commit 比较，必然“漂移”。
- **带 `/` 的 ref 无法通过 `ngm add` 表达**：`ParseAddSpec` 只在最后一个 `@` 之后不含 `/` 或 `:` 时才把尾部当 ref（`internal/core/resolve/addspec.go:53-58`）。于是 `github:org/repo@feature/x` 被当成 scp-like URL，直接 exit 3。Git 分支/标签带 `/` 是常态。
- **重复根声明不报错**：`config.ProjectFile.Validate` 不检查同一 `(name,path)` 重复；`mergeFrontier` 在 ref 冲突时用 `rootSet[k]`（最后一个根声明）覆盖（`internal/core/resolve/graph.go:399-406`）。黑盒复现：v1/v2 两条根声明，`ngm.json OK`，update 静默选择 v2。
- **SHA-256 仓库支持不完整**：`ParseLSRemoteOutput` 只接受 40 位 SHA（`internal/core/git/lsremote.go:60-61`），lock 的 commit 也只接受 40 位（`internal/core/lock/schema.go:272-276`）；而 `subtreeOid` 却支持 sha256。两者不一致。
- **`ReadFileAtCommit` 的路径被当作 Git pathspec**：`internal/core/git/blob.go:28`。如果子路径含 `*`/`?`/`[`，可能匹配到别的文件；`ValidateSubPath` 未拒绝这些字符。

**评分：8.0/10。** 主逻辑清晰，边界处理认真；短 commit、ref 语法、重复声明是必须修的正确性问题。

### 5.2 `internal/core/lock`（lock schema 与确定性）

**实现要点：** v2 lock、字段顺序固定、`requiredBy` 直接边、排序/去重、`Validate`（版本、唯一性、幽灵边）、原子写。

**优点：**

- `Marshal` 在序列化时归一化 `requiredBy`，同一解析结果字节稳定。
- `ValidateUnique` 拒绝同 `(name,subPath)` 重复，避免 `Index`/`Find` 不一致。
- `ValidateEdges` 拒绝幽灵边，`HasGraph` 让旧 lock 优雅降级。
- `lock.Read` 返回 `(nil,nil)` 表示无 lock，语义清楚。

**缺陷/风险：**

- **`VendorPath` 未校验与 canonical repo 的一致性**：`internal/core/lock/schema.go:312-314` 只要求非空。`verify` 用 lock 的 `vendorPath` 定位 vendor（`internal/core/verify/verify.go:776`），而 `install` 用 canonical repo 落地（`cmd/ngm/env.go:410`）。手改/旧 lock 可导致 verify 检查错误目录。
- **commit 只接受 40 位**：与 SHA-256 仓库不兼容（见 5.1）。
- **`archiveDigest` 形状只校验前缀**：这是有意的（避免把篡改降级成 exit 3），但消费方（sbom）已补完整校验；其他消费方若忘记，会写出空哈希。当前 `sbom.FromLock` 已处理。
- **`resolvedAt` 不校验格式**：手工 lock 可写任意字符串；不影响判定，但影响审计可读性。

**评分：8.0/10。** 格式纪律很强；`vendorPath` 与 SHA-256 是两个具体缺口。

### 5.3 `internal/core/digest`（archiveDigest/moduleDigest）

**实现要点：** NUL 分隔清单、路径 UTF-8 字节序、mode 三态、symlink 目标作为内容、LFS 指针拒绝、模块 scope 头。

**优点：**

- 格式定义清楚、可跨平台重放；不对 tar/zip 字节做哈希，避开 Git host archive API 差异。
- `BuildModuleManifest` 与 `ngm-module-digest/v1` 域分离，避免根/模块 digest 混用。
- `ValidateRecord` 对不可信清单做 NUL 注入防护（`internal/core/digest/validate.go`）。

**缺陷/风险：**

- **模块批量路径绕过了条目校验**：`BuildModuleMaterials` 直接构造 `digest.Record`，没有调用 `validateTreeEntry`（`internal/core/git/tree.go:491-497`），因此 gitlink/未知 mode 会进入模块清单。根路径会拒绝（`tree.go:214-219`），模块路径不会。黑盒已复现（见 F2）。
- **`cat-file --batch` 的 size 无上界**：`make([]byte, size)`（`internal/core/git/catfile.go:136`）在 mirror 损坏/恶意时可造成巨额分配。本地 mirror 通常是可信的，但这是可加的硬边界。
- **空仓库 digest 合法**：只有头部；这与“空 commit 合法”一致，但消费方需知道。

**评分：9.0/10。** 这是项目最扎实的一层；模块 gitlink 是 implementational bug，不是格式设计问题。

### 5.4 `internal/core/git`（Git 封装）

**实现要点：** 唯一构造点 `newGitCommand`（run:git 门禁 + spawn 计数 + WaitDelay）、ls-remote 解析、clone/fetch、cat-file batch、tree/digest 读取、签名验证、config 快速读、GC。

**优点：**

- `newGitCommand` 是单一构造点，`TestSpawn_OnlyOneFileBuildsProcesses` 机械守住；这是很好的安全设计。
- 剔除 `GIT_DIR` 等仓库定位变量；`GIT_TERMINAL_PROMPT=0`；`LC_ALL=C`；token 输出脱敏。
- `IsMirror` 用 refspec 判断“克隆是否完成”，区分“形状像裸仓的废墟”；`CloneLandingInfix` 原子落地。
- `cat-file --batch` 用单进程批读，避免数百次 spawn。

**缺陷/风险：**

- **SHA-256 仓库的 ls-remote 全部丢弃**（40 位检查）；commit 也是 40 位限制。
- **cat-file 分配无上界**。
- **tag 签名验证依赖 `BAD signature` 文案**（`internal/core/git/signature.go:136-139`）：git 版本/语言变化可能影响“bad vs untrusted”分类；不影响 pass/fail（退出码决定），但影响报告语义。
- **`ReadFileAtCommit` 的 pathspec 语义**（见 5.1）。
- **`BuildModuleMaterials` 不校验 mode**（见 5.3）。

**评分：8.0/10。** 安全施工质量高；两处“未来兼容/资源边界”需要补。

### 5.5 `internal/core/vendor`（mirror / module cache / content store / link tree）

**实现要点：** 四层布局、content v1/v2 双读、blob 池 + tree manifest、hardlink/copy/symlink、原子发布、prune/gc、case-collision 防护、symlink 逃逸拒绝。

**优点：**

- 路径逃逸与 symlink 目标逃逸在落地前 fail-closed（`links.go:262-279`），且 verify 用同一判据（`EscapesPackageTree`）。
- Windows 大小写折叠冲突检测在真实磁盘上探测，不靠 GOOS 猜测；拒绝而不是静默丢文件。
- content store 发布顺序：blob → fsync → manifest → rename → fsync dir；中断不会留下“半棵树”。
- `store prune --orphans` 在读不出清单时拒绝删除；年龄门槛 24h；逻辑严谨。
- 模块 cache 用 `blob:none`，filter 未生效会删除并报错，不假装省流量。

**缺陷/风险：**

- **模块 gitlink 落地为普通文件**：`linkEntries` 对 mode 160000 当作普通文件复制/链接（`links.go:430-458`），而 store 里写的是 commit 对象的文本。黑盒复现得到 156 字节的 commit 文本文件。
- **旧 vendor 不会随依赖删除而清理**：`LinkTree.Materialize` 只删“本次目标目录”（`links.go:218-222`），不维护 vendor 根的所有权清单；`installFromLock` 只处理 lock 中存在的条目。`remove` 后旧依赖仍在 vendor/mappings。
- **`ContentStore.Has` 只检查清单 + meta，不检查 blob 是否存在/正确**（`content.go:120-135`）；`Put` 遇到已存在 blob 直接复用（`content_v2.go:223-225`）。`verify --deep` 能发现，但默认浅校验发现不了。
- **`vendor.globalPath` 来自项目清单且不受限**（见 F6）。
- **materialize 的父目录 symlink 风险**：`os.MkdirAll(filepath.Dir(dst))` 会跟随父目录 symlink；如果仓库提交了带 symlink 的 `ngm.vendor`，install 可能写到项目外。当前 entry 级 symlink 有防护，但父目录没有。

**评分：8.0/10。** 这是全项目最复杂的子系统，防护做得很细；但“所有权/清理”与“模块 gitlink”是结构性缺口。

### 5.6 `internal/core/verify`（三级校验）

**实现要点：** ref/binding/digest/landing，依赖级并发，结果按索引落位，digest 缓存，离线/在线取数策略，SARIFF/GitHub 渲染。

**优点：**

- 退出码优先级明确：critical(2) > operational(4) > unexpected(1) > expected(0)；`--allow-drift` 不降级 critical。
- 不完整检查也留痕：ref 失败时 digest 记录 “not attempted”，避免“少一条 = 通过”。
- 在线 verify 只 ls-remote，不传对象；commit 缺失先取再判“上游丢弃”，避免把“没下载”误报成“force push”。
- landing 默认浅校验，`--deep` 追加逐文件哈希 + 从 store 重放 digest；明确写出“默认检不出等长篡改”。

**缺陷/风险：**

- **选择性 verify 的幽灵边**：`filterLock` 保留完整 `RequiredBy`（`cmd/ngm/verify.go:430-472`），随后 `verify.Run` 对过滤后的 lock 调 `Validate`（`internal/core/verify/verify.go:148`），`validateEdges` 发现父节点不在子集 → exit 3。黑盒：`ngm verify github:acme/dep --offline` 报 “requiredBy references github:acme/lib, which is not in this lock (a ghost edge)”。
- **短 commit 自反漂移**（F3），根因在 resolve，不在 verify 本身。
- **Summary 计数漏掉 critical+operational 的组合**：`Summarize` 遇到 `Err != ""` 就 `continue`，不统计 `DriftKind`（`internal/core/verify/report.go:228-249`）。一个依赖同时 critical + ref 失败时，`summary.critical` 会是 0，但 `exitCode` 仍是 2；人读汇总行会写 “incomplete”。
- **默认浅校验的安全语义依赖注释**：`verifySummaryLine` 已加提示，但如果 CI 只读 exit code，仍可能把结构一致当成字节一致。

**评分：8.0/10。** 判定/渲染分离和退出码设计优秀；选择性过滤与汇总计数是明确 bug。

### 5.7 `internal/security`（权限与沙箱）

**实现要点：** 五命名空间 default-deny、`planSandbox`、Deno 参数显式 deny、`--no-npm/--no-config/--no-lock`、超时与有界输出、env 剔除。

**优点：**

- `DenoArgs` 显式写 `--deny-run/--deny-write/--deny-net/...`，边界可读；不从用户 `run:` 继承，避免“派生进程绕过沙箱”。
- 沙箱 read 只给依赖子树；net/env 必须显式 allow；no-remote/no-npm/no-config 补上模块加载侧洞。
- `boundbuf` 限制 stdout/stderr 内存，截断显式标记。
- `envutil.Without` 三个施力点共用，避免“只修两处”。

**缺陷/风险：**

- **`vendor.globalPath` 不受权限模型约束**：权限模型明确写 `write:$PROJECT/ngm.vendor`，但 `write:` 不在 ngm 自身路径上施加；项目清单可把 vendor 指到项目外。
- **Deno 沙箱没有内存/CPU 上限**（docs 诚实记录），只限输出；一个 CPU 密集脚本仍可跑满 30 秒。
- **`--deny-read` 对模块加载是否完全有效需要单独验证**：Deno 的静态 `import` 走模块加载器，不完全等同 `Deno.readFile` 权限；当前依赖 `--no-config/--no-npm/--no-remote` 收窄，但没有在真实攻击面测试“file: 绝对导入”。
- **token 公开表述与代码不符**：`auth.go` 会读取 `GITHUB_TOKEN` 等环境变量的值用于脱敏（`internal/core/git/auth.go:25-49`）；README 却写 “ngm never reads or stores tokens”（`README.md:141`）。这不是“用 token 认证”，但字面承诺不成立。

**评分：7.5/10。** 在同类工具里属于很强的默认安全设计；边界仍有缺口。

### 5.8 `internal/supplychain`（OSV / 白名单 / 晾晒期）

**实现要点：** OSV 按 commit 查询、24h 缓存、离线只用缓存、allowlist/拒绝来源链、`minimumReleaseAge` 基于 committer date、`osvIgnoreSeverities`、audit hook。

**优点：**

- 按 commit 而不是版本查询，命中“版本没变、commit 变了”的投毒形态。
- `CoverageNote` 强制出现在报告里，明确“查不到 ≠ 安全”。
- 白名单在解析前生效，拒绝时带来源链；`allowlistRepos` 放行同时豁免晾晒期（紧急修复通道）。
- OSV 客户端有 30s 超时；`CheckNet` 在真实端点主机上判定。

**缺陷/风险：**

- **晾晒期没有默认值**：pnpm 11、Yarn 4.12、Deno 2.9 都已默认 24h/1d；ngm 需要显式配置。作为“安全优先”工具，这是竞争力缺口。
- **时间源可伪造**：committer date 由提交者控制；docs 已诚实记录，但门禁信任度有限。
- **OSV 覆盖面有限**：按 commit 查询的公告覆盖率低；没有恶意包/typosquatting 检测。
- **audit 不做传递依赖的“路径去重”**：同一 commit 在多路径下会重复查询（缓存缓解）；报告 `Findings` 按 lock 条目，不按唯一 commit。
- **`severitySummary` 排序 bug**：先按严重度拼字符串，再 `sort.Strings`（`internal/supplychain/report.go:114-130`），会打乱严重度顺序（例如 “10 HIGH” 排在 “2 CRITICAL” 前）。

**评分：7.5/10。** 方向正确、边界诚实；默认策略强度不及 2026 年的头部竞品。

### 5.9 `internal/adapter` + 构建命令

**实现要点：** embed/subprocess/wasm/remote（排除）、catalog、selection、argv 翻译、stdout 产物上限、回退链、run: 权限、env 剔除。

**优点：**

- `splitCommand` 不经过 shell，直接 execve；清单来自仓库也不会有 `sh -c` RCE（这是明确的安全选择）。
- `runArtifact` 对 stdout 产物截断即失败，避免“看起来成功但产物被截断”。
- wasm 32 MiB 内存上限 + WASI 只读 preopen + 无 socket/spawn；这是 subprocess 之外的真实隔离选项。
- `preflight` 顺序（能力 → 可用性 → 权限）合理，错误码 5/3 区分清楚。

**缺陷/风险：**

- **首次 build 的权限摩擦**：`run:esbuild` 默认需配置；quickstart 只提示 `net:`，未提示 `run:`；黑盒复现 `ngm build` exit 3。
- **自定义引擎清单是仓库可控的**：`ngm.engines.json` 可覆盖内置 `command`；虽然有 `run:` 权限，但用户一旦授权 `run:esbuild`，仓库就能替换实际二进制/参数。这是“项目代码可信”假设的一部分，需要在文档中更醒目。
- **`compile` 的 outfile 未校验**：subprocess `Compile` 只把路径写进结果，不检查文件是否真的出现；`Bundle` 已修，`Compile` 未修。
- **`typecheck` 帮助文本过时**：说“本 build 未适配类型引擎”，但内置 catalog 已有 `typescript`（optional）。
- **`transform` 的 stdin 无上限**：`io.ReadAll(stdinReader)` 可被超大输入打爆内存。

**评分：8.0/10。** 协议设计与安全边界很好；权限 UX 与个别对称性缺口需要修。

### 5.10 可观测性 / SBOM / attestation / adopt / modules

| 子系统 | 优点 | 风险 |
|---|---|---|
| `why` | lock 图读取，0 git，离线可用；路径上限/截断诚实；`Source` 明确 | `--all` 可指数爆炸；依赖 lock 图完整性 |
| `tree` | cycle/truncation/drift 标记；OSV 显式开启 | 仍重新解析（帮助文本说 lock 无拓扑，已过时）；按路径展开可爆炸 |
| `outdated` | unknown ≠ 无更新；offline 不触网；tag semver/日期规则 | tag 语义不区分 prerelease/分支来源；非 semver 时“更新”判断较粗 |
| `sbom` | CycloneDX 1.5/SPDX 2.3；确定性；无许可证臆测；图不完整则整段省略 | `pkg:git` 非主流 purl 类型；`tools` 使用 1.5 的 legacy 数组形态（schema 允许但已弃用） |
| `attest` | in-toto v1 + SLSA v1；无时间戳；输入字节哈希；可外部签名 | 不覆盖依赖图（有意）；purl 同上；签名仍归用户 |
| `adopt` | 零切换成本；npm lock 可追溯 commit/integrity；`--check` 只读；覆盖统计诚实 | 只解析 npm lock；pnpm/yarn 明确“不解析”；digest 跨平台去掉了 mode，因此与 Git 侧 archiveDigest 不等 |
| `modules` | 集合索引/分片/本地分支；逐模块 publish；版本规划 | 模块 gitlink（F2）；`source.repo` 基本为空（F9）；`index` 默认写本地 branch，可能移动当前分支 |

**评分：7.5/10。** 输出纪律与“不知道就说不知道”做得很好；互操作与少数路径需要补。

### 5.11 CLI / UX / 文档

**优点：**

- 错误模型统一：`errs.NgmError` + 退出码 + 可复制 hint；`runErr` 在 `--json` 下同时给人读与机器读信封。
- 大量“实测缺陷 → 修复 → 加网”的记录，说明维护者极度重视“读数不说谎”。
- 文档量惊人：41 个 ADR、每版计划/复盘、`project-state`、`metrics`、`capabilities`；诚实边界写得很清楚。

**缺陷/风险：**

- **quickstart 不可逐字执行**：第 5 步 `ngm build` 在只有 `net:github.com` 授权时会 exit 3（缺少 `run:esbuild`）。
- **文档与实现漂移**：`tree` 说 lock 无拓扑；`typecheck` 说未适配类型引擎；README 说 v0.57 最新可下载，但 GitHub API 显示 v0.64.0 release 有 7 个资产；README 说 token 永不读取，代码会读取用于脱敏。
- **默认协议声明不一致**：`config show` 的 builtin 写 `GitProtocol: ssh`，实际 `newProjectEnv` 默认 HTTPS；`protocol.go` 注释也写“缺省 ssh”。
- **文档成本高**：2.49 MB 文档 / 30k 生产代码；新用户上手需要读大量 ADR，学习曲线陡。

**评分：8.0/10（文档质量）/ 6.5/10（首次可用性）。**

---

## 6. 缺陷清单（按严重度）

> 评级定义：  
> **P1** = 核心承诺被破坏/安全边界可绕过，需优先修；  
> **P2** = 重要功能错误、可导致错误结论或错误落地；  
> **P3** = 边界硬化、可维护性、文档/互操作问题。

### P1（必须先修）

#### F1. `ngm remove` 后 `ngm install` 不会移除 lock/vendor 中的旧依赖

**证据：** `cmd/ngm/install.go:141-155` 只检查“manifest 中的根是否都在 lock 中”（`lockManifestMismatch`，`install.go:192-205`），不检查 lock 中是否有 manifest 已不存在的条目；`installFromLock` 遍历整个 lock（`install.go:344-508`）。

**复现：**

```
ngm update --all --offline   # lock: lib + dep
ngm remove github:acme/lib
ngm install --offline        # exit 0
# lock 仍是 lib + dep；ngm.vendor 仍含 lib 与 dep
```

**影响：** 删除依赖后，旧代码仍被落地、仍被 mappings 暴露给构建工具；审计视图与 lock 不一致。`remove` 的帮助文本说“install 会刷新 lock”，与事实不符。

**建议：** `install` 应计算“manifest 声明闭包 + lock 可达集”，发现 lock 有孤儿条目时拒绝或触发重解析；或 `remove` 标记 lock 过期，`install` 检测到“条目数/可达性变化”后走 fresh 路径。不要把“尊重 lock”与“忽略声明删除”混为一谈。

#### F2. 模块依赖中的 gitlink 被当成普通文件落地，随后 verify 永远无法完成

**证据：** `buildModuleProofs` 用 `BuildModuleMaterials`（`cmd/ngm/module_proof.go:50`）；`BuildModuleMaterials` 构造 `digest.Record` 时没有调用 `validateTreeEntry`（`internal/core/git/tree.go:491-497`）；`linkEntries` 把 mode 160000 当普通文件复制/链接（`internal/core/vendor/links.go:430-458`）。而 `verify` 走 `modproof.Prove`，会拒绝 gitlink（`internal/core/verify/verify.go:614-626`）。

**复现：**

```
ngm update --all --offline     # 模块 commit 内含 submodule
ngm.vendor/.../packages/mod/sub 是 156 字节的 commit 对象文本
ngm verify --offline           # exit 4: "contains a gitlink (submodule) at \"sub\""
```

**影响：** 模块集合只要含一个 submodule，install 会产出错误内容，verify 无法证明；这是“看起来成功、结果是错的”的典型形态。根条目已正确拒绝 gitlink（`tree.go:544-560`），模块路径没有。

**建议：** `BuildModuleMaterials` 对每个条目调用 `validateTreeEntry`，在生成 digest/写入 store 前拒绝 gitlink/未知 mode；同时补一条模块级负测试。

#### F3. 短 commit 在 verify 中会“自己和自己漂移”

**证据：** `expandShortCommit` 只在 `opts.GitURL` 存在时补齐，否则原样返回（`internal/core/resolve/reftype.go:112-121`）；`RemoteRefResolver` 对 commit 型调用 `ResolveRef` 时没有传 GitURL（`cmd/ngm/env.go:172-174`）；`checkRef` 拿返回值与 lock 的完整 commit 比较（`internal/core/verify/verify.go:439-465`）。

**复现：**

```
ngm add github:acme/shorty@5b28ce4 --ref-type commit
ngm update --all --offline      # lock.commit 是全 40 位
ngm verify --offline            # exit 1
# 输出：5b28ce4 → 5b28ce4 (now 5b28ce4) — unexpected drift
```

**影响：** 任何使用短 commit 声明的依赖都会在 CI 中被误报为“上游漂移”；这是最容易被用户当成“工具坏了”的错误。

**建议：** commit 型解析始终在本地 mirror 上补齐（verify 已有 mirror），或在 `RemoteRefResolver` 中传入 mirror 路径；若对象不存在，明确报“无法解析”，不要返回短 hash。

#### F4. 带 `/` 的 branch/tag 无法通过 `ngm add` 表达

**证据：** `ParseAddSpec` 只在尾部 `@ref` 不含 `/` 与 `:` 时才剥离（`internal/core/resolve/addspec.go:53-58`）。`feature/x`、`release/1.0` 是常见分支名。

**复现：**

```
ngm add github:acme/slash@feature/x --ref-type branch
# exit 3: invalid git address; hint: scp-like address must be `user@host:org/repo`
```

**影响：** 用户只能手改 `ngm.json` 才能添加这类依赖；对一个“Git-first”工具是明显功能缺口。

**建议：** 只把 `:` 视为 scp-like 的排除字符，允许 ref 含 `/`；或增加 `--ref` 参数；同时补 `feature/x` 的解析测试。

#### F5. `mappings` 的 `to` 路径校验可被 `../../` 绕过，integrations 防逃逸检查同源失效

**证据：** `mappings.Validate` 只用字符串前缀判断 `to` 是否在 vendor 根下（`internal/mappings/validate.go:124-137`），`resolveToAbs` 用 `filepath.Join` 解析（`validate.go:239-244`）；`integrations` 的前置检查复用同一 `Validate`（`cmd/ngm/integrations.go:94-113`）。`ngm build` 的 `mappingsAlias` 直接读取 `to`，不做校验（`cmd/ngm/engine.go:206-234`）。

**复现：**

```
# 已有一个项目 + lock（dep 存在）
写 ngm.mappings.json: {"from":"github:acme/shorty","to":"./ngm.vendor/../../outside-b","main":"./index.js"}
ngm mappings validate  # exit 0: "ngm.mappings.json OK (1 mappings)"
```

**影响：** 一个提交进仓库的 mappings 文件可以让 `ngm build` 的 alias 指向 vendor 之外的目录；`integrations` 的“别名不得指向 vendor 之外”承诺被绕过。虽然 `ngm install` 会重生成 mappings，但“先提交 mappings、后 install”与 CI 读取已提交配置的场景存在。

**建议：** 对 `to` 先 `filepath.Clean`，拒绝含 `..` 的段，再用 `filepath.Rel(vendorRootAbs, toAbs)` 判定 `..`；`mappingsAlias` 在消费前也校验一次（至少 `EscapesVendor`）。

#### F6. 项目清单的 `vendor.globalPath` 可把 vendor 写到项目之外

**证据：** `VendorConfig.GlobalPath` 来自 `ngm.json`（`internal/config/config.go:146-150`）；`Validate` 只要求非空（`internal/config/validate.go:287-312`）；`VendorRoot` 直接返回 `expandUserPath(p)`（`cmd/ngm/env.go:289-297`）；`LinkTree.Materialize` 会在该路径下 `RemoveAll(dst)`（`internal/core/vendor/links.go:218-222`）。

**复现：**

```
ngm.json: "vendor":{"mode":"global","globalPath":"<项目外目录>"}
ngm update --all --offline
# <项目外目录>/github.com/acme/lib 被写入
```

**影响：** 项目清单是仓库内容（不可信输入），却决定了写盘位置；这与安全模型“写 vendor 是 $PROJECT/ngm.vendor”的声明不一致。可覆盖/删除目标路径下同形状的目录，也可写到网络盘。

**建议：** 把 `vendor.mode=global` 与 `globalPath` 移到用户级 `~/.ngm/config.json`，或至少要求 `globalPath` 只能是默认 `~/.ngm/global-vendor`；项目级只能“选择是否使用全局 vendor”，不能指定任意路径。写入前增加 canonical 校验与“目标必须在已授权根下”的检查。

### P2（重要，应尽快修）

#### F7. 选择性 `ngm verify <dep>` 对传递依赖报幽灵边

**证据：** `filterLock` 保留完整 `RequiredBy`（`cmd/ngm/verify.go:430-472`）；`verify.Run` 对过滤后的 lock 调 `Validate`（`internal/core/verify/verify.go:148`）；`validateEdges` 拒绝父节点不在子集的边（`internal/core/lock/schema.go:216-239`）。

**复现：**

```
ngm verify github:acme/dep --offline
# exit 3: requiredBy references github:acme/lib, which is not in this lock (a ghost edge)
```

**建议：** 过滤时清空/裁剪 `RequiredBy`（verify 不需要图），或给 `Run` 一个“跳过边校验”的选项；补一条“只验传递依赖”的测试。

#### F8. `ngm update --all` 在空依赖时提前返回，旧 vendor/mappings 不清理

**证据：** `runUpdate` 在 `len(g.Nodes)==0` 时写空 lock 后直接返回（`cmd/ngm/update.go:150-157`），不调用 `materializeVendor`；`LinkTree` 只删每个本次目标的目录，没有全量清理（`internal/core/vendor/links.go:218-222`）。

**复现：**

```
ngm remove github:acme/lib
ngm update --all --offline
# lock: dependencies []
# ngm.vendor 仍含 lib/dep；ngm.mappings.json 仍映射 lib/dep
```

**建议：** 为 vendor 建立“本次拥有的路径清单”，每次 install/update 结束时删除不在清单内的 ngm 拥有的目录；空依赖时也要重写 mappings（空数组）并清理。不要删除非 ngm 拥有的用户目录。

#### F9. `modules index` 的 `source.repo` 基本为空

**证据：** `collectSource` 用 `resolve.ParseSlug(url)` 解析 `git remote get-url origin`（`cmd/ngm/modules.go:790-795`）；而 `ParseSlug` 明确拒绝 `://`（`internal/core/resolve/slug.go:31-37`）。正常的 `https://...` / `ssh://...` remote 全部解析失败，`Source.Repo` 留空。

**建议：** 用 `Normalize`（允许 URL）而不是 `ParseSlug` 解析 remote；补 HTTPS/SSH remote 的测试。

#### F10. 默认协议声明与实际行为不一致

**证据：** `DefaultBuiltin().GitProtocol = "ssh"`（`internal/config/config.go:221-232`）；`config show` 输出 `GitProtocol: "ssh"`；但 `newProjectEnv` 初始化 `proto := resolve.ProtocolHTTPS`（`cmd/ngm/env.go:79`）；`protocol.go` 注释也写“缺省 ssh”。

**影响：** 用户看 `config show` 以为默认 SSH，实际 clone 用 HTTPS；排查认证问题时会被误导。

**建议：** 二选一：把 builtin/documentation 改成 https，或让 `newProjectEnv` 使用 builtin 的 ssh 作为默认；不要保留两套默认。

#### F11. SBOM/attestation 使用非标准 `pkg:git` purl

**证据：** `Entry.Purl()` 返回 `pkg:git/<canonical>@<commit>`（`internal/sbom/sbom.go:113-127`）；attestation subject 直接使用它（`internal/attest/attest.go:105-109`）。

**影响：** purl 生态的已知类型以 `github`/`gitlab`/`bitbucket`/`generic` 等为主，`git` 并非主流解析器广泛支持的类型；下游 SBOM 工具可能无法解析该 purl，导致组件引用失效。CycloneDX schema 只把 purl 当字符串，因此 schema 校验不会发现。

**建议：** GitHub/GitLab/Gitee 使用对应 purl 类型；自建 host 用 `pkg:generic/...?vcs_url=git+<url>@<commit>`；或保留 `bom-ref` 稳定 ID，但把 `purl` 换成标准形态。补一条“用主流 purl 解析器解析”的测试。

#### F12. 重复根声明被接受并静默“最后一个赢”

**证据：** `ProjectFile.Validate` 不检查重复 `(name,path)`（`internal/config/validate.go:72-76`）；`mergeFrontier` 在根声明 ref 冲突时使用 `rootSet[k]`（最后一个写入者）（`internal/core/resolve/graph.go:399-406`）。

**复现：** `ngm.json OK`；`ngm update --all --offline` 静默选择 v2，lock 只有一条 v2 记录。

**建议：** 配置校验拒绝重复根声明；或在 `ResolveGraph` 对“两个根声明不同 ref”报 exit 3，并列出两条声明。

#### F13. `tree` 仍重新解析，帮助文本与 v0.73 事实不符

**证据：** `tree` 的帮助写“ngm.lock 是扁平列表，没有拓扑”（`cmd/ngm/tree.go:30-34`），但 `ngm.lock` 自 v0.73 带 `requiredBy`；`why` 已经用 `GraphFromLock` 读取（`cmd/ngm/why.go:100-105`），`tree` 仍走 `ResolveGraph`（`tree.go:107-111`）。

**影响：** `tree` 每次都要 mirror/解析，离线冷启动更容易失败；文档也误导用户。

**建议：** 像 `why` 一样优先 `GraphFromLock`；旧 lock 无图时再回退解析，并在报告里写 `Source`。

#### F14. `typecheck` 帮助文本过时

**证据：** `typecheck` 帮助写“This build adapts no type-checking engine”（`cmd/ngm/typecheck.go:28-36`），但内置 catalog 已包含 `typescript`（optional，`internal/adapter/catalog.go:207-217`），且 `ngm typecheck --engine=typescript` 可用（需授权 `run:tsc`）。

**建议：** 更新帮助：说明内置 tsc 是 optional、需要 `run:tsc` 权限；给出可复制的配置片段。

#### F15. `verify` 汇总计数在 critical+operational 同时出现时不一致

**证据：** `Summarize` 在 `d.Err != ""` 时 `continue`，不统计 `DriftKind`（`internal/core/verify/report.go:228-249`）；`ExitCode` 则先看 critical（`report.go:192-223`）。

**影响：** `summary.critical` 可能为 0，而 `exitCode` 为 2；人读汇总行写“incomplete”，掩盖“已确证篡改”。

**建议：** `Summarize` 同时统计 `DriftKind` 与 `Operational`（两者不是互斥），或明确优先级并在汇总行显示“critical（另有 N 条未完成）”。

#### F16. 公开的 token 承诺与代码不符

**证据：** README `README.md:141` 与 `docs/architecture/security-model.md:252` 写“never reads / 不读取 token”；`internal/core/git/auth.go:25-49` 会读取 `GITHUB_TOKEN` 等值用于脱敏；`env.go:111` 构造 secrets。

**影响：** 安全审查会把它读成“代码与声明不一致”；虽然不是认证用途，但“永不读取”应改为“只读取用于输出脱敏，不参与认证、不落盘”。

**建议：** 修改公开措辞；在 `auth.go` 注释里明确“read-for-redaction”边界，并加一条测试证明值不被写入 lock/vendor/日志。

### P3（边界硬化/可维护性）

| 编号 | 问题 | 证据 | 建议 |
|---|---|---|---|
| P3-1 | `cat-file --batch` 的 size 无上限，可巨额分配 | `internal/core/git/catfile.go:130-136` | 加最大对象大小；超过即报错 |
| P3-2 | `ContentStore.Has` 不验证 blob 存在/内容；`Put` 复用已存在 blob | `content.go:120-135`、`content_v2.go:223-225` | `Has` 增加 blob 存在性检查，或默认 verify 增加“清单→blob”完整性 |
| P3-3 | `Compile` 的 `--outfile` 不校验产物存在（Bundle 已校验） | `internal/adapter/subprocess.go:257-277` | 与 Bundle 同样 `os.Stat`，否则报错 |
| P3-4 | `transform` 的 stdin `io.ReadAll` 无上限 | `cmd/ngm/transform.go:171-183` | 用 `io.LimitReader` 或显式上限 |
| P3-5 | `severitySummary` 排序破坏严重度顺序 | `internal/supplychain/report.go:114-130` | 不要 `sort.Strings`；按固定顺序输出 |
| P3-6 | `ngm add` 的 `#path` 中 `@` 会被当 ref 分隔 | `addspec.go:53-58` | 与 F4 一起修 |
| P3-7 | `ReadFileAtCommit` 的 path 被当 pathspec | `internal/core/git/blob.go:28` | 用 `--literal-pathspecs` 或先 `ls-tree` 精确匹配 |
| P3-8 | `integrations` 的临时文件名 `abs+".ngm-tmp"` 可能与用户文件冲突 | `internal/integrations/scaffold.go:264` | 用 `CreateTemp`/唯一后缀 |
| P3-9 | `modules index` 默认写 `ngm-index` 分支，可能移动当前分支 | `cmd/ngm/modules.go:804-937` | 若当前分支等于目标 branch，拒绝或提示 |
| P3-10 | `action.yml` 的输出退出码列表没有 5 | `action.yml:61` | 补 5（`--sandbox`/Deno 缺失） |
| P3-11 | `verify --json` 的 summary 与 checks 语义可再收紧 | `report.go` | 与 F15 一起修 |
| P3-12 | `collectSource` 的 remote 解析失败静默 | `cmd/ngm/modules.go:790-795` | 解析失败时输出 warning，而不是静默省略 |

---

## 7. 安全模型：真实防线与真实缺口

### 7.1 做得好的部分（可核对）

| 攻击路径 | ngm 的防线 | 证据 |
|---|---|---|
| 依赖内 `.npmrc` 被解释为配置 | ngm 不解析 npm 配置 | 全仓库无 `.npmrc` 读取；依赖内 `.npmrc` 只作为文件进入 digest |
| 依赖 postinstall 执行任意代码 | 默认 deny；allow 时只在 Deno 沙箱跑 JS 钩子；npm shell 钩子检测但不执行 | `cmd/ngm/postinstall.go`、`internal/security/sandbox.go` |
| 依赖替换 git 二进制 | 唯一构造点 `newGitCommand`，`run:git` 门禁；无配置键改二进制 | `internal/core/git/spawn.go` |
| 引擎 command 经 shell 注入 | `splitCommand` 不经过 shell；引号只做切分 | `internal/adapter/catalog.go:120-154` |
| vendor 路径/符号链接逃逸 | 条目路径 + symlink 目标双重预检；verify 共用判据 | `internal/core/vendor/links.go:262-279` |
| 大小写折叠丢文件 | 真实磁盘探测 + fail-closed | `links.go:281-340` |
| Windows 管道挂起 | `cmd.WaitDelay`；Deno/git 共用 | `internal/core/git/spawn.go:42`、`security/sandbox.go:282` |
| 子进程输出打爆内存 | `boundbuf` 1 MiB 上限 + 截断标记 | `internal/boundbuf/boundbuf.go` |

**评价：** 这些防线不是“文档承诺”，而是有单一构造点、有负测试、有真实工具验证的实现。尤其 `newGitCommand` 单点、`splitCommand` 无 shell、`EscapesPackageTree` 双端复用，是同类项目中少见的工程纪律。

### 7.2 真实缺口

| 缺口 | 影响 | 优先级 |
|---|---|---|
| `vendor.globalPath` 项目可控 | 写到项目外/覆盖目标子树 | P1 |
| `mappings.to` 可用 `../` 绕过 | build alias 指向 vendor 外 | P1 |
| 模块 gitlink 未拒绝 | 错误落地 + verify 不可完成 | P1 |
| 短 commit 误报 drift | CI 假失败/绕过判断 | P1 |
| Deno 沙箱无内存/CPU 上限 | 不可信脚本可耗尽资源 30s | P2 |
| 沙箱静态 `file:` 导入未实证 | 可能读取允许目录外文件（需攻击面测试） | P2 |
| token “永不读取”表述不实 | 审计/合规口径不一致 | P2 |
| OSV 覆盖率有限、无恶意包扫描 | “无已知漏洞”不等于安全 | P2（能力边界） |
| committer date 可伪造 | 晾晒期可被绕过 | P2（能力边界） |
| 项目 `ngm.engines.json` 可替换引擎 | 用户授权后执行项目指定命令 | P2（信任模型） |
| 默认浅 verify 不检测等长篡改 | 结构一致 ≠ 字节一致 | P3（已提示） |
| 无 SBOM 许可证、无 secrets 扫描 | 不能替代合规/安全产品 | 能力边界 |

### 7.3 与 PackageGate / SLSA 的关系

- **PackageGate** 验证了 ngm 的产品假设：Git 依赖是真实攻击面，npm 官方拒绝修 `.npmrc` 路径。ngm 在这条具体路径上确实由构造不可能。
- **但“免疫 PackageGate”不等于“Git 依赖安全”**：上游被攻破、恶意 commit、用户自己授权的引擎、Deno 运行时漏洞、OSV 覆盖不足都仍在。
- **SLSA L3 与 ngm 正交**：npm 证明“谁构建了这个 tarball”，ngm 证明“这份内容是不是锁定 commit 的字节”。2026 年的 Mini Shai-Hulud 事件说明：**有合法 SLSA L3 provenance 的包仍可以是恶意的**。因此两者都需要“来源 + 内容 + 策略 + 恶意检测”，单靠任一轴都不够。
- **ngm 的天然位置是“内容侧 L2 的一半”**：有内容↔commit 绑定，但没有签名；`ngm attest` 已接上 in-toto/SLSA 谓词，签名交给 cosign 等标准工具，这个定位是诚实的。

---

## 8. 性能与资源

### 8.1 项目自测读数（`docs/internals/metrics.md`）

环境：Windows 10 / i3-10100 / 8 逻辑核 / Go 1.27.1 / git 2.56；本地 fixture，无网络。

| 操作 | 目标 | 实测（中位/极值） | 结论 |
|---|---|---|---|
| cold install（10 依赖） | <5s | 1.88–1.94s | 达标 |
| warm install（10 依赖） | <500ms | 12ms | 达标 |
| warm install（100 依赖） | <500ms | 119ms | 达标 |
| verify 100 依赖（在线） | <3s | 2.556s（2.201–3.068s） | 贴线，方差跨目标 |
| verify 100 依赖（离线） | <5s | 2.167s（1.740–2.328s） | 达标 |
| verify --deep 100 依赖（离线） | 参考 | 2.478s | 参考 |
| archiveDigest 100MB | <1s | 434–457ms | 达标 |

**注意：** 这些是项目自测，不是本轮独立复测；cold install 是本地 fixture，不是真实公网 clone。真实世界性能会被 Git host、网络、仓库大小主导。

### 8.2 架构层面的性能事实

- `verify` 的主要成本是 git 子进程启动：commit 型 2 spawn/依赖，tag/branch 型 3 spawn/依赖；`DefaultConcurrency=8` 是实测拐点。
- digest 重放的瓶颈是 `ls-tree` + `cat-file --batch`，不是哈希；项目已用并行哈希把 `--deep` 提升约 2.09×（2000×8 KiB）。
- content v2 的 blob 去重把跨 commit 放大比从 20.0× 降到 0.6×；但 store **只增不减**，只有 `prune --orphans` 能回收无人引用 blob，没有按项目可达性的 GC（这是有意的安全取舍）。
- vendor 默认 `auto` hardlink；跨卷/不支持时整树降级 copy，实际 mode 会报告。
- `tree`/`why` 的路径展开在上限处截断；`why` 默认 64 条、100k 访问预算；这是防上游“宽图”造成指数爆炸的正确做法。

### 8.3 与竞品的性能位置

- 对 **registry 安装**：ngm 完全没有可比性；Bun/pnpm 的安装速度是另一个量级（2026 独立基准常见结论：Bun 最快，pnpm 是 Node 侧最快，npm 最慢）。
- 对 **Git 依赖的验证**：ngm 的 100 依赖约 2 秒、离线可重放，这个量级是合理的；但竞品没有等价功能，不能直接横比。
- **磁盘**：ngm 的 blob 池接近 pnpm content store 的思路；但 vendor 落地层是每项目一份（hardlink 缓解），跨项目共享不如 pnpm store。
- **二进制**：15.62 MB，单文件；比 Node 生态工具轻，但比原生小工具重；可接受。

---

## 9. 工程质量与交付成熟度

### 9.1 测试与 CI

**强项：**

- 三平台 matrix（Ubuntu/macOS/Windows）+ `go build` / `go vet` / `go test -timeout 40m`。
- 独立 `-race` job；独立 acceptance job（M1–M7 + 后续版本组）；独立 real-engine job（真实 esbuild/tsc/postcss/Deno 2.4），并**显式拒绝 skip**（避免“工具缺失 → 全 skip → CI 绿”）。
- gofmt、CRLF、PowerShell BOM、workflow YAML 解析、docs links、release/tag 对账、lock 字节复现，全部 CI 化。
- 测试数量 801，类型覆盖广；负测试与源码级判据（single spawn point、flag 表、字段接线）是亮点。

**局限：**

- 相当一部分测试是“源码文本/契约扫描”而非行为测试；它们能守住“规则写在唯一处”，但守不住“规则之间不碰撞”。本轮 8 组黑盒就找到了多个测试漏掉的真实路径。
- `cmd/ngm` 单包测试约 8 分钟；CI 40m 超时是必要补丁，说明测试偏慢。
- 没有 fuzz、没有 property-based、没有真实公网 Git host 的常规集成（部分 opt-in fixture）。
- 没有对“remove → install”“selective verify”“module gitlink”“mappings `../`”这些工作流的测试。

### 9.2 发布与分发

- 101 个 tag、60 个 GitHub release；API 核对显示 60/60 个 release 至少有一个资产，最新 `v0.64.0` 有 7 个资产（macOS/Linux/Windows 的 amd64/arm64 + SHA256SUMS）；本轮没有逐个核对每个 release 的资产数量。
- 最新 release 是 `v0.64.0`；v0.65–v0.101 只有 tag，没有 release（README 也承认 41 个 tag 无 release）。
- README 说“v0.57.0 是 GitHub 最新可下载”，但 GitHub API 显示 `v0.64.0` 有 7 个资产；这是文档内部矛盾（也可能是“可下载”定义不同，但未说明）。
- GitHub 0 star / 0 fork；Gitee 1 watcher；项目 2026-09-29 创建，尚无外部采用证据。
- `action.yml` 是 composite action，要求 `ngm` 已在 PATH；不自动下载二进制。这是供应链上的正确选择，但也提高了采用门槛。

### 9.3 维护性

- 单一作者、32 天、361 commits、101 tags：极高的个人产出，但没有第二双眼睛；bus factor = 1。
- 代码注释大量记录“某版本某评估报告修了什么”，可读性对作者友好，对新维护者是负担；部分注释已经与代码漂移（本报告 F13/F14/F16）。
- 41 个 ADR + 每版计划/复盘，是很好的决策档案；但文档 2.49 MB 对 30k 生产代码，学习成本高。
- `internal/core` 抽取与 adapter 分层是正确的长期结构；但 `cmd/ngm` 仍有 1128 行的 `modules.go`、605 行的 `install.go`，可以继续拆分。

---

## 10. 竞品全维度对比

> 说明：下表是 2026-11 的能力观察，符号含义：**强** = 一等公民/默认；**中** = 有但不完整/需配置；**弱** = 基本没有；**—** = 不适用。具体细节以各官方文档为准。  
> 竞品版本参照：npm 12.x、pnpm 11.x、Yarn 4.x、Bun 1.4.x、Deno 2.9.x（2026 年公开资料）。

### 10.1 总矩阵

| 维度 | npm | pnpm | Yarn | Bun | Deno | vlt | ngm |
|---|---|---|---|---|---|---|---|
| Git 依赖一等支持 | 中 | 强 | 强 | 强 | 弱 | 中 | **强** |
| Git ref 类型声明（tag/branch/commit） | 弱 | 弱 | 弱 | 弱 | 弱 | 弱 | **强** |
| Git 依赖 commit 锁定 | 强 | 强 | 强 | 强 | 弱 | 中 | **强** |
| Git 依赖内容级摘要 | 弱（integrity=commit） | 中（store integrity，但 Git/codeload 有缺口） | 中（checksum） | 中（lock integrity） | 中（远程模块 integrity） | 中 | **强（archiveDigest/moduleDigest）** |
| ref 漂移检测/三态分类 | 弱 | 中 | 中（immutable） | 弱 | 弱 | 弱 | **强** |
| 完全离线可复现安装 | 中（cache） | 强（store） | 强（zero-install） | 中（cache） | 中（cache） | 中 | **强（store+vendor+frozen+offline）** |
| 内容寻址/磁盘去重 | 弱 | 强 | 强（cache） | 中 | 强（cache） | 强 | 强（blob 池） |
| registry 安装速度 | 慢 | 快 | 中 | **最快** | 快 | 快 | —（不做 registry） |
| 生命周期脚本默认安全 | 中（opt-out） | 强（默认阻止 + allowlist） | 中（可关） | 中（trustedDependencies） | 中 | 强 | **强（默认 deny + 沙箱）** |
| 不可信代码沙箱 | 弱 | 弱 | 弱 | 弱 | **强（权限模型）** | 弱 | 强（Deno，限钩子/自检） |
| 漏洞审计 | 强 | 强 | 强 | 中 | 强 | 强（含恶意扫描） | 中（OSV 按 commit，覆盖有限） |
| 最小发布年龄默认 | 中 | **强（24h 默认）** | **强（1d 默认）** | 弱 | **强（24h 默认）** | 强（24h） | 中（可配置，无默认） |
| 来源白名单/策略 | 中 | 强 | 强（approvedGitRepositories） | 中 | 中 | 强 | 强（hosts/repos + 晾晒期） |
| Git 签名验证/门禁 | 弱 | 弱 | 弱 | 弱 | 弱 | 弱 | **强（报告 + --require-signed）** |
| SBOM | 强（npm sbom） | 中 | 中 | 中 | 中 | 中 | 强（CycloneDX/SPDX） |
| in-toto/SLSA attestation | **强（SLSA L3）** | 中 | 中 | 中 | 中 | 中 | 中（内容侧 L2，未签名） |
| registry 生态 | **最强** | 强 | 中 | 强（npm 兼容） | 强（npm/JSR） | 强（npm 镜像） | 无 |
| monorepo/workspace | 强 | **最强** | 强（PnP） | 强 | 强 | 中 | 中（模块集合，不做 workspace 解析） |
| 构建/类型/CSS 集成 | 弱 | 弱 | 弱 | 强（一体） | 中（bundle 实验） | 弱 | 中（adapter 调外部引擎） |
| Windows 支持 | 强 | 强 | 中（PnP/zip 边界） | 强 | 强 | 强 | 强（CI 三平台，但有 symlink 权限边界） |
| 发布/市场成熟度 | **最强** | 强 | 强 | 强 | 强 | 中 | **弱（v0.x、0 star、release 缺口）** |
| 学习曲线 | 低 | 中 | 中 | 低 | 中 | 中 | **高（权限 + ADR + Git-only）** |
| 审计交付物（离线/可提交） | 中 | 中 | 中 | 中 | 中 | 中 | **强** |
| 多语言/扩展性 | 弱 | 弱 | 弱 | 中 | 中 | 中 | 弱（JS/Deno；core 语言中立但入口仍窄） |

### 10.2 逐个竞品的真实位置

#### npm

- **优势**：默认选择、生态最大、workspaces、`npm sbom`、`npm audit`、`npm audit signatures`、Trusted Publishing/OIDC、SLSA L3 provenance；2026 年 8 月起发布强制 2FA。
- **Git 依赖短板**：只锁 commit；`package-lock.json` 对 Git 依赖的 `integrity` 是 commit SHA，不是内容树哈希；`.npmrc`/Git 路径仍是攻击面（PackageGate 中 npm 拒绝修）。
- **对 ngm 的含义**：npm 是“registry 标准答案”，ngm 不应正面竞争；npm 的 provenance 是 ngm 的正交互补轴。

#### pnpm

- **优势**：content-addressable store、硬链接/符号链接、workspaces、磁盘效率标杆；pnpm 11 默认 `minimumReleaseAge=1440`、`blockExoticSubdeps=true`、`strictDepBuilds=true`、`trustPolicy` 可配、`onlyBuiltDependencies`；安装速度快。
- **短板**：Git 依赖的内容完整性仍不完整（CVE-2026-48995 codeload tarball 不受 lock 哈希约束；CVE-2025-69264 Git 依赖生命周期脚本绕过；CVE-2026-50014 lockfile commit 传给 git fetch 缺少校验）；没有 refType/漂移三态/按 commit 的 OSV。
- **对 ngm 的含义**：pnpm 是“磁盘 + 默认安全”的强竞品；ngm 若只讲“内容寻址”会被 pnpm 压住，必须讲“Git 内容证明 + 漂移分类 + 离线审计”。

#### Yarn

- **优势**：PnP/zero-install、`approvedGitRepositories`、`enableImmutableInstalls`、`npmMinimalAgeGate`（4.12 默认 1d）、workspaces、成熟生态。
- **短板**：PnP 兼容性边界；Git 依赖仍是 commit + checksum 层，没有 refType/内容重放/漂移三态；Git 签名不在一等流程。
- **对 ngm 的含义**：Yarn 的 Git 审批很强，但“审批来源”与“证明内容”是两件事；ngm 可在“内容证明”上补位。

#### Bun

- **优势**：最快（官方宣称 25× npm；2026 独立基准普遍把 Bun 排第一）、单二进制、install/bundler/test/runtime 一体、npm 兼容、trustedDependencies、workspaces。
- **短板**：安全/供应链能力相对年轻；Git 依赖的内容证明、漂移分类、审计交付物不是重点；版本迭代快。
- **对 ngm 的含义**：Bun 证明了“速度/一体化”已经商品化；ngm 不能以“快”为卖点，只能以“证明/审计”为卖点。

#### Deno

- **优势**：default-deny 权限模型、`deno audit`、`minimumDependencyAge`（2.9 默认 24h）、npm/JSR、workspaces、安全沙箱文化。
- **短板**：Git 依赖不是一等公民；没有 commit + 内容树 digest 的 Git 依赖证明；bundle 仍实验。
- **对 ngm 的含义**：Deno 是“运行时安全”的标杆，ngm 借用了它的权限哲学；但 Deno 的 npm/JSR 生态会吸走大部分“安全敏感”用户。

#### vlt

- **优势**：npm 兼容、registry 侧恶意包拦截、`vlt query ':malware'`、安装脚本保护、24h 新版本隐藏、policy/security 默认。
- **短板**：registry-centric；Git 依赖证明不是核心；新项目、生态小。
- **对 ngm 的含义**：vlt 证明“默认安全 + registry 扫描”是市场方向；ngm 的 Git 证明与其互补，不是替代。

#### 供应链工具（OSV-Scanner / Snyk / Socket / Phylum / Syft/Grype / cosign / SLSA）

- 这些是 ngm 的**互补层**，不是竞品：OSV/Snyk 做漏洞与恶意包检测，Syft/Grype 做 SBOM/扫描，cosign 做签名，SLSA 做 provenance。
- ngm 的 `audit`/`sbom`/`attest` 是“把 Git 依赖事实导出给这些工具”的入口；它不替代恶意包扫描、许可证合规、秘密扫描。

### 10.3 市场位置判断

| 问题 | 判断 |
|---|---|
| ngm 是否替代 npm/pnpm/Yarn/Bun/Deno？ | **否**。没有 registry、workspace 解析、通用安装语义。 |
| ngm 是否在“Git 依赖证明”上领先？ | **是**，但领先的是“组合闭环”，不是每个单项。npm/pnpm/Yarn/Bun/Deno 都有部分控制点，没有一个把 refType + 内容 digest + 漂移三态 + 离线重放 + 模块证明 + SBOM/attestation 串成默认流程。 |
| ngm 的最大竞争者是？ | **“什么都不做”**：大多数团队用 lockfile + Dependabot + npm audit 已经够用；ngm 需要额外的权限配置、Git-only 依赖模型和审计流程。 |
| ngm 的最强采用入口？ | `ngm adopt` + GitHub Action：不换安装器，先对现有 `node_modules` 出证明；这是唯一不要求“先迁移”的路径。 |
| ngm 的最大商业风险？ | 0 外部用户、release 缺口、单作者、v0.x 版本号膨胀；没有证据表明市场愿意为“Git 内容证明”单独付费/迁移。 |

---

## 11. 评分卡

| 维度 | 权重 | 得分 | 依据 |
|---|---:|---:|---|
| 架构与设计一致性 | 15% | 8.5 | 分层清晰、ADR 完整、core/adapter 解耦、单一构造点 |
| 代码质量与可维护性 | 15% | 7.5 | 防御式编程、错误模型统一；注释考古、单作者、巨型命令文件 |
| 核心功能正确性 | 20% | 6.0 | F1/F2/F3/F4 是核心路径缺陷；主链路其余部分扎实 |
| 安全设计 | 20% | 7.0 | 默认拒绝、沙箱、路径防护强；F5/F6 与 token 表述是缺口 |
| 性能与资源 | 10% | 8.0 | Git 依赖验证 2s/100 deps；非 registry 场景不可比 |
| 兼容性与生态 | 10% | 3.0 | 无 registry、workspace 解析；adopt 只支持 npm lock |
| 测试/CI/文档 | 5% | 8.5 | 801 测试、三平台、race、真实引擎；黑盒覆盖不足 |
| 交付/市场成熟度 | 5% | 4.5 | 0 star、60/101 release、v0.x、单作者 |
| **加权总分** | 100% | **6.3** | 工程优秀，产品/市场早期 |

**分场景评分：**

- 作为“Git 依赖证明/审计层”：**7.0/10**（核心闭环可用，缺陷可修）。
- 作为“企业 CI 安全门禁”：**6.5/10**（需要先修 F1/F2/F3/F5/F6）。
- 作为“通用 JS 包管理器”：**3.0/10**（不适用）。
- 作为“开源项目/社区产品”：**4.0/10**（无采用、无 release、单作者）。

---

## 12. 改进路线图（按优先级）

### 第 1 阶段：修核心承诺（P1）

1. **remove/install 语义闭环**：`install` 检测 lock 孤儿条目；`remove` 后要么触发 fresh，要么明确拒绝并提示 `ngm update`。
2. **模块 gitlink 拒绝**：`BuildModuleMaterials` 调用 `validateTreeEntry`；补模块负测试。
3. **短 commit 全链路补齐**：verify 在线/离线都用 mirror 补齐；补短 commit CI 验收。
4. **ref 解析支持 `/`**：修 `ParseAddSpec`；加 `feature/x`、`release/1.0` 测试。
5. **vendor/mappings 路径边界**：`vendor.globalPath` 限制到用户级配置/默认目录；`mappings.to` 做 canonical 校验；`mappingsAlias` 消费前校验。

### 第 2 阶段：修正确性与体验（P2）

6. **选择性 verify**：过滤时裁剪 `RequiredBy`，或 `Run` 跳过边校验。
7. **vendor 所有权与清理**：维护本次拥有的路径清单；空依赖/删除依赖时清理旧 vendor/mappings。
8. **重复根声明**：配置校验拒绝；或解析时报冲突。
9. **默认协议统一**：builtin 与实际默认二选一；`config show` 不再说谎。
10. **purl 标准化**：GitHub/GitLab/Gitee/generic 分流；补 purl 解析测试。
11. **tree 用 lock 图**：优先 `GraphFromLock`；更新帮助文本。
12. **文档对账**：quickstart 的 `run:esbuild`、README 的 v0.57/v0.64、token 表述、typecheck 帮助。

### 第 3 阶段：能力与生态（P3/产品）

13. **adopt 扩展**：解析 pnpm-lock.yaml / yarn.lock（可引入 YAML 解析或调用安装器导出），把“零切换”做成真正的入口。
14. **默认安全策略**：考虑 `minimumReleaseAge` 默认 24h（可 opt-out），与 pnpm/Deno/Yarn 对齐；保持 ngm 的“默认拒绝”风格。
15. **SBOM/attestation 签名示例**：cosign 集成文档 + CI recipe；不要自造签名。
16. **发布收敛**：停止每个小改动打 tag；建立稳定版本节奏；补齐 release 资产或删除无产物 tag；提供安装脚本/包管理器分发。
17. **贡献者与治理**：第二维护者、security policy、issue/PR 模板、roadmap；单作者是最大运营风险。
18. **性能/规模**：在真实 100 仓库 + 公网 Git host 上复测；记录 p50/p95；对 `tree/why` 的指数上限做压力测试。

---

## 13. 最终结论

**ngm 的价值是真实的，但它目前的价值是“证明层”，不是“包管理器”。**

- 它把 Git 依赖从“一个 commit 字符串”提升为“声明 + commit + 内容 digest + 漂移分类 + 离线重放 + 审计产物”的可验证对象；这是 npm/pnpm/Yarn/Bun/Deno 都没有完整提供的组合。
- 它的工程实现明显高于同类早期项目：单点 git 构造、路径/符号链接防护、内容寻址 store、默认拒绝沙箱、三平台 CI、801 测试、完整 ADR。
- 但它也有早期项目典型的问题：核心路径有可复现 bug，安全边界有绕过，文档与实现漂移，版本号膨胀，发布产物不完整，零外部采用。
- **如果今天就要用**：把它当作“Git 依赖审计/CI 门禁”的窄工具，先在非关键流水线试用 `verify`/`audit`/`sbom`/`adopt`；不要把它当作 pnpm/npm 的替代安装器。
- **如果要做成产品**：先修 P1（remove/module/short commit/ref slash/mappings/globalPath），再把 `adopt` 扩展到 pnpm/yarn，收敛发布与治理；否则它会更像一份“高质量的安全工程论文”，而不是一个可采用的工具。

> 一句话：**这是一个值得认真对待的 Git 依赖证明层；它的敌人不是 pnpm/Bun，而是自己的完成度与市场验证。**

---

## 14. 本轮 P1 修复记录（2026-11-01 后续）

本报告初稿完成后，按 P1 清单修复了 6 个核心缺陷。下表是**修复后的当前状态**，不是初稿的原始读数：

| 编号 | 修复内容 | 关键改动 | 回归验证 |
|---|---|---|---|
| F1 | `remove` 后 `install` 不再保留旧依赖 | `lockManifestMismatch` 检测“lock 有、声明没有”的根；`installFresh` 重新解析；`cleanupStaleVendor` 删除旧本地 vendor 目录；`update` 空依赖也清理 vendor/mappings | 更新后的 `TestM3Acceptance/remove_edits_declaration_only`；黑盒 `remove → install` 后 lock 为空、vendor 目录消失 |
| F2 | 模块 gitlink 不再被当普通文件落地 | `BuildModuleMaterials` 对每个非 tree 条目调用 `validateTreeEntry`；`validateEntryPaths` 拒绝 gitlink/tree 条目 | `TestBuildModuleMaterialsRejectsGitlink`；`v066_safety` 的 gitlink 子测试；黑盒模块 update 现在 exit 3 并点名 gitlink |
| F3 | 短 commit 不再自反漂移 | `expandShortCommit` 在 `MirrorDir` 中补齐短 hash；在线 commit 型传入 `MirrorDir`；`verify.checkRef` 在对象库中再次补齐，补不齐算 operational 而不是 drift | `TestP1ShortCommitVerifyResolvesThePinnedCommit`；黑盒短 commit verify 现在 exit 0 |
| F4 | `ngm add` 支持带 `/` 的 ref | 新增 `splitRefSuffix`：区分 URL userinfo 与 ref；ref 允许 `/`，仍拒绝 `:` | `addspec_test` 新增 `feature/x`、`release/1.0`、userinfo 用例；黑盒 `ngm add ...@feature/x` 成功 |
| F5 | mappings `to` 不能逃出 vendor | `hasDotDotSegment` 拒绝 `..`；`filepath.Rel` 规范化后判定边界；`ngm build` 读取 mappings 前复用同一校验 | `TestP1MappingsToCannotEscapeTheVendorRoot`；黑盒 `mappings validate` 对 `../../outside` 现在 exit 3 |
| F6 | 项目清单不能把 vendor 写到项目外 | `validateVendorLocation`：只允许 `~/.ngm/global-vendor` 或其子目录；`ReadManifest` 与 `config validate` 共用；`update` 不会因为坏 lock 阻塞清理 | `TestP1ProjectGlobalPathStaysInNgmGlobalVendor`；黑盒 `vendor.globalPath=<项目外>` 现在 exit 3，outside 目录不存在 |

**修复后的判断变化：**

- 6 个 P1 已在当前工作树中修复，新增/更新了 6 组回归测试；`go build ./...`、`go vet ./...` 与相关包测试通过。
- **`-race` 已补齐**：CI 的 race job 显式加 `-timeout 40m`（默认 10m 会把 `cmd/ngm` 的慢误读成失败）；本机用临时便携 MinGW 跑通 `go test -count=1 -race -timeout 40m ./...`（`cmd/ngm` 约 477s）。首轮 `-race` 抓到两处**测试自身**的数据竞争（`graph_diamond_test.go` 的调用计数 map、`policy_gate_test.go` 的 ensured 切片），已加互斥锁；修复后整轮全绿，未发现生产代码竞争。
- 这不改变本报告对**产品成熟度**的判断：P2（选择性 verify 幽灵边、`modules index` 的 `source.repo`、默认协议文档漂移、purl 互操作等）仍在；发布、采用、生态问题不是代码修复能解决的。
- 评分上，**核心功能正确性**从 6.0 上调到约 7.5（P1 清零，P2 仍会扣分）；综合分仍建议保持在 6.5–6.8 区间，除非 P2 与发布问题也收敛。

---

## 附录 A：外部事实来源（2026-11 检索）

- npm Trusted publishing：<https://docs.npmjs.com/trusted-publishers/>
- npm 多重 trusted publishing 配置（2026-09）：<https://github.blog/changelog/2026-09-03-multiple-trusted-publishing-configurations-for-npm/>
- npm package-lock `integrity`（Git 依赖为 commit）：<https://docs.npmjs.com/cli/v8/configuring-npm/package-lock-json/>
- npm audit signatures：<https://docs.npmjs.com/cli/v10/commands/npm-audit/>
- pnpm 11.0 发布说明：<https://pnpm.io/blog/releases/11.0>
- pnpm supply chain security：<https://pnpm.io/supply-chain-security>
- pnpm minimumReleaseAge 设置：<https://pnpm.io/settings/dependency-resolution>
- pnpm benchmarks：<https://pnpm.io/benchmarks/>
- Yarn Git protocol / approvedGitRepositories：<https://yarnpkg.com/protocol/git>
- Yarn security / npmMinimalAgeGate：<https://yarnpkg.com/features/security>
- Bun install / trustedDependencies：<https://www.bunjs.cn/docs/cli/install>
- Deno 2.9（min-release-age 默认 24h）：<https://deno.com/blog/v2.9>
- Deno 2.6（minimumDependencyAge）：<https://deno.com/blog/v2.6>
- vlt 从 pnpm 迁移：<https://docs.vlt.io/client/migration/from-pnpm>
- vlt 安全：<https://www.vlt.io/platform/security>
- SLSA 对 Mini Shai-Hulud 的边界说明：<https://slsa.dev/blog/2026/05/mini-shai-hulud-what-slsa-can-and-cannot-do>
- 各 registry 签名/attestation 对比（2026-10-30）：<https://nesbitt.io/2026/10/30/signing-and-attestations-across-package-registries.html>
- pnpm CVE-2026-48995（codeload tarball 无 lock 哈希）：<https://www.cve.org/CVERecord?id=CVE-2026-48995>
- pnpm CVE-2026-50014（lockfile commit 传给 git fetch）：<https://corgea.com/advisories/vulnerabilities/CVE-2026-50014>
- PackageGate 报道（中文二手源，项目文档自述更详细）：<https://cloud.tencent.com.cn/developer/news/3536407>
- CycloneDX 1.5 JSON schema：<https://cyclonedx.org/schema/bom-1.5.schema.json>
- PURL 规范（ECMA-427）：<https://ecma-tc54.github.io/ECMA-427/>
- PURL 支持类型（FOSSA）：<https://docs.fossa.com/docs/sbom/purl-support>
- 2026 包管理器对比（市场/性能参考）：<https://devtoolpicks.com/blog/pnpm-vs-npm-vs-yarn-vs-bun-indie-hackers-2026>
- npm/pnpm/Yarn 份额估计（2026 二手源）：<https://8devmx.com/comparacion/herramientas/npm-vs-yarn-vs-pnpm/>

## 附录 B：项目内证据索引

- 项目自评与能力矩阵：`docs/internals/capability-matrix.md`、`docs/internals/project-state.md`、`docs/internals/metrics.md`
- 竞品分析（项目版，2026-09-29）：`docs/COMPETITIVE-ANALYSIS.md`
- 安全模型：`docs/architecture/security-model.md`、`docs/architecture/trust-model.md`
- 关键代码：
  - `cmd/ngm/install.go`、`cmd/ngm/update.go`、`cmd/ngm/remove.go`
  - `cmd/ngm/verify.go`、`internal/core/verify/verify.go`、`internal/core/verify/report.go`
  - `internal/core/git/tree.go`、`internal/core/git/catfile.go`、`internal/core/git/spawn.go`
  - `internal/core/resolve/reftype.go`、`internal/core/resolve/addspec.go`、`internal/core/resolve/graph.go`
  - `internal/core/vendor/links.go`、`internal/core/vendor/content_v2.go`、`internal/core/vendor/content_maintain.go`
  - `internal/mappings/validate.go`、`internal/integrations/scaffold.go`
  - `cmd/ngm/modules.go`、`internal/sbom/sbom.go`、`internal/attest/attest.go`
- 本轮黑盒复现：remove/install、模块 gitlink、短 commit、带 `/` 分支、mappings `../`、重复根声明、quickstart build、globalPath 外写（详见第 6 节）。
