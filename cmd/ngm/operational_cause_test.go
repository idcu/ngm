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
var operationalCauses = []struct {
	caseName string
	// marker 必须出现在**这一成因**的行动行里：
	//   · mirror 不在 ⇒ 那句建议点名 `ngm install`（把 mirror 取下来）
	//   · 网络被拒 ⇒ 那句建议点名 permissions.allow（去放行那个 host）
	marker string
}{
	{"verify/检查未能完成（mirror 不在）", "`ngm install`"},
	{"verify/检查未能完成（网络被策略拒）", "permissions.allow"},
}

func TestV35OperationalFailuresAdviseTheirOwnCause(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

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

	// 判据的核心：**不能是同一句话**。
	// 同一片分支覆盖两种成因，若两处给出一模一样的建议，那么其中至少一处是错的
	// ——因为这两条路该做的事本来就不同（一个去取 mirror，一个去放行 host）。
	if len(advice) == 2 {
		a, b := "", ""
		for _, v := range advice {
			if a == "" {
				a = v
				continue
			}
			b = v
		}
		if a == b {
			t.Errorf("both operational causes were given the same advice:\n%s\n"+
				"one branch cannot be right for both — a missing mirror is fixed by fetching, "+
				"a denied host by allowing it", a)
		}
	}
	t.Logf("operational causes: %d advices, distinct=%v", len(advice), len(advice) == 2)
}
