package git

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/security"
)

// TestHostFromRemoteURL 固定"什么算网络访问"这条判断。
//
// 它决定 net 权限门禁会不会误伤：把本地路径当成网络，纯离线的 fetch 会被拦下，
// 而用户会被提示去加一条与他操作无关的权限。
func TestHostFromRemoteURL(t *testing.T) {
	cases := []struct {
		in     string
		host   string
		isNet  bool
		reason string
	}{
		{"https://github.com/org/repo.git", "github.com", true, "https"},
		{"https://x-access-token:ghp_x@github.com/org/repo.git", "github.com", true, "userinfo 不影响 host"},
		{"ssh://git@github.com/org/repo.git", "github.com", true, "ssh URL"},
		{"git@github.com:org/repo.git", "github.com", true, "scp 形式"},
		{"git@gitee.com:org/repo.git", "gitee.com", true, "scp 形式（另一个 host）"},
		{"foo:bar", "foo", true, "无点单 token 仍按 scp host 判定——net 门禁必须保持 fail-closed，不能因为形状可疑就当本地路径放行"},
		{"github.com:org/repo.git", "github.com", true, "无 @ 但带点的合法 scp"},
		{"git://example.com/repo.git", "example.com", true, "git 协议"},
		{"/srv/git/repo.git", "", false, "绝对路径"},
		{"./vendor/repo.git", "", false, "相对路径"},
		{"../sibling/repo.git", "", false, "父目录相对路径"},
		{"file:///srv/git/repo.git", "", false, "file URL"},
		{`C:\repos\repo.git`, "", false, "Windows 盘符"},
		{"C:/repos/repo.git", "", false, "Windows 盘符（正斜杠）"},
		{`\\server\share\repo.git`, "", false, "UNC"},
		{"", "", false, "空"},
	}
	for _, tc := range cases {
		host, ok := HostFromRemoteURL(tc.in)
		if ok != tc.isNet {
			t.Errorf("HostFromRemoteURL(%q) isNet=%v, want %v (%s)", tc.in, ok, tc.isNet, tc.reason)
			continue
		}
		if ok && host != tc.host {
			t.Errorf("HostFromRemoteURL(%q) host=%q, want %q", tc.in, host, tc.host)
		}
	}
}

// CheckNetAccess 只在**真的走网络**时才要求权限。
func TestCheckNetAccess(t *testing.T) {
	pol, err := security.NewPolicy(nil, nil, "~/.ngm/config.json")
	if err != nil {
		t.Fatal(err)
	}

	if err := CheckNetAccess(pol, "https://github.com/org/repo.git"); err == nil {
		t.Error("a remote URL needs net permission")
	}
	// 本地路径不需要——这条是"不误伤离线操作"的可执行形式
	for _, local := range []string{"/srv/git/repo.git", "./repo.git", `C:\repos\repo.git`, "file:///srv/repo.git"} {
		if err := CheckNetAccess(pol, local); err != nil {
			t.Errorf("a local path must not need net permission (%s): %v", local, err)
		}
	}

	granted, err := security.NewPolicy([]string{"net:github.com"}, nil, "~/.ngm/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckNetAccess(granted, "https://github.com/org/repo.git"); err != nil {
		t.Errorf("github.com was allowed: %v", err)
	}
	if err := CheckNetAccess(granted, "https://gitlab.com/org/repo.git"); err == nil {
		t.Error("allowing github.com must not allow gitlab.com")
	}
	// 提示要指向具体的那一条
	err = CheckNetAccess(pol, "https://gitlab.com/org/repo.git")
	if err == nil || !strings.Contains(err.Error(), "net:gitlab.com") {
		t.Errorf("the error should name the exact permission: %v", err)
	}

	// 没有策略（配置未加载）时放行：这是调用方没拿到配置的情形，
	// 与"策略判定为默认档位"是两件事。
	if err := CheckNetAccess(nil, "https://github.com/org/repo.git"); err != nil {
		t.Errorf("a nil policy means no enforcement here: %v", err)
	}
}

// TestValidateRemoteURLRejectsAmbiguousScp 固定 H2（评估报告 P3）：
// `foo:bar` 此前被当作合法 scp，net 门禁于是报 "need net:foo"——用户从没写过 host foo，
// 提示把他引向一条无关权限。现在在 clone 前就拒绝这种形状并点名正确写法。
//
// 关键：这里**不**改 HostFromRemoteURL 的 isNet（上面用例固定它仍为 true）。
// 若把它改成 false，CheckNetAccess 会放行，而 git 仍把 `foo:bar` 当 scp 远端解析 →
// 真实触网却绕过 net 门禁（fail-open）。拒绝形状与保持 net fail-closed 必须同时成立。
func TestValidateRemoteURLRejectsAmbiguousScp(t *testing.T) {
	bad := []string{"foo:bar", "repo:main", "abc:def/ghi"}
	for _, in := range bad {
		err := ValidateRemoteURL(in)
		if err == nil {
			t.Errorf("ValidateRemoteURL(%q) accepted an ambiguous scp-like token", in)
			continue
		}
		hint := errs.Hint(err)
		if !strings.Contains(err.Error(), in) || !strings.Contains(hint, "user@host:path") {
			t.Errorf("ValidateRemoteURL(%q) error should name the token (%v) and hint the fix (%q)", in, err, hint)
		}
	}

	good := []string{
		"git@github.com:org/repo.git", // 带 @
		"github.com:org/repo.git",     // 无 @ 但带点
		"git@localhost:org/repo.git",  // localhost
		"https://github.com/org/repo.git",
		"/srv/git/repo.git",
	}
	for _, in := range good {
		if err := ValidateRemoteURL(in); err != nil {
			t.Errorf("ValidateRemoteURL(%q) rejected a legal form: %v", in, err)
		}
	}
}
