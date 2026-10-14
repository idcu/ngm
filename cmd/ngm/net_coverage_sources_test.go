package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.53：**"覆盖集从哪来"——把一句口号立成可查的纪律**（v0.46 §5.1 的产物）。
//
// v0.46 的结论是一句话：**判断标准不是"这版有没有抄"，而是"覆盖集从哪来"**。
// 当时它只是口号——而口号管不住下一个人（v0.42 的手抄表、v0.43 的手抄表、
// v0.45 的文件名派生、v0.46 的手抄表，四次都是"来源"出的问题）。
//
// 这一版把它变成一张**可查的表 + 两条机械判据**：
//
//	① 每个登记的网，其**文件**必须真的读**源码**或**运行时表**——
//	   用一组被认可的入口名认（`sourceFiles` / `flagsetBlocksOf` / `commands` /
//	   `surfaceCases` / `reportCases` / `repoRoot` …）。这些入口都是"从实现里取事实"。
//	② 若这张网的表是**手抄**的，那它必须在 `why` 里写清**为什么那不是覆盖集**
//	   （例如 v0.37 的夹具输入表：它描述的是"喂什么参数"，不是"查了几件事"）。
//
// 另外一条守卫：登记的键必须是**真的存在的测试函数**——否则那条登记什么也没看着。
type netSource struct {
	what   string // 这张网查什么（一句话）
	source string // 覆盖集从哪来
	why    string // 手抄时必须写清：为什么它不是覆盖集
}

// 认可的"从实现取事实"的入口（出现任一即算读过源码或运行时表）。
var derivationEntryPoints = []string{
	"sourceFiles(",        // cmd/ngm 下的源码文件清单
	"flagsetBlocksOf(",    // 按 newFlagSet 切块
	"boolFlagsOfCommand(", // 跨文件找某个命令的布尔 flag
	"valueFlagPairs(",     // 取值 flag 的派生表
	"subcommandBoolPairs(",
	"commands",     // 运行时命令表
	"repoRoot(",    // 源码路径
	"surfaceCases", // 错误面表（本身由夹具 + 命令名组成）
	"reportCases",
	"exitCodeMeasured",
	"matrixArgs",
	// v0.56 补：文档/源码两侧的派生函数名。它们与上面那些是同一类东西——
	// 判据的覆盖集从**实现或文档**读出来，而不是从我的记忆里抄。
	"readDoc(",                // 读一份文档（退出码契约那批）
	"parseJSONShapeTable(",    // 解析 cli.md 的《--json 的形状》表
	"jsonTagsOf(",             // 扫源码里的 json tag
	"structJSONTags(",         // 从具名类型解析顶层 json tag
	"livingDocs(",             // 活文档清单（doc 类判据的覆盖集）
	"implementationJSONTags(", // 全仓库 json tag 集合
}

var netCoverageSources = map[string]netSource{
	// ---- v0.56：把更早那批契约网也审一遍（v0.53 §4 的遗留）----
	//
	// 它们的覆盖集本来就是从实现/文档派生的，只是当年没收进这张表——
	// 于是"它从哪来"这件事没有任何判据看着（本版普查时逐个确认过）。
	"TestV21UsageExitCodeSectionsAreWellFormed": {
		what:   "每条 usage 里的退出码段落格式良好，且每个声明的码都被测量过或在缺口表里",
		source: "运行时命令表 `commands` 的 `Usage` 字段（就是实际打印的那份文本）",
	},
	"TestV22DeclaredCodesAreMeasuredOrKnownGaps": {
		what:   "声明的码 ∈ 已测量 ∪ 别处测量 ∪ 已知缺口——三处都不在即红",
		source: "`commands` × `declaredExitCodes(usage)`；三张判定表（`exitCodeMeasured`/`exitCodeElsewhere`/`exitCodeGaps`）双向对账",
	},
	"TestV24ExitCodeContractAgreesAcrossSources": {
		what:   "退出码契约在四处事实源上一致（实现常量 · p0-core · observability · cli 参考）",
		source: "`readDoc(` 读 `internal/errs/errs.go` 与三份文档，四份集合互为对账",
	},
	"TestV25JSONShapeTableMatchesTheImplementation": {
		what:   "`--json` 形状表里每个字段都能在实现的 json tag 里找到",
		source: "`parseJSONShapeTable(` 解析 cli.md 的表 × `jsonTagsOf(` 扫源码的 tag",
	},
	"TestV25ConfigFieldTablesMatchTheSchema": {
		what:   "配置字段表里每个字段都能在 schema 结构体上找到",
		source: "解析 configuration.md 的三张表 × `configSectionFiles` 映射（映射有单向守卫：文档新增一行而无映射即红）",
	},
	"TestV26EveryDocumentedShapeIsANamedType": {
		what:   "文档里每一行形状都指向一个**具名类型**（反向：映射条目必须仍有文档行）",
		source: "`parseJSONShapeTable(` × `structJSONTags(` × 映射 `jsonShapeRowTypes`（双向）",
	},
	"TestV26EveryFieldOfTheReportIsDocumented": {
		what:   "报告类型的每个顶层字段都被文档点名",
		source: "`structJSONTags(` 从源码解析 json tag",
	},
	"TestV27ProseJSONFieldNamesExist": {
		what:   "散文中提到的 json 字段名在实现里存在（早期靠白名单，v0.43 已改派生）",
		source: "`livingDocs(` 的文档 × `implementationJSONTags(` 全仓库 tag × AST 标识符集",
	},
	"TestV27EveryConfigFieldIsDocumented": {
		what:   "配置结构体的字段都被 configuration.md 点名（`vendor` 与全局节**明确不做**：那里的字段是自由的）",
		source: "`configReverseSections` 映射 + `structJSONTags(` 源码派生",
	},
	"TestV28UserFacingErrorsCarryAHint": {
		what: "`cmd/ngm` 里 `errs.New/Wrap` 的空 hint 字面量必须为零",
		why: "它的覆盖集是**文件系统**（`filepath.Glob(\"./*.go\")` + AST 扫全部非测试源码），" +
			"不是任何表或运行时集合——这条判据的广度来自『扫目录』，没有可指的表",
	},
	"TestV30ErrorsThatReachTheUserAreNotSilentAndCarryAHint": {
		what:   "每个可达的非零退出码都带着非空的成因与建议",
		source: "`exitCodeMeasured` 的全部非零条目（该表由 V22 的声明对账兜住）",
	},
	"TestV30EmptyHintCeilingIsARepositoryWideRatchet": {
		what:   "全仓库空 hint 站点数的棘轮（当前 118）",
		source: "遍历全仓库非测试 `.go` 的 AST（`repoRoot(` 定位）",
	},
	"TestV32EveryFailureReportSaysWhatToDoNext": {
		what:   "每条失败报告都说清下一步（两种行动行约定都要被走到）",
		source: "夹具表 `reportCases`（11 条，每条带 `remedies`）",
	},
	"TestV33ActionLinesNameSomethingExecutable": {
		what:   "行动行点名的东西真的存在：命令在命令表里、配置键在 schema 里",
		source: "被扫对象是 `reportCases`；**核对锚点是派生的**——命令来自运行时 `commands`，配置键来自 `repoRoot(` + schema 文件",
	},
	"TestV34AdviceMatchesTheFailureShape": {
		what:   "建议与失败的形状匹配（同一形状的所有用例都要有行动行）",
		source: "夹具表 `reportCases` 的 `remedies`",
	},
	"TestV35OperationalFailuresAdviseTheirOwnCause": {
		what:   "同一分支下不同成因的建议必须**两两不同**，且每种成因都被承认",
		source: "夹具表 `reportCases`——v0.56 起**家族成员由命名前缀派生**（`verify/检查未能完成（…）`），两个方向都对账：多了成因没登记即红、登记指向空气即红",
	},
	"TestV36EveryFailingReportCarriesANextStep": {
		what:   "扫描式核对：每份失败报告都带一行下一步",
		source: "`exitCodeMeasured` 非零条目 ∪ `surfaceCases` 非零条目的并集",
	},
	"TestV39ArgumentDimensionHoldsItsContracts": {
		what: "无参数与垃圾参数两个维度下每个命令的通道与退出码",
		source: "运行时 `commands` × `matrixArgs`（每命令参数表，V37 双向对账）× " +
			"`argumentDimensions`（v0.65 提成命名变量，每个维度自带一句契约）",
	},
	"TestV40ArgumentSpellingHoldsItsContracts": {
		what:   "参数拼法（顺序 · `--` · 重复）在真实命令表上保持一致",
		source: "运行时 `commands` × `matrixArgs`；跳过项必须公开记入日志并有下限守卫",
	},
	"TestV64SchemaFilesAreDerivedFromDocsAndSource": {
		what: "`schemaFiles` 那份手写清单必须与两处事实源对得上（文档的配置表 × 定义 schema 的源码）",
		source: "文档侧读 `docs/guides/configuration.md` 的配置文件表（`readDoc(`）；" +
			"代码侧遍历非测试 `.go`（`nonTestGoFiles(`）并复用 `fieldWithTag` 同形的 json tag 判据",
	},
	"TestV94SubPathEndToEnd": {
		what: "**`subPath` 端到端**（从 v0.79 起挂在「需要推一个 monorepo 远端库」上的那件事）：真实 monorepo（根 + `packages/a` + `packages/b`）⇒ `init → add git://…#path=packages/a@v1.0.0 → update --all --offline → install --offline → verify → mappings validate`，然后断言契约——**只有子包被 vendor**（兄弟包一个文件都不许进来）· 声明里 `path: packages/a` · mappings 指向子包自己的 `exports`（`src/index.js` + `src/index.d.ts`，且不含 `packages/b`）· 反面：不存在的子路径在 `update` 那一步明确失败且**不留半个 vendor**",
		why: "这条链路一直被记为「等一次人工推送」。这一版把它做掉，同时纠正了前提里**两处**想当然：①「起个本机 `git daemon` 就有远端」**不成立**——`git://` 只是地址写法，抓取协议只有 ssh / https（`~/.ngm/config.json` 的 `git.defaultProtocol`，见 `internal/resolve/protocol.go`）；" +
			"v0.83 的 ADR-029 放开回环主机是为了本机 daemon，但那一半之后还有一半：实测 install 会把 canonical 重建成 `https://127.0.0.1/…` 去连 443。②但 **subPath 的契约不在抓取那一步**——过滤子目录、写 `path`、按子包自己的 `exports` 推入口，全都发生在 vendor / mappings 层 ⇒ 用仓库既有的**本地镜像**（镜像是**整仓**，正是 monorepo 的真实形状）+ `--offline` 就能把契约走完整。" +
			"顺带记下三条实测细节（都在测试注释里）：显式端口被设计拒绝（「ngm identifies a repository by host and path only」）· 路径至少两段（`org/repo`）· Windows 上 `git daemon` 会 exec 出 `git-daemon.exe`，只杀父进程会留下占着 9418 的残骸",
		source: "判据在 `cmd/ngm/v094_subpath_e2e_test.go`；夹具用 `testutils.NewGitRepo` + `seedMirror`（既有的本地镜像设施），CLI 走 `dispatch`",
	},
	"TestV91DocsLinksGoldenCases": {
		what: "文档链接检查的**黄金用例**（25+ 个刁钻形态）：反引号里的示例不算链接 · 一行里两条都要看到 · `..` 一直退到根 · 根目录文件（base 为空）拼路径不能带前导 `/` · 括号出现在 label / target 里（**v0.92 起 target 里的括号能被正确解析**，旧实现把它切碎 ⇒ 一处已知漏检就此关上）· **v0.93 起目标不限于 `.md`/`.txt`**（源码 / `LICENSE` / 目录都查；`.md`/`.txt` 仍要求是**文件**，其余允许目录；`vendor/` 等被忽略目录里的目标不算）· 子目录 · `.txt` · `vendor/` 不参与",
		why: "v0.90 把每条链接一次 `cd … && pwd` 换成纯 bash（133.72s → 33.07s），v0.91 再把「剥反引号 + 找链接 + 提 target + 规范化」整体搬进**一个 awk 进程**（33.07s → **1.33s**，实测）。重写的凭据是当时与旧实现的**逐字对照**——但那次对照是一次性的；这条网就是它的常驻版本（写它时正好验证了：我手写的期望行号是错的、而两个实现一致——" +
			"出问题的是我的期望，不是实现）。**v0.92 在这条网上补上了原先写着的那处已知漏检**：destination 改按**平衡括号**解析（`](./b(c).md)` 的结尾是最后一个配平的 `)`），" +
			"旧实现「截到第一个 `)`」把它切成 `c` ⇒ 既不报也看不见；动手前先量（全仓 253 个 `.md` 里这类链接 **0 处** ⇒ 变强不会引入新红），改完做前后对照（同一夹具旧报 1 条、新报 2 条；而「带括号且**存在**」两侧都不报 —— 证明不是「见括号就报」）。" +
			"**v0.93 又把范围扩到所有仓库内目标**：此前只查 `.md`/`.txt`，于是把 `LICENSE` 改名、把 `internal/errs/errs.go` 挪走，文档里的链接会**静默**烂掉（全仓 16 处这类目标：3 个 `.go` + 13 个目录链接）。同样先量（2103 处链接全部存在 ⇒ 0 处新红）再做前后对照（旧 2 条 / 新 4 条，多的是 `.go` 缺失与目录缺失；「存在的非 md/txt」两侧都不报）",
		source: "判据在 `cmd/ngm/v091_docs_links_golden_test.go`；实现在 `scripts/check-docs-links.sh`（`link_scan` 一个 awk 进程 + bash 只做 `[ -f ]`）",
	},
	"TestV91GiteeReadingRunsOffline": {
		what: "Gitee 侧读数**离线可测**：对着假 API 跑三种结论——附件齐 ⇒ exit 0 且报 `OK (7 attachments …)` · 缺附件 ⇒ exit 1 且**点名**缺的是哪一个 · 请求失败 ⇒ exit 3 且写 `NOT CHECKED`（查不动既不报缺也不报成功）；并顺带跑一遍 Windows 入口，要求同一份实现的同一个结论",
		why: "它一直在登记表里躺着，理由写的是「没有离线模式（无 --api-base）」——**那条理由是错的**：实现里一直有 `GITEE_API_BASE` 环境变量。写下理由时没人验过它，接进判据只花了两分钟。" +
			"于是登记表只剩最后一条（`upload-gitee-assets.ps1`：真需要凭据）。夹具也顺手记下一条：假 API 要照着实现的判据造——第一版没写 `id`，三条断言全错成 MISSING（脚本的规则是「没有 id = 没有 release」）",
		source: "判据在 `cmd/ngm/v091_gitee_offline_test.go`；实现在 `scripts/check-gitee-release-status.sh`（`GITEE_API_BASE` 早已存在）",
	},
	"TestV90PowerShellWrappersRunTheirImplementation": {
		what:   "**入口真的跑一次**：五个 `.ps1`（`check-docs-links` · `check-vendored-standard` · `check-mirror-coverage` · `check-release-status`（v0.90 新加的入口）· `write-readings-summary`）各跑一次，且每个都有**两条断言**——① 能跑；② 用**必然失败的输入**验它的退出码与点名真的传了回来（不合格的 vendored 夹具 ⇒ exit 1 · 缺 tag 的仓库 ⇒ exit 1 · 假 API 的读数必须出现在输出里 · `-Out` 必须真的写成文件且是追加）",
		why:    "v0.89 的登记表把「没人跑」变成必须显式登记的事实；v0.90 接着把它**变短**——登记表是待办清单，不是永久豁免（5 条 → 2 条）。只验「退出 0」的网会被一个 `exit 0` 的坏包装器骗过去，所以每个入口都用反面输入验一遍",
		source: "判据在 `cmd/ngm/v090_ps1_wrappers_run_test.go`；假 API 与合成仓库见同文件的 `syntheticDeliveredRepo`（v0.86/v0.88 各自内联过同一段夹具）",
	},
	"TestV90WrapperOptionsParity": {
		what: "**入口不许吞参数**：同名 `.ps1` 必须**带引号地转发** `.sh` 自己接受的每一个长选项（只取 `case \"$1\" in` 块里的 `--xxx` 分支，不含脚本内部调用别的工具用的选项）；没有 `.ps1` 的脚本必须在 `noWindowsEntry` 里写明原因",
		why: "实测抓到 `check-mirror-coverage.ps1` 只声明 `-Repo`、**不转发 `--api-base`**——而那个选项存在的全部理由就是「给判据指一个本地假 API」，于是 Windows 上做不了同样的验证：「唯一实现」的纪律在这里破了（实现支持、入口不支持，两者就不是同一个东西）。" +
			"另有两处漏了 `--help`。**第一次写这条网时它被自己的注释骗过**（查的是「文本里有没有这个名字」，而入门的注释里正解释着这个选项）——牙齿没红才发现，改成必须**带引号转发**",
		source: "判据在 `cmd/ngm/v090_ps1_wrappers_run_test.go`；修复：mirror 入口补 `-ApiBase`，两个入口补 `-Help`，另为 `check-release-status.sh` **新增**了 Windows 入口",
	},
	"TestV90DocsLinksStillCatchesBrokenLinks": {
		what: "文档链接检查的**负例**：一个临时文档树里放 4 条链接——存在的相对链接（过）· `../` 形式（过）· 子目录形式（过）· 反引号里的示例（**忽略**）· 一条真的坏链接（**红并点名**），并要求「BROKEN LINK」恰好出现一次",
		why: "v0.90 把它的路径规范化从「每条链接一次子进程」（`cd … && pwd`）换成纯 bash 字符串处理，实测 **133.72s → 33.07s**（docs/ 有 248 个文件、上千条链接 ⇒ 原来上千次 spawn）。**加速最容易顺手改坏语义**，而改坏会伪装成「全过」：" +
			"这条网是那次改动的对照（反引号那条语义尤其容易被顺手改掉）",
		source: "判据在 `cmd/ngm/v090_ps1_wrappers_run_test.go`；改动在 `scripts/check-docs-links.sh` 的 `normalize_path`",
	},
	"TestV89ScriptsArtifactsAreEitherRunOrExplained": {
		what: "**交付脚本必须「被谁跑」说得清**：`scripts/*.sh|*.ps1` 要么能沿着「判据 / 工作流 / 其它脚本」的引用链走到某处（**传递闭包**：A 被某条判据跑、A 的正文提到 B ⇒ B 也算覆盖），要么在 `explainedNotAutomated` 里写明为什么跑不了；登记表里出现已不存在的文件也红；新增脚本而两样都没有 ⇒ 红",
		why: "v0.88 抓到的那两个 `.ps1` 是交付给 Windows 用户的入口，却在仓库里躺了很久**根本不能被解析**——它们为什么没人发现？因为**没有任何东西在跑它们**（Go 侧只有一个判据读了它的文本，用来对齐 Find-Bash 顺序）：「读过」与「跑过」差了整整一个层次。" +
			"这条网把「没人跑」变成必须显式登记的事实。记账口径写在判据注释里：按**引用**记账，是筛子不是证明（它让「从没被提到」不可能悄悄发生；「真的跑过一次」由各脚本自己的判据守着）",
		source: "判据在 `cmd/ngm/v089_artifacts_test.go`；牙齿：往 `scripts/` 放一个没人跑、也没登记的新脚本 ⇒ 红并点名它，删掉 ⇒ 绿",
	},
	"TestV89PackageGateDemoRuns": {
		what:   "`scripts/packagegate-demo.sh` 在判据里**真的跑一次**，并要求它自己报出结论（`… passed, 0 failed`）——不接受「跑完没报错」",
		why:    "它是 `docs/COMPETITIVE-ANALYSIS.md` 里那套对照的**证据生成器**（demo 的断言只写在它里面），而此前它只被文档与 `.ps1` 入口提到——正是 v0.88 教训里的那一类「只被读过、从没被跑过」。它不碰网络（全部在本地合成目录里做），适合进全量测试",
		source: "判据在 `cmd/ngm/v089_packagegate_demo_test.go`；实现在 `scripts/packagegate-demo.sh`（`.ps1` 转调同一个实现）",
	},
	"TestV89WorkflowReferencesExistingScripts": {
		what: "**工作流里点名的脚本必须真的在**：扫 `.github/workflows/*.yml` 里所有 `scripts/<file>` 形式的引用，逐个查磁盘（去重后报）",
		why: "守的是一类很安静的腐烂：CI 里写着 `bash scripts/xxx.sh`，而那个文件被改名或删掉——直到下一次有人推 tag / 开 PR 才会发现，而本仓库镜像最后一次推送是 `v0.65.0`，这条路可能几个月没人走（" +
			"v0.88 已经吃过一次同类的亏：只被读取、从未被执行的东西会烂）",
		source: "判据在 `cmd/ngm/v089_artifacts_test.go`；牙齿：往 ci.yml 里塞一个指向不存在脚本的步骤 ⇒ 红并点名它（去重），恢复 ⇒ 绿",
	},
	"TestV89ShellScriptsParse": {
		what:   "`scripts/*.sh` 全部过 `bash -n`（只解析、不执行，因此不需要夹具也不碰网络）——与 v0.88 的 `.ps1` 解析网对称：交付物最低限度的两种语言，各有一条网",
		why:    "v0.88 的教训反过来用在 `.sh` 上：解析失败是最容易被忽略的坏法（没人跑就没人知道）。实测 10 个 `.sh` 当时都过——这条网守的是**下一次**",
		source: "判据在 `cmd/ngm/v089_artifacts_test.go`",
	},
	"TestV88PowerShellScriptsParse": {
		what: "`scripts/*.ps1` 必须能被 **PowerShell 自己的解析器**解析；含非 ASCII 的必须带 UTF-8 BOM（否则 PowerShell 5.1 按 ANSI 解码 UTF-8 文本，中文在 GBK 下的前导字节会**把换行一起吃掉**）",
		why: "写这条网时它当场抓到两个**交付给 Windows 用户的入口脚本根本不能被解析**：`check-library-standard.ps1` 与 `packagegate-demo.ps1`（按 UTF-8 解码 0 错、按 ANSI 3 错——" +
			"错在同一处：一行注释的末尾，乱码把 LF 吞了，后面的代码被并上前一行）。它们坏得很安静：Go 侧有个判据**只读它的文本**来对齐 Find-Bash 顺序，因此从没发现它跑不了",
		source: "判据在 `cmd/ngm/v088_powershell_scripts_test.go`（用 `[Parser]::ParseFile`，即 CI 与用户真正会遇到的那一步）；修复：给 5 个缺失的 `.ps1` 补 BOM（另外 4 个本来就有——约定其实一直是 BOM，是这 5 个漂移了）",
	},
	"TestV88ReadingsSummaryIsWritten": {
		what:   "读数要放在**会被看见**的地方：`scripts/write-readings-summary.sh` 把两个源的覆盖写成 markdown（CI 用 `--out \"$GITHUB_STEP_SUMMARY\"` 贴到 job summary 页），并守两条纪律——**读不出必须写\"没读成\"**（不许静默少一块）、**`--out` 是追加**（`$GITHUB_STEP_SUMMARY` 本就是追加写的地方，截断会抹掉同 job 别的步骤）",
		why:    "仓库不缺读数（v0.86 已把它们换成随时可再测的命令），缺的是**有人看**：埋在流水线日志里的读数与没有差不多。牙齿：把 `--api-base` 指到一个没人听的地址 ⇒ 摘要里必须出现\"这一段**没读成**\"，且段落本身仍在（消失才是要防的）",
		source: "实现 `scripts/write-readings-summary.sh`（+ `.ps1` 转调，同一条纪律：唯一实现是 .sh）；判据在 `cmd/ngm/v088_readings_summary_test.go`（本地假 API，不碰网络）；接线在 `.github/workflows/ci.yml`（`if: always()`，永不判失败：读数 ≠ 门禁）",
	},
	"TestV88DeclaredEntryMustExist": {
		what: "「有没有入口」与「入口文件在不在」是**两条判据**：`entry-resolvable` 按**声明**判定（与 `InferEntry` 同口径——声明的路径产品就会用），新增必做判据 `entry-file-exists` 才问「文件真的在仓库里吗」，并点名缺的是哪一个",
		why: "v0.88 之前两者混在一起（\"第一个**存在**的候选\"）：一个\"声明了但文件不在\"的包，产品用那个路径、自检器说\"三层都没找到入口\"——两边都说了话，却**说不清到底哪里不对**。" +
			"拆开之后，两种缺陷各自有一句可执行的结论。牙齿：声明 `./gone.js` ⇒ 入口判据过、存在性判据红（理由里有点名的路径）、exit 1；补上文件 ⇒ 两条都过、exit 0",
		source: "判据在 `cmd/ngm/v087_entry_alignment_test.go`（与入口对齐网同文件：它们守的是同一件事的两半）；实现在 `scripts/check-library-standard.sh`",
	},
	"TestV87EntryInferenceAgreesBetweenProductAndChecker": {
		what: "**自检器与产品必须在同一批形态上给出同一个入口**：11 种 `package.json` 形态（`ngm.json` 第 1 层 · `exports` 字符串/`\".\"` 字符串/条件映射/无 `\".\"` 条件映射/只有 `require`/`default` 先于 `require` · `main`+`types` 字段 · `index.js` · `index.cjs` · 两侧都找不到）逐个对账——一侧取 `mappings.InferEntry`（产品真正会写进 mappings 的入口），另一侧解析自检器**自己打印的那句话**（`入口 <path>（来自 …`）",
		why: "v0.78 抓到过「自检器比产品严」：标准第 1 条推荐 `exports`，自检器却不读它，于是照着标准做的库被判「三层都没找到入口」。" +
			"当时的处置是**人工**逐字对齐，而下一次有人在任一侧加一层，没有任何判据会红 ⇒ 这条网把它变成机械对账（牙齿：把自检器的 exports 两层拿掉 ⇒ 四条 exports 形态立刻红）",
		source: "产品侧 `internal/mappings.InferEntry`（+ 判据里的真实目录读器）；自检器侧 `scripts/check-library-standard.sh` 的 `entry-resolvable` 输出。已知边界写在测试注释里：自检器只打印 Main，`Types` 侧只判「有没有」",
	},
	"TestV86MirrorCoverageIsAMeasuredReading": {
		what: "`scripts/check-mirror-coverage.sh`：镜像侧覆盖是**按需实测的读数**——两次请求拿 tag 数与 release 数，算出「未推送 = 已交付 − 镜像 tag」；**缺口不判失败**（读数不是门禁），只有「问不到」（HTTP 非 200 / 连不上）才 exit 3 并明说「不下结论」",
		why: "仓库里那几处「GitHub 有 65 个 tag / 60 个 release」是写死的历史快照，而快照过期的方式很安静——「未推送数」那处**漏过两次**（v0.84 复盘）。" +
			"这条命令让同一件事随时可再测；判据用**本地假 API**（httptest）喂它，因此不碰网络",
		source: "实现在 `scripts/check-mirror-coverage.sh`（+ `.ps1` 转调同一实现）；判据在 `cmd/ngm/v086_mirror_coverage_test.go`：读得到（65 tag · 60 release · 最新 v0.64.0 · UNPUSHED = 已交付 − 65）· 假 API 回 500 ⇒ exit 3 · 连不上 ⇒ exit 3",
	},
	"TestV84WorktreeStateNamesUncommittedVersionArtifacts": {
		what: "收口脚本里的**工作树状态读数**：干净 ⇒ 只计数不点名 · 出现未提交的版本工件（plan/retrospective）⇒ **点名它**并写出「没有版本保护」 · 提交之后点名消失（点名认的是「未提交」而不是「文件存在」）· 就地改一份已跟踪的快照 ⇒ 单独一句话 · 另含「收口清单」（该跑哪两条命令）",
		why: "起因是一次真实事故：v0.83 开工时没看工作树，覆盖了并行工作线的两份**未跟踪**文件（无 Git 副本、无 IDE 历史）⇒ 只能重建。" +
			"但这个读数**不做成门禁**：开发中途工作树本来就不干净，恒红的门禁等于没有门禁——它的价值在于「每次收口都打印出来」",
		source: "实现在 `scripts/check-release-status.sh`（v0.84 新增两节：worktree state / before you call it delivered）；判据在**合成仓库**上跑（`git init` + 一份复盘 + 一个附注 tag），不依赖本仓库状态",
	},
	"TestV83BrokenGlobalConfigIsRejectedNotSilentlyDegraded": {
		what: "**坏配置必须报出来**：没有配置文件照常（默认值就是为此存在的）· 非法值（`git.defaultProtocol: sqlite`）⇒ `ConfigInvalid` 且**用户看到的文本**里点名文件与字段（`FormatHuman` 的 message + cause + hint 三段一起判）· 未知字段（typo）同样报错 · 合法配置照常（牙齿：证明不是「有配置就报错」）",
		why: "起点是实测：把 `git.defaultProtocol` 写成非法值，命令照样跑、没有任何提示（协议被悄悄换回 https）。旧注释的理由是「读不出来时策略为 nil ⇒ 判定退化为默认档位 ⇒ 更严」——" +
			"那个论证只覆盖**策略**一个消费者，另外两个的退化**不是更严**：协议被换掉（报错指向网络，真因不可见）· `tokenEnvVars` 丢失用户声明的 token 变量名 ⇒ 脱敏漏掉它 ⇒ token 会被原样打进日志与报错",
		source: "实现在 `cmd/ngm/env.go` 的 `newProjectEnv`（v0.83：`config.Load` 的错误不再被 `cerr == nil` 吞掉，改为 `WrapUnlessHinted(CodeConfigInvalid, …)`）；判据用 `isolateUserEnv` 隔离 HOME 后直接调用 `newProjectEnv`，断言读的是 `errs.FormatHuman(err)`（用户实际看到的那段）",
	},
	"TestV80NestedMonorepoIsReportedAsIncomplete": {
		what: "**嵌套 monorepo**（子包自己也是 monorepo）的三种结论：① 一条 `nested-monorepo` 记录，点名它声明的子包模式与「约 N 个子包没有被体检」；② 它的**包专属**判据记不适用（不是必做失败），孙辈**一条判据都不出现**；③ **根能承担的判据照旧生效**（缺 README 仍要红——嵌套不是免检牌）；外加口径断言 `complete:false`（与 `ok` 互不顶替）",
		why: "此前嵌套根掉进「它是个包」的判据里，红的是 `entry-resolvable`（「三层都没找到入口」）——**用错理由红，等于没红**：" +
			"看报告的人会去给出入口，而那个仓库缺的不是入口。同时「已查的通过」与「符合标准」必须分得开（`complete` 字段），" +
			"否则一次没查全的体检会被读成一次合格",
		source: "实现在 `scripts/check-library-standard.sh`（v0.80：子模式也做 monorepo 检测、但不递归；`nested-monorepo` 记录 + 汇总口径 + `--json` 的 `complete` 字段，合并时向父级传播）；夹具由 `writeNestedMonorepoFixture` 现场造（外层 → inner → units/g1）",
	},
	"TestV79VendoredScanRunsTheStandardOnEveryDependency": {
		what: "`scripts/check-vendored-standard.sh`：对 `ngm.mappings.json` 里的**每个依赖**跑一次库标准自检——文本模式点名不合格的依赖与判据 · `--json` 的 id 带依赖名前缀且**条数与直接自检一致**（包装器不许少跑）· 映射指向不存在的目录 ⇒ `vendored-dir-missing` 必做失败 · 没有 mappings ⇒ **exit 2**（没有查过 ≠ 查过且合格）",
		why: "评分表说了「每个依赖都要符合标准」，那就得有一条命令能对「我 vendor 了什么」整体回答合不合格——v0.76 的候选 4 连落两版，" +
			"原因每次都是「它不属于产品代码」；它真正属于的是**标准**（产物侧的那一半）。三条纪律写进了判据：" +
			"事实源用 mappings（遍历 `ngm.vendor/` 会把 prune 残留也算成依赖，结论会飘）· 结论必须归属回依赖名 · 包装器与自检器条数对账",
		source: "实现在 `scripts/check-vendored-standard.sh`（+ `.ps1` 转调同一个实现，Windows 入口）；夹具由 `writeVendoredGood` / `writeVendoredBad` 现场造，与真实 vendored 副本一致（无 `.git`）",
	},
	"TestV78LibraryStandardReadsOtherMonorepoSignals": {
		what: "monorepo 的另外两个信号（pnpm / lerna）与三种连带结论：**根可以没有 package.json**（pnpm 形态，清单在 `pnpm-workspace.yaml`）⇒ 根的 `pkg-json-exists`/`pkg-name`/`pkg-version`/`license`/`engines` 记为不适用；" +
			"**子包只写 `exports`** 时必须解析出入口与类型（与 `internal/mappings.InferEntry` 的三种写法对齐）；" +
			"**读不出的 `packages` 写法**（YAML 映射 / 内联对象）⇒ `workspace-manifest-unreadable` 必做失败，且花括号 glob 里的逗号不许把模式切碎",
		why: "v0.77 只认 npm `workspaces`。实测另两种形态时三件事同时暴露：pnpm 的根**合法地**没有清单（此前是必做红）· lerna 的子包清单只写在 lerna.json（整个仓库会被当成单包）· " +
			"而最刺眼的是第三条：自检器**比产品严**——只写 `exports` 的包（正是标准推荐的第 1 条）被判「找不到入口」，而 `InferEntry` 一直读 `exports`。判据比被检对象更严，就是误判",
		source: "判据与实现在 `scripts/check-library-standard.sh`（v0.78：检测三信号 · 新增「读不出即失败」· exports 解析逐字对齐 `parseExports`）；夹具由 `writePnpmFixture` / `writeLernaFixture` / `writeExportsOnlyChild` 现场造",
	},
	"TestV77LibraryStandardHandlesOtherPackageShapes": {
		what: "库标准自检器对**其他包形态**的三种结论：CJS 优先（显式 `type=commonjs` 必须被当成「写明的选择」而不是缺陷）· monorepo（根不是包 ⇒ 包专属判据不适用；子包逐个按同一条判据体检且结论带路径前缀）· workspaces 里的目录缺 package.json（显式失败）",
		why: "起因是**实测到的误伤**：显式 CJS 的包被判「未声明 type=module」——而判据自己的措辞就是「CJS 需显式说明」，" +
			"一个无法被满足的推荐项不是严格而是错的；monorepo 的根则被套上五条「包」的判据（入口/exports/类型/ESM/发布范围），" +
			"而那些问题的正确答案是「这里没有这个问题」。**归属**也在这条判据里守着：子包结论不带前缀，合并后的报告说不清是谁没通过",
		source: "判据与实现在 `scripts/check-library-standard.sh`（v0.77 新增 monorepo 模式：子包用**同一支脚本**递归执行，父进程只做归属与合并）；夹具由 `writeCJSFixture` / `writeMonorepoFixture` 现场造",
	},
	"TestV76LibraryStandardCheckerHoldsItsVerdicts": {
		what: "库标准自检器的三种结论各钉一个场景：pass（合规夹具）· fail（注入 postinstall / 删 runtime 要点名具体判据）· skip（不是 git 仓库 ⇒ 不适用且不计入）",
		why: "它守的是**判据自己的判据**：两次缺陷都是「跑出来才发现」的（v0.74 的 CR 转义按错模式搜索、v0.76 把 vendored 副本误判为缺 tag），" +
			"而脚本的结论此前没有任何东西守着。需要 bash + node，缺任一时**大声跳过**（写明原因），与脚本自身缺 node 时 exit 2 的行为一致",
		source: "判据与实现在 `scripts/check-library-standard.sh`（唯一实现）；夹具由 `writeConformingFixture` 现场造",
	},
	"TestV71AdrIndexCoversEveryAdrFile": {
		what: "ADR 索引必须与 `docs/adr/` 的文件集双向对账：每文件恰好一行、链接存在、3 列、编号连续",
		why: "v0.70 修掉的缺陷就是这条判据的缺席——ADR-025 的行缺失、ADR-024 重复、" +
			"ADR-026 的行与之粘连，挂了数版没有任何东西变红；索引行靠手抄，而手抄必然漂移",
		source: "文件集来自 `docs/adr/adr-*.md`（`os.ReadDir(`）；索引行来自 `docs/adr/README.md` 的表格（`readDoc(`）",
	},
	"TestV63MilestoneTablesListEveryVersionExactlyOnce": {
		what: "逐版表：每个版本恰好一行、升序、连续到最新",
		why: "版本集**派生**自 `docs/development/` 的复盘文件名（与 v0.58 同源）；" +
			"「哪些文档算逐版表」由**扫描活文档**决定——任何一份活文档只要其版本行够得到最新版本，" +
			"就必须在 `milestoneTableStartsAt` 里声明起点（没声明即红，新表不会漏网）；" +
			"起点之前的行不归它管（roadmap 之类）。四张表的起点都是量出来的现状",
	},
	"TestV63EngineCommandsAreMeasuredForCodeSix": {
		what: "最后四个命令（build · typecheck · css · transform）的码 6 搬进实测",
		why: "它的覆盖集是**探针试出来的四个场景**，不是任何表或运行时集合——" +
			"而「到不了」与「到得了」的差别只在三件事是否凑齐（`AllowEngines` 声明权限、" +
			"`isolateUserEnv` 换 HOME、`m6Catalog` 登记引擎），v0.62 正是把「少了一件」" +
			"读成了「钩子坏了」",
	},
	"TestV62TheRemainingGapCommandsAreMeasuredForCodeSix": {
		what: "缺口表里能兑现的兑现：audit 与 integrations 的码 6 搬进实测",
		why: "它的覆盖集是**两个具体的命令场景**（audit 用 `v3AuditProject` 的本地 OSV 替身、" +
			"integrations 用已安装项目 + 真实工具名）；两者都由**探针试出来的**。" +
			"（v0.62 时它还想管 build/typecheck/css/transform 四个，当时判成\"到不了\"；" +
			"v0.63 翻了这个判断——少的是夹具里的一行 `isolateUserEnv`，" +
			"那四个现在由 `TestV63EngineCommandsAreMeasuredForCodeSix` 覆盖）",
	},
	"TestV61BrokenPipeIsIntentNotFailure": {
		what: "管道被对端关闭（`ngm verify | head`）是用户的意图，不是失败",
		why: "它的覆盖集是**探针量出来的三种情形**（人读+对端关闭 · 人读+一般写失败 · " +
			"JSON+对端关闭）——第三种是**有意的不对称**，写成断言是为了让它可查；" +
			"「对端关闭」的判定本身由本机的探针量出（Windows 给的是 232/ERROR_NO_DATA， " +
			"而 `errors.Is(err, syscall.EPIPE)` 在那里是 false）",
	},
	"TestV60TheHumanChannelTreatsWriteFailureTheSameWay": {
		what: "两条通道对「写不出去」判法一致：人读路径也退 6",
		why: "它的覆盖集是**探针逐一量过的六个场景**（verify · why · outdated · tree · engines · " +
			"`--help`），每个都配一条对照（正常 stdout ⇒ 仍退 0）——" +
			"而「所有命令」那一侧由 `dispatch` 里的**唯一一处包装**覆盖（不是逐命令加检查），" +
			"所以这张网不需要从命令表派生；它要证明的是「一致性」本身",
	},
	"TestV58PublishedReadingsAgreeWithTheirSource": {
		what: "散文里的发布读数：版本总数 · 最新版本 · GitHub/Gitee 数 · 缺口数 · 成功次数",
		source: "文件系统派生已交付版本数（`docs/development/` 的复盘与计划文件数，两者交叉核对）" +
			"+ `livingDocs(` 的活文档清单（另加发布清单那张**索引**，它是活的）",
	},
	"TestV57ExitCodeSourcesAreRegistered": {
		what: "码 1 的来源：谁能让进程以 1 退出 · 每一处归哪一类 · 每类都要写进文档",
		why: "它的覆盖集是**源码自己**——`filepath.WalkDir` 走 `cmd/ngm` 与 `internal`、" +
			"用 `go/parser` 求出每处站点所在的函数；没有任何一张可指的表或运行时集合，" +
			"广度来自『扫源码树』（与 `TestV28…` 那条同一种形态）",
	},
	"TestV56RegisteredPointersResolve": {
		what:   "登记里写成指针的条目必须指到**存在的东西**（`TestX/sub` 的子测试真的在源码里）",
		source: "源码：`testFilesInThisDir(` 找那个测试函数，再在它体内找 `t.Run(\"<子测试>\"`",
	},
	"TestV42EmptyValueNeverSilentlyChangesBehaviour": {
		what:   "取值 flag 的 37 条空值语义",
		source: "源码派生（`valueFlagPairs` → `flagsetBlocksOf`，跨文件、带声明种类）",
	},
	"TestV43BooleanFlagsHoldGoSemantics": {
		what:   "命令层布尔 flag 的四条语义（40 对）",
		source: "源码派生（`boolFlagsOfCommand`，跨文件）",
	},
	"TestV45SubcommandFlagsHoldTheSameSemantics": {
		what:   "子命令层布尔 flag（2 对）+ 覆盖并集守卫",
		source: "源码派生（`subcommandBoolPairs`；守卫的覆盖集由三张网**各自的选择函数**拼出）",
	},
	"TestV47TwoTokenSpellingsAreTheSameThing": {
		what:   "两 token 的写法（37 条取值 + 42 对布尔）",
		source: "源码派生（与 v0.42/43/45 共用同一批选择函数）",
	},
	"TestV49ConfigValidationAlwaysCarriesAHint": {
		what:   "config 校验错误必须自带建议（静态普查）+ `duration.go` 的具名允许数",
		source: "源码（读 `internal/config/*.go` 的每一行）",
	},
	"TestV50MachineReadableReportsCarryTheNextStepToo": {
		what:   "报告里的\"下一步\"要跨通道成立",
		source: "夹具表（`reportCases`）+ 派生 flag 表（`hasJSONFlag` → `boolFlagsOfCommand`）",
		why:    "`reportCases` 是**夹具表**：它描述'造什么状态'，而'哪些命令支持 --json'是派生的",
	},
	"TestV55RootLevelTakesOnlyHelpAndVersion": {
		what: "子命令之前的 flag 不许被静默丢弃（根级只认 --help/-h/--version）",
		source: "运行时表 `rootFlags`（`dispatch` 校验时读的同一份）取「接受」一侧；" +
			"`boolFlagsOfCommand` 派生的命令层 flag 取「拒绝」一侧的样本",
	},
	"TestV54EarlyFailuresAreMachineReadableToo": {
		what:   "解析阶段的失败（未知 flag 两种顺序 · 未知命令 · 缺子命令）也要有信封",
		source: "运行时命令表（`commands`）+ 派生 flag 表（`hasJSONFlag` → `boolFlagsOfCommand`）",
	},
	"TestV51JSONErrorEnvelopeIsCompleteAndSameSentence": {
		what:   "失败路径上的错误信封",
		source: "错误面表（`surfaceCases`，复用 v0.31 的夹具）+ 派生 flag 表",
		why:    "`surfaceCases` 是**夹具表**；覆盖面由'哪些命令支持 --json'这道机械闸门决定",
	},
	"TestV52TheDeferredListIsNamedAndCounted": {
		what:   "挂着的条目必须具名、写法完整、总数受棘轮",
		source: "自证：被查的表就是它自己",
		why: "它不声称覆盖任何**实现**——被查的对象是 `debts` 这张登记表本身；" +
			"所以它所在的文件里没有（也不需要）派生入口",
	},
	"TestV37EveryCommandUnderEveryConfigErrorUsesItsChannel": {
		what: "21 个命令 × 8 种参数形状 × 配置错误通道",
		source: "运行时命令表（`commands`）+ 夹具输入表（`matrixArgs`）+ 配置形状轴（`configErrorShapes`，v0.65 提成命名变量）；" +
			"每一列还要求自己的**证据**（至少一格走到配置层，且签名不与其他列相同——等价要写进 `shapeEquivalents`）",
		why: "`matrixArgs` 是**夹具输入**：它给每个命令一份'语法上够用'的参数，不是覆盖集",
	},
	"TestV31EveryReachableFailurePathExplainsItself": {
		what:   "按失败路径组织的错误面（23 条）",
		source: "运行时命令表 + 夹具表（每条路径一个夹具）",
		why:    "夹具表在这里**就是**被判据查询的对象（'这条路径该失败'），不是覆盖率的来源",
	},
}

func TestV53CoverageComesFromImplementationNotFromMemory(t *testing.T) {
	if len(netCoverageSources) == 0 {
		t.Fatal("登记表是空的——这条纪律没有可查的对象")
	}

	files := map[string]string{} // 测试函数名 → 它所在的文件内容
	for _, file := range testFilesInThisDir(t) {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for name := range netCoverageSources {
			if strings.Contains(text, "func "+name+"(") {
				files[name] = text
			}
		}
	}

	sources, exceptions := map[string]int{}, 0
	for name, ns := range netCoverageSources {
		text, ok := files[name]
		if !ok {
			t.Errorf("登记的 %q 在这个目录里找不到同名测试函数——**登记看着空气**", name)
			continue
		}
		sources[ns.source]++
		fromImpl := false
		for _, ep := range derivationEntryPoints {
			if strings.Contains(text, ep) {
				fromImpl = true
				break
			}
		}
		if !fromImpl && strings.TrimSpace(ns.why) == "" {
			t.Errorf("%q 覆盖集既不来自源码、也不来自运行时表——"+
				"它所在的文件里找不到任一认可的入口（%v）。\n"+
				"要么改成从实现里取，要么在这个登记里写清 `why`：为什么它不需要那样取",
				name, derivationEntryPoints)
		}
		if !fromImpl {
			// 没有派生入口 ⇒ 必须是一条**具名例外**（`why` 非空，上面已查过）。
			exceptions++
		}
	}

	t.Logf("coverage sources: %d net(s) registered, %d named exception(s) — %v",
		len(netCoverageSources), exceptions, sources)
}

// testFilesInThisDir 列出本目录的 `_test.go`（纪律要查的是它们）。
func testFilesInThisDir(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(".", n))
	}
	return out
}
