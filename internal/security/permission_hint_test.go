package security

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// v0.113：提示里那一段必须是**真能粘贴的 JSON**。
//
// 缺陷形态：`Check` 的注释与 `configuration.md` 都承诺"提示里给出可以直接粘贴的
// 那一行"，而实现给的是一句散文（"add \"net:github.com\" to `permissions.allow`
// in ~/.ngm/config.json"）。用户还得自己把这句话翻译成 JSON，而这是他第一次用
// ngm 时撞到的第一面墙——`init` 与 `add` 都成功，`install` 才 exit 3。
//
// 判据不看措辞，看**能不能解析**：把提示里那段 JSON 抠出来 Unmarshal，
// 并断言它真的会让这条权限被放行。措辞可以再改，这条性质不能丢。
func TestV113PermissionHintCarriesPasteableJSON(t *testing.T) {
	pol := mustPolicy(t, nil, nil)

	for _, perm := range []string{"net:github.com", "run:esbuild"} {
		// 先确认这条权限在空配置下确实被拒（否则后面的断言无意义）。
		err := pol.Check(mustPermission(t, perm))
		if err == nil {
			t.Fatalf("%s must be refused by default; this test would be vacuous", perm)
		}
		hint := errs.Hint(err)

		snippet := extractJSONObject(t, hint)
		var parsed struct {
			Permissions struct {
				Allow []string `json:"allow"`
			} `json:"permissions"`
		}
		if uerr := json.Unmarshal([]byte(snippet), &parsed); uerr != nil {
			t.Fatalf("the snippet in the hint is not JSON (%v): %s", uerr, snippet)
		}

		// 关键：粘贴之后这条权限**真的**会被放行。
		allowed := false
		for _, a := range parsed.Permissions.Allow {
			if a == perm {
				allowed = true
			}
		}
		if !allowed {
			t.Errorf("the pasted snippet must allow %q, got %+v", perm, parsed.Permissions.Allow)
		}

		// 三条旧承诺仍然要在（用户得知道加在哪、加什么）。
		for _, want := range []string{perm, "permissions.allow", "config.json"} {
			if !strings.Contains(hint, want) {
				t.Errorf("the hint must still mention %q:\n%s", want, hint)
			}
		}
	}
}

// 拒绝（deny 显式列出）那一支不该给"粘贴就能过"的假希望：
// 它的动作是**从 deny 里删掉**，不是往 allow 里加。
func TestV113DeniedPermissionHintSaysRemoveDontAdd(t *testing.T) {
	pol := mustPolicy(t, nil, []string{"net:github.com"})
	err := pol.Check(mustPermission(t, "net:github.com"))
	if err == nil {
		t.Fatal("a denied permission must be refused")
	}
	hint := errs.Hint(err)
	if !strings.Contains(hint, "remove") {
		t.Errorf("the deny branch must tell the user to remove the entry, got:\n%s", hint)
	}
	if strings.Contains(hint, `"permissions":{"allow"`) {
		t.Errorf("the deny branch must not offer an allow snippet — adding it would not help:\n%s", hint)
	}
}

// mustPermission 把 "net:github.com" 解析成 Permission；解析不了就是测试写错了。
func mustPermission(t *testing.T, s string) Permission {
	t.Helper()
	p, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return p
}

// extractJSONObject 抠出提示里第一个 `{...}` 片段。
//
// 刻意写得很笨：提示是人读文本，能做这个假设的前提是**片段里没有嵌套花括号的字符串**，
// 而这里生成的片段永远只有一层。真有嵌套时它会解析失败而不是静默取错——那更好。
func extractJSONObject(t *testing.T, s string) string {
	t.Helper()
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		t.Fatalf("no JSON object in the hint:\n%s", s)
	}
	return s[start : end+1]
}
