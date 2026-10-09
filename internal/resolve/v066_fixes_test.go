package resolve

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// v0.66：三条来自外部评估报告的修复的断言。
//
//	① 短 commit 必须在 mirror 里补齐为 40 位（否则 lock 写入合法、读回非法）
//	② URL 里的 `@` 若落在**路径**中，不得被当成 userinfo 剥离
//	③ 图解析的 `ls-remote` 必须走 `run:git` 门禁（Policy 透传）

// TestV66ShortCommitIsExpandedInMirror 固定"解析到哪个 commit 就锁哪个 commit"。
//
// 修复前的行为：4 位以上的短 hash 被**原样**返回，一路写进 lock，
// 而 lock 的 Validate 要求 40 位 ⇒ 下一次读 lock 直接 exit 3，且用户无法
// 通过正常路径修复（评估报告 P2#6 的往返不一致）。
func TestV66ShortCommitIsExpandedInMirror(t *testing.T) {
	dir, head := fixtureRepo(t)
	repo := MustNormalize("github:org/repo")

	short := head[:8]
	got, err := ResolveRef(context.Background(), repo, short, RefTypeCommit, ResolveOptions{GitURL: dir})
	if err != nil {
		t.Fatalf("ResolveRef(short, with mirror): %v", err)
	}
	if got != head {
		t.Errorf("短 hash 必须补齐为完整 hash：got %q, want %q", got, head)
	}
	if len(got) != 40 {
		t.Errorf("补齐结果是 %d 位，lock 要求 40 位", len(got))
	}
}

// TestV66ShortCommitWithoutObjectIsRejected：mirror 里找不到时**报错**，
// 绝不把短 hash 交给下游。
func TestV66ShortCommitWithoutObjectIsRejected(t *testing.T) {
	dir, _ := fixtureRepo(t)
	repo := MustNormalize("github:org/repo")

	_, err := ResolveRef(context.Background(), repo, "abcdef1234", RefTypeCommit, ResolveOptions{GitURL: dir})
	if err == nil {
		t.Fatal("mirror 里不存在的短 hash 必须被拒绝")
	}
	if code := errs.ExitCode(err); code != 3 {
		t.Errorf("exit code = %d, want 3（声明与实际不符属于配置类）", code)
	}
}

// TestV66AtSignInPathIsNotUserinfo 固定评估报告 P2#9。
//
// 修复前 `LastIndex("@")` 作用在整个 URL 尾段上：
//
//	https://evil.example.com/a@github.com/org/repo
//	                        ^ 这个 @ 在**路径**里，却被当成 userinfo 分隔符
//
// ⇒ 解析出 `github.com/org/repo`——一个**不同的仓库**。
// 修复后要么完整保留路径里的 `@`，要么明确拒绝；两者都不允许"解析成另一个仓库"。
func TestV66AtSignInPathIsNotUserinfo(t *testing.T) {
	got, err := Normalize("https://evil.example.com/a@github.com/org/repo")
	t.Logf("探针：Normalize → host=%q path=%q err=%v", got.Host, got.Path, err)
	if err != nil {
		// 拒绝也是安全的：关键是不能解析成 github.com。
		if strings.Contains(err.Error(), "github.com") && !strings.Contains(err.Error(), "evil.example.com") {
			t.Errorf("错误提到了 github.com 而没提 evil.example.com——@ 又被当成 userinfo 了: %v", err)
		}
		return
	}
	if got.Host != "evil.example.com" {
		t.Errorf("Host = %q, want evil.example.com（@ 必须留在路径里，不能把路径后半段当 host）", got.Host)
	}
	if got.Path != "a@github.com/org/repo" {
		t.Errorf("Path = %q, want a@github.com/org/repo", got.Path)
	}
}

// TestV66RealUserinfoIsStillStripped 是上一条的对照：真正的 userinfo 仍要剥。
func TestV66RealUserinfoIsStillStripped(t *testing.T) {
	cases := []struct {
		in       string
		wantHost string
		wantPath string
	}{
		{"https://user:pass@example.com/org/repo", "example.com", "org/repo"},
		{"https://user@example.com/org/repo", "example.com", "org/repo"},
		// 密码里含 @：剥离必须取 host 段里的**最后一个** @。
		{"https://user:p@ss@example.com/org/repo", "example.com", "org/repo"},
	}
	for _, tc := range cases {
		got, err := Normalize(tc.in)
		if err != nil {
			t.Errorf("Normalize(%q): %v", tc.in, err)
			continue
		}
		if got.Host != tc.wantHost || got.Path != tc.wantPath {
			t.Errorf("Normalize(%q) = %s/%s, want %s/%s", tc.in, got.Host, got.Path, tc.wantHost, tc.wantPath)
		}
	}
}

// （Policy 透传的断言在 graph_policy_forward_test.go 里：它需要
// graph_test.go 的 fixture world，而那是**外部测试包** resolve_test——
// 两个测试包不能共享助手，所以分两个文件。）
