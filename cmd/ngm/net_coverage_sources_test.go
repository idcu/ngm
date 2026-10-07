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
}

var netCoverageSources = map[string]netSource{
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
