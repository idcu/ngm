package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.35：**同一个分支，不同成因，建议必须分化。**
//
// `remediationFor` 里 `res.Err != ""` 是一支，但它覆盖了**成因完全不同**的失败。
// v0.34 给它们同一句话（`run \`ngm install\` … or re-run \`ngm verify\` without --offline`），
// 而对"网络被策略拒"那一支，这句话是**错的**：`ngm install` 也会被拒。
//
// 真相是产品**早就写好了**那句话——权限层在构造错误时就带着
// `add "net:<host>" to \`permissions.allow\` in ~/.ngm/config.json`，
// 只是 `res.Err = merr.Error()` 只留下了 message。
//
// 所以这一版的判据是：**两种成因各自拿到承认自己成因的建议，且两句话不相同。**
//
// 这也是 v0.28 那条原则（"建议沿包装链找第一句非空的"）在**报告**这一侧的同一句话：
// 最贴近成因的那一层最知道该怎么办，**这个函数猜不出来**。
// operationalCasePrefix 是这一族的**命名前缀**（v0.56）。
//
// 用它把"哪些用例属于这一族"从**夹具表**派生出来，而不是靠我记：
// 从前这张表只能查"悬挂键"（登记了、表里没有 ⇒ 红），
// 而**漏因查不出来**——新增第四种成因时，判据自己看不见自己少了一条。
const operationalCasePrefix = "verify/检查未能完成（"

var operationalCauses = []struct {
	caseName string
	// marker 必须出现在**这一成因**的行动行里：
	//   · mirror 不在 ⇒ 那句建议点名 `ngm install`（把 mirror 取下来）
	//   · 网络被拒 ⇒ 那句建议点名 permissions.allow（去放行那个 host）
	//   · 锁钉的 commit 不在本机 mirror ⇒ 那句话说的是"镜像冷"，
	//     而它的下一步正是**跑一次在线**（那才能分辨"镜像没取过"与"上游丢了它"）
	marker string
}{
	{"verify/检查未能完成（mirror 不在）", "`ngm install`"},
	{"verify/检查未能完成（网络被策略拒）", "permissions.allow"},
	{"verify/检查未能完成（锁钉的 commit 不在本机 mirror）", "the local mirror is cold"},
}

func TestV35OperationalFailuresAdviseTheirOwnCause(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// 先对账**成员集**：一族有几个成员，由夹具表的命名说了算（v0.56）。
	//
	// 两个方向都要查：
	//   · 派生 ⊆ 登记 —— 多了成因却没登记 ⇒ 红（这是**漏因**，从前查不出来）
	//   · 登记 ⊆ 派生 —— 登记指向空气 ⇒ 红
	var derived []string
	for i := range reportCases {
		if strings.HasPrefix(reportCases[i].name, operationalCasePrefix) {
			derived = append(derived, reportCases[i].name)
		}
	}
	if len(derived) == 0 {
		t.Fatalf("夹具表里没有任何用例以 %q 开头——这条判据的范围缩到零了", operationalCasePrefix)
	}
	registered := map[string]bool{}
	for _, oc := range operationalCauses {
		registered[oc.caseName] = true
	}
	for _, name := range derived {
		if !registered[name] {
			t.Errorf("夹具表里的 %q 属于这一族（名字以 %q 开头），这里却没有它的 marker——"+
				"**新增了一种成因，而这条判据还只查旧的那几条**：给它写一条 marker，"+
				"说明这一成因的建议必须点名什么", name, operationalCasePrefix)
		}
	}
	for _, oc := range operationalCauses {
		ok := false
		for _, name := range derived {
			if name == oc.caseName {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("登记里的 %q 不在夹具表的这一族里——登记指向空气", oc.caseName)
		}
	}

	advice := map[string]string{} // 用例名 → 它拿到的那句建议

	for _, oc := range operationalCauses {
		var rc *reportCase
		for i := range reportCases {
			if reportCases[i].name == oc.caseName {
				rc = &reportCases[i]
				break
			}
		}
		if rc == nil {
			t.Fatalf("the case %q is gone from reportCases — this net has nothing to look at", oc.caseName)
		}

		t.Run(oc.caseName, func(t *testing.T) {
			home := isolateUserEnv(t)
			dir := rc.setup(t, home)
			args := append(append([]string{}, rc.args...), "--dir="+dir)

			var out, errb bytes.Buffer
			if code := dispatch(args, &out, &errb); code == 0 {
				t.Fatalf("this case is meant to fail and report, but exited 0:\n%s", out.String())
			}
			text := out.String() + errb.String()
			lines := reActionLine.FindAllString(text, -1)
			if len(lines) == 0 {
				t.Fatalf("this operational failure carries no next-step line at all:\n%s", text)
			}
			joined := strings.Join(lines, "\n")
			if !strings.Contains(joined, oc.marker) {
				t.Errorf("the advice does not acknowledge **this** cause (expected %q in it):\n%s",
					oc.marker, joined)
			}
			advice[oc.caseName] = joined
		})
	}

	// 判据的核心：**两两不能是同一句话**。
	// 同一片分支覆盖多种成因，若两处给出一模一样的建议，那么其中至少一处是错的
	// ——因为这些路该做的事本来就不同（去取 mirror / 去放行 host / 跑一次在线做分辨）。
	//
	// v0.44 起是**两两**核对（成因从 2 种涨到 3 种）：原来是硬编码"两句不相同"，
	// 那种写法在成因变多之后会**静默地只查前两句**。
	seen := map[string]string{}
	for name, line := range advice {
		if prev, dup := seen[line]; dup {
			t.Errorf("two operational causes were given the same advice (%q 与 %q):\n%s\n"+
				"one branch cannot be right for both — a missing mirror is fixed by fetching, "+
				"a denied host by allowing it, an absent commit by going online once",
				prev, name, line)
		}
		seen[line] = name
	}
	if len(advice) != len(operationalCauses) {
		t.Fatalf("only %d of %d causes produced an advice line — a cause silently dropped out",
			len(advice), len(operationalCauses))
	}
	t.Logf("operational causes: %d advices, all distinct=%v", len(advice), len(seen) == len(advice))
}
