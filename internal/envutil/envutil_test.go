package envutil

import (
	"strings"
	"testing"
)

// 三处施力点共用这一份语义（git / 沙箱 / 引擎），所以它自己的判据要写全。
func TestWithout(t *testing.T) {
	env := []string{"PATH=/bin", "GITHUB_TOKEN=secret", "HOME=/home/x", "noequals"}

	t.Run("名字匹配且大小写不敏感", func(t *testing.T) {
		for _, deny := range [][]string{{"GITHUB_TOKEN"}, {"github_token"}} {
			got := strings.Join(Without(env, deny), " ")
			if strings.Contains(got, "secret") {
				t.Errorf("deny=%v left the value behind: %s", deny, got)
			}
			if !strings.Contains(got, "PATH=/bin") {
				t.Errorf("deny=%v removed an unrelated variable: %s", deny, got)
			}
		}
	})

	t.Run("裸名字形态也能被剔除", func(t *testing.T) {
		for _, e := range Without(env, []string{"NOEQUALS"}) {
			if e == "noequals" {
				t.Error("an entry without `=` must still be matched by name")
			}
		}
	})

	t.Run("空拒绝列表原样返回（不复制）", func(t *testing.T) {
		got := Without(env, nil)
		if len(got) != len(env) {
			t.Errorf("len = %d, want %d", len(got), len(env))
		}
		if &got[0] != &env[0] {
			t.Error("with nothing denied the list must be returned as-is (no allocation on the common path)")
		}
	})

	// 前缀不是匹配：`GITHUB_TOKEN` 不该连带删掉 `GITHUB_TOKEN_OLD`。
	t.Run("只按完整变量名匹配", func(t *testing.T) {
		got := strings.Join(Without([]string{"GITHUB_TOKEN_OLD=x", "GITHUB_TOKEN=y"}, []string{"GITHUB_TOKEN"}), " ")
		if !strings.Contains(got, "GITHUB_TOKEN_OLD=x") {
			t.Errorf("a prefix match would over-remove: %s", got)
		}
	})
}
