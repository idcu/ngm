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
		what:   "无参数与垃圾参数两个维度下每个命令的通道与退出码",
		source: "运行时 `commands` × `matrixArgs`（每命令参数表，V37 双向对账）",
	},
	"TestV40ArgumentSpellingHoldsItsContracts": {
		what:   "参数拼法（顺序 · `--` · 重复）在真实命令表上保持一致",
		source: "运行时 `commands` × `matrixArgs`；跳过项必须公开记入日志并有下限守卫",
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
		what:   "21 个命令 × 8 种参数形状 × 配置错误通道",
		source: "运行时命令表（`commands`）+ 夹具输入表（`matrixArgs`）",
		why:    "`matrixArgs` 是**夹具输入**：它给每个命令一份'语法上够用'的参数，不是覆盖集",
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
