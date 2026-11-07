package main

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV128ModuleVersionHasOneRule 固定 strict 与 plan 对"版本是否合法"只有一份答案。
//
// 此前 semverLike 接受 `1.2.3-rc1`，而 rePlanVersion 只接受 `x.y.z`：
// `modules validate --strict` 说通过、`modules plan` 说不是 semver。
func TestV128ModuleVersionHasOneRule(t *testing.T) {
	cases := []struct {
		in     string
		semver bool
		plan   bool
		next   string // plan 接受时的 patch 进位结果
	}{
		{in: "1.2.3", semver: true, plan: true, next: "1.2.4"},
		{in: "v1.2.3", semver: true, plan: true, next: "1.2.4"},
		{in: "1.2.3-rc1", semver: true, plan: false},
		{in: "1.2.3+build.5", semver: true, plan: false},
		{in: "1.2", semver: false, plan: false},
		{in: "1.2.3.4", semver: false, plan: false},
		{in: "1.2.x", semver: false, plan: false},
		// 溢出：正则匹配但 Atoi 失败——旧实现把错误丢掉后当成 0 进位。
		{in: "18446744073709551616.0.0", semver: false, plan: false},
	}
	for _, tc := range cases {
		if got := semverLike(tc.in); got != tc.semver {
			t.Errorf("semverLike(%q) = %v, want %v", tc.in, got, tc.semver)
		}
		got, err := nextModuleVersion(tc.in, "patch")
		if tc.plan {
			if err != nil {
				t.Errorf("nextModuleVersion(%q) errored: %v", tc.in, err)
				continue
			}
			if got != tc.next {
				t.Errorf("nextModuleVersion(%q) = %q, want %q", tc.in, got, tc.next)
			}
			// 一致性：plan 接受的版本，strict 必须也接受。
			if !semverLike(tc.in) {
				t.Errorf("%q: plan accepts but strict rejects", tc.in)
			}
			continue
		}
		if err == nil {
			t.Errorf("nextModuleVersion(%q) = %q, want an error", tc.in, got)
		}
	}
}

func TestV128NextModuleVersionBumps(t *testing.T) {
	cases := []struct{ current, bump, want string }{
		{"1.2.3", "major", "2.0.0"},
		{"1.2.3", "minor", "1.3.0"},
		{"1.2.3", "patch", "1.2.4"},
		{"0.9.9", "major", "1.0.0"},
	}
	for _, tc := range cases {
		got, err := nextModuleVersion(tc.current, tc.bump)
		if err != nil {
			t.Fatalf("nextModuleVersion(%q,%q): %v", tc.current, tc.bump, err)
		}
		if got != tc.want {
			t.Errorf("nextModuleVersion(%q,%q) = %q, want %q", tc.current, tc.bump, got, tc.want)
		}
	}
}

// TestV128ModulesOriginWarningIsRedacted 是脱敏规则的端到端判据。
//
// `modules index` 读不到可解析的 origin 时会把原始地址写进 warning；
// 若那里内嵌凭据，CI 日志就会把它们打印出来。这里用一个 Normalize 必然
// 拒绝的 scheme，同时保留 host/path 以便用户还能认出是哪个 remote。
func TestV128ModulesOriginWarningIsRedacted(t *testing.T) {
	testutils.AllowEngines(t, "git")
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/ngm.json", `{"name":"a","version":"1.0.0"}`)
	repo.Commit("init")
	repo.SetRemote("origin", "ftp://user:tok3n@example.invalid/org/repo.git")

	code, out := runCaptureCode(t, "modules", "index", "--dir="+repo.Dir, "--dry-run")
	if code != 0 {
		t.Fatalf("modules index exit=%d:\n%s", code, out)
	}
	if strings.Contains(out, "tok3n") {
		t.Fatalf("origin credentials leaked into output:\n%s", out)
	}
	if !strings.Contains(out, "example.invalid/org/repo.git") {
		t.Fatalf("redaction dropped the address itself:\n%s", out)
	}
}
