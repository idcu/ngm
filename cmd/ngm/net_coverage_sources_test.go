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
