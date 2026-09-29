package resolve_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/testutils"
)

// denied 是测试用的最小策略：拒绝指定 repo，其余放行。
//
// 用假的判定函数而不是真的 supplychain.Policy：这里要验的是**图解析侧的门禁**
// （时机、来源链、退出码），策略本身的语义在 internal/supplychain 有自己的测试。
func denied(t *testing.T, blocked ...string) func(host, repoPath string) error {
	t.Helper()
	return func(host, repoPath string) error {
		for _, b := range blocked {
			if host+"/"+repoPath == b {
				return errs.New(errs.CodeConfigInvalid,
					"blocked by policy: "+b,
					"add "+b+" to supplyChain.allowlistRepos if it is trusted")
			}
		}
		return nil
	}
}

// TestPolicyGate_TransitiveDependencyIsRejected 是组 A 的核心验收：
// 传递依赖命中未授权仓库 → 阻断（exit 3）并输出完整来源链。
func TestPolicyGate_TransitiveDependencyIsRejected(t *testing.T) {
	w := newFixtureWorld(t)
	w.add("github:w/evil", leafRepo(t, "v1"))

	// 注意：repoWithDeps 只提交、不自动打 tag——按 tag 解析必须自己补
	mid := repoWithDeps(t, dep("github:w/evil", "v1"))
	mid.Tag("v1", false)
	w.add("github:w/mid", mid)

	// 记录 mirror 请求：用来证明门禁发生在**触网之前**
	base := w.opts()
	var ensured []string
	opts := base
	opts.EnsureMirror = func(ctx context.Context, repo resolve.Canonical) (string, error) {
		ensured = append(ensured, repo.Slug())
		return base.EnsureMirror(ctx, repo)
	}
	opts.CheckRepo = denied(t, "github.com/w/evil")

	_, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/mid", Ref: "v1", RefType: resolve.RefTypeTag},
	}, opts)

	if err == nil {
		t.Fatal("a transitive dependency outside the allowlist must be rejected")
	}

	// 退出码：策略拒绝属于配置/策略错误（3），CI 需要能与网络失败（4）区分
	if code := errs.ExitCode(err); code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}

	var ne *errs.NgmError
	if !errors.As(err, &ne) {
		t.Fatalf("want *errs.NgmError, got %T", err)
	}

	// 来源链：必须能从根一路看到违规节点
	const wantChain = "(root) → github:w/mid → github:w/evil"
	if !strings.Contains(ne.Message, wantChain) {
		t.Errorf("message should carry the provenance chain %q; got:\n%s", wantChain, ne.Message)
	}

	// Hint 必须被保留（withProvenance 只补链路，不改写策略层给出的修法）
	if !strings.Contains(ne.Hint, "allowlistRepos") {
		t.Errorf("the policy hint must survive; got: %q", ne.Hint)
	}

	// **触网之前**判定：违规仓库的 mirror 不该被请求
	for _, s := range ensured {
		if strings.Contains(s, "evil") {
			t.Errorf("the gate must run before any remote access, but %q was ensured (all: %v)", s, ensured)
		}
	}
}

// TestPolicyGate_NoPolicyDoesNotGate 守住反面：未配置策略时不得有任何拦截。
//
// 这一条防的是"把没配策略当成全部拒绝"——那会让所有既有项目突然装不上。
func TestPolicyGate_NoPolicyDoesNotGate(t *testing.T) {
	w := newFixtureWorld(t)
	w.add("github:w/a", leafRepo(t, "v1"))

	// CheckRepo 保持 nil（未配置策略）
	g, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/a", Ref: "v1", RefType: resolve.RefTypeTag},
	}, w.opts())
	if err != nil {
		t.Fatalf("without a policy nothing may be blocked: %v", err)
	}
	if g.Len() != 1 {
		t.Errorf("nodes = %d, want 1", g.Len())
	}
}

// TestPolicyGate_RootViolation 根声明违规时，来源链应显示它来自根。
func TestPolicyGate_RootViolation(t *testing.T) {
	w := newFixtureWorld(t)
	w.add("github:w/direct", leafRepo(t, "v1"))

	opts := w.opts()
	opts.CheckRepo = denied(t, "github.com/w/direct")

	_, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/direct", Ref: "v1", RefType: resolve.RefTypeTag},
	}, opts)
	if err == nil {
		t.Fatal("a root declaration outside the allowlist must be rejected")
	}
	const wantChain = "(root) → github:w/direct"
	if !strings.Contains(err.Error(), wantChain) {
		t.Errorf("chain should show the root origin %q; got:\n%s", wantChain, err.Error())
	}
}

// TestPolicyGate_ChainNamesTheMonorepoNode 固定 v0.2 设计复核里那条发现。
//
// 来源链用**节点 Key**（name#subPath），不是 Name——否则同一仓库的多个子路径节点
// 会被显示成同一个节点，使用者会去改错地方。
//
// 本用例同时记录一条实现事实：子路径节点的传递依赖取自**仓库根**的 ngm.json
// （readUpstreamDeps 不拼接 SubPath），因此 core 与 web 两个节点会各自引入同一个依赖，
// 正好构成菱形。来源链只显示一条路径，但必须说明还有别的。
func TestPolicyGate_ChainNamesTheMonorepoNode(t *testing.T) {
	w := newFixtureWorld(t)

	mono := testutils.NewGitRepo(t)
	mono.WriteFile("packages/core/index.ts", "core\n")
	mono.WriteFile("packages/web/index.ts", "web\n")
	mono.WriteFile("ngm.json",
		`{"dependencies":[{"name":"github:w/evil","ref":"v1","refType":"tag"}]}`)
	mono.Commit("mono")
	mono.Tag("v1", false)

	w.add("github:w/evil", leafRepo(t, "v1"))
	w.add("github:w/mono", mono)

	opts := w.opts()
	opts.CheckRepo = denied(t, "github.com/w/evil")

	_, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/mono", Ref: "v1", RefType: resolve.RefTypeTag, SubPath: "packages/core"},
		{Name: "github:w/mono", Ref: "v1", RefType: resolve.RefTypeTag, SubPath: "packages/web"},
	}, opts)
	if err == nil {
		t.Fatal("the dependency declared by the monorepo must be rejected")
	}

	// 链上必须出现带子路径的 Key，而不是裸的仓库名
	const wantSubPath = "github:w/mono#packages/"
	if !strings.Contains(err.Error(), wantSubPath) {
		t.Errorf("the chain should name a sub-path node (%q); got:\n%s", wantSubPath, err.Error())
	}
	// 两个子路径节点都引入了它 → 必须披露"还有别的路径"
	if !strings.Contains(err.Error(), "more path(s)") {
		t.Errorf("the chain should disclose the other introducing paths; got:\n%s", err.Error())
	}
}
