package resolve

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// 端口在四种拼法里的行为必须**一致**（v0.108）。
//
// 缺陷形态：`gitlab.corp:8443/team/repo` 被读成 host=gitlab.corp、
// path=`8443/team/repo`，于是 clone 的地址变成
// `https://gitlab.corp/8443/team/repo.git`——**另一个仓库**，而且一路成功。
// 同一地址的 URL 形式早就被明确拒绝了；两种拼法一个报错、一个悄悄指向别处，
// 是最坏的那种不一致。
func TestPortFormsAgreeAcrossSpellings(t *testing.T) {
	// 非默认端口：**三种**拼法都必须被拒绝（此前只有 URL 形式拒绝）。
	rejected := []string{
		"https://gitlab.corp.example:8443/team/repo",
		"gitlab.corp.example:8443/team/repo",
		"git@gitlab.corp.example:8443/team/repo",
	}
	for _, raw := range rejected {
		t.Run("reject "+raw, func(t *testing.T) {
			_, err := Normalize(raw)
			if err == nil {
				t.Fatalf("%q carries a non-default port that canonical form cannot represent; "+
					"silently dropping it targets a different repository", raw)
			}
			// 细节在 hint / cause 链上，不在外层 message 里——按人读形态断言。
			if human := errs.FormatHuman(err); !strings.Contains(human, "port") {
				t.Errorf("the error must name the cause, got:\n%s", human)
			}
		})
	}

	// 默认端口：与"不写"等价——三种拼法都必须得到**同一个** canonical。
	same := []string{
		"https://gitlab.corp.example/team/repo",
		"gitlab.corp.example:443/team/repo",
		"git@gitlab.corp.example:443/team/repo",
	}
	var want string
	for i, raw := range same {
		c, err := Normalize(raw)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if i == 0 {
			want = c.Slug()
			continue
		}
		if got := c.Slug(); got != want {
			t.Errorf("%q normalized to %q, want %q (a default port is the same as none)", raw, got, want)
		}
	}

	// 反向：路径的第一段**不是**纯数字时，绝不能当成端口。
	// `github.com:org/repo` 是 shorthand 语法，必须照旧工作；
	// `host:1234abc/repo` 里的 `1234abc` 是一个合法的（怪）路径段。
	for _, raw := range []string{"github.com:acme/repo", "gitlab.corp.example:1234abc/repo"} {
		if _, err := Normalize(raw); err != nil {
			t.Errorf("%q must still parse: only an all-digit first segment is treated as a port: %v", raw, err)
		}
	}
}
