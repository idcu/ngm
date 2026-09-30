package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// writeGlobalConfig 写入隔离环境里的 `~/.ngm/config.json`。
func writeGlobalConfig(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".ngm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestV03PermissionsAcceptance 是 v0.3 D 组的端到端验收：权限配置**真的被施加**。
//
// 在此之前 `permissions` 段只被解析、从未被使用——写下 `deny` 不会阻止任何事。
// 因此这里测的不是"能不能解析"，而是"命令的行为会不会因此改变"。
func TestV03PermissionsAcceptance(t *testing.T) {
	// 需要访问远端却被默认档位拦下：exit 3 + 指出该加哪一条。
	//
	// 这个用例同时证明了"门禁在任何网络 I/O 之前"：如果它跑到网络那一步，
	// 得到会是 clone 失败的 exit 4，而不是权限的 exit 3。
	t.Run("a network fetch is refused until net: is granted", func(t *testing.T) {
		isolateUserEnv(t)

		proj := newProject(t)
		// 只声明、不预置 mirror：解析时非去远端不可
		if code, out := runCaptureCode(t, "add", "github:perm/gate@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a fetch without net: permission must exit 3, got %d:\n%s", code, out)
		}
		for _, want := range []string{"net:github.com", "permissions.allow", "config.json"} {
			if !strings.Contains(out, want) {
				t.Errorf("the report should contain %q so the user can act:\n%s", want, out)
			}
		}
	})

	// 反向纪律：**本地路径不是网络访问**。
	//
	// 若按 slug 的主机名判定，本地 mirror（测试、CI 预置镜像、本地裸仓库）
	// 会被当成网络操作拦下，用户则被提示去加一条与他操作无关的权限。
	t.Run("a local mirror needs no permission at all", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:perm/local", "export const x = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:perm/local@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("a mirror seeded from a local path must install without any permission, got %d:\n%s", code, out)
		}
	})

	// 写错的权限必须立刻报出来：把它当成"没配"会让用户以为整份列表都生效了。
	t.Run("an unknown namespace fails loudly", func(t *testing.T) {
		home := isolateUserEnv(t)
		writeGlobalConfig(t, home, `{"permissions":{"allow":["fetch:github.com"]}}`)

		proj := newProject(t)
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a malformed permission list must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "unknown permission namespace") {
			t.Errorf("the error should say what is wrong:\n%s", out)
		}
		if !strings.Contains(out, "net") {
			t.Errorf("the error should list the valid namespaces:\n%s", out)
		}
	})

	// `run:git` 被拒绝时不得启动 git：否则用户会从"git 失败了"去猜原因。
	t.Run("denying run:git stops git from starting", func(t *testing.T) {
		home := isolateUserEnv(t)
		writeGlobalConfig(t, home, `{"permissions":{"deny":["run:git"]}}`)

		// 预置 mirror，确保失败只可能来自权限而不是"找不到仓库"
		scUpstream(t, "github:perm/nogit", "export const y = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:perm/nogit@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("denying run:git must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "run:git") {
			t.Errorf("the report should name the denied permission:\n%s", out)
		}
	})

	// 权限判定排在**可用性之后**：引擎根本没装时，正确的退出码是 5
	// （工具缺失，脚本据此区分"环境问题"与"配置问题"），而不是权限错误。
	//
	// 顺序反了的话，一个拼错的引擎名会被报成"请把 run:ngm-definitely-not-a-real-engine
	// 加进配置"——用户照做之后仍然跑不了，而那条权限永远不会有用。
	t.Run("a missing engine still exits 5, not a permission error", func(t *testing.T) {
		isolateUserEnv(t)
		proj := m6Project(t, `{"bundle": "ghost"}`)
		// 已声明、但命令不在 PATH：这才是"引擎不可用"（exit 5）的成立条件。
		// 名字不在清单里是另一回事（配置错误，exit 3）。
		testutils.WriteFile(t, proj, "ngm.engines.json", `{
  "version": 1,
  "engines": [
    {"name": "ghost", "kind": "bundle", "adapter": "subprocess", "command": "ngm-definitely-not-a-real-engine"}
  ]
}
`)

		code, out := runCaptureCode(t, "build", "--dir="+proj)
		if code != 5 {
			t.Fatalf("a missing engine must exit 5, got %d:\n%s", code, out)
		}
		if strings.Contains(out, "permissions.allow") {
			t.Errorf("a missing engine must not be reported as a permission problem:\n%s", out)
		}
	})

	// 允许列表只放行**那一条**，不是整个命名空间。
	t.Run("granting one host does not grant another", func(t *testing.T) {
		home := isolateUserEnv(t)
		writeGlobalConfig(t, home, `{"permissions":{"allow":["net:gitee.com"]}}`)

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:perm/other@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("expected a denial, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "net:github.com") {
			t.Errorf("the report should name the missing permission:\n%s", out)
		}
	})
}
