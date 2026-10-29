package git

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// v0.106："没跑起来"不是签名结论。
//
// 缺陷形态：`TagSignature` 用 `vres, _ := Run(...)` 吞掉错误，于是一次
// **执行失败**（git 不在 PATH / `run:git` 被拒 / ctx 取消，ExitCode = -1）
// 落进 `SigUntrusted`。用户看到"签名不可信"，去查密钥信任——而真实原因是
// 命令根本没执行。`--require-signed` 又是一个门禁，于是这条把
// "操作失败"说成了"安全结论"。
//
// 判据走纯函数（`verdictFromVerifyTag`）：这一支在真实仓库上很难构造
// （要先有一个带签名的 tag，再让 git 恰好第二次调用起不来）。
func TestV106NoExitCodeIsNotASignatureVerdict(t *testing.T) {
	cases := []struct {
		name    string
		res     *Result
		runErr  error
		wantErr bool
		want    SignatureStatus
	}{
		{
			name:    "未能启动（ExitCode = -1）",
			res:     &Result{ExitCode: -1},
			runErr:  errs.New(errs.CodeGitFetch, "permission denied: run:git", "add run:git to permissions.allow"),
			wantErr: true,
		},
		{
			// 与 exec.go 在 ctx 取消时构造的错误同形：它自己带建议，
			// 因此这条断言检查的是"建议指向执行，而不是密钥"。
			name:    "ctx 取消（同样是 -1）",
			res:     &Result{ExitCode: -1},
			runErr:  errs.New(errs.CodeGitFetch, "git was cancelled", "re-run the command, or raise the timeout"),
			wantErr: true,
		},
		{
			name:   "签名有效",
			res:    &Result{ExitCode: 0},
			want:   SigGood,
		},
		{
			name:   "签名损坏",
			res:    &Result{ExitCode: 1, Stderr: []byte("gpg: BAD signature from ...")},
			want:   SigBad,
		},
		{
			name:   "有签名但我的密钥不认",
			res:    &Result{ExitCode: 1, Stderr: []byte("gpg: Can't check signature: No public key")},
			want:   SigUntrusted,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := verdictFromVerifyTag(tc.res, tc.runErr, "v1.0.0")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("a git that never ran must be an error, got verdict %q — "+
						"that turns an operational failure into a security conclusion", got.Status)
				}
				// 建议必须指向"为什么没跑起来"，而不是密钥。
				if h := errs.Hint(err); h == "" {
					t.Error("the error must say what to do next")
				}
				if strings.Contains(strings.ToLower(errs.Hint(err)), "key") {
					t.Errorf("the hint points at key configuration, but the cause is execution: %q", errs.Hint(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Status != tc.want {
				t.Errorf("status = %q, want %q", got.Status, tc.want)
			}
		})
	}
}
