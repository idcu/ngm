package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// 组 A（v0.2）的端到端验收：供应链策略从 ngm.json 读入，在解析阶段生效。
//
// 单元测试分别覆盖了 resolve 侧的门禁与 supplychain 侧的策略匹配，但**接线**
// （ngm.json → config → policy → GraphOptions.CheckRepo）只有走 CLI 才能证明。
// 本文件补的正是这一段。

// scUpstream 建一个上游 fixture，可同时声明 ngm.json（用于构造传递依赖）。
func scUpstream(t *testing.T, slug, body, manifest string) *testutils.GitRepo {
	t.Helper()
	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", body)
	if manifest != "" {
		r.WriteFile("ngm.json", manifest)
	}
	r.Commit("feat: " + slug)
	r.Tag("v1", false)
	seedMirror(t, slug, r.Dir)
	return r
}

// writeSupplyChain 把 supplyChain 段写进项目的 ngm.json（保留其余字段）。
func writeSupplyChain(t *testing.T, proj, sc string) {
	t.Helper()
	path := filepath.Join(proj, "ngm.json")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var section map[string]any
	if err := json.Unmarshal([]byte(sc), &section); err != nil {
		t.Fatal(err)
	}
	doc["supplyChain"] = section

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestV02SupplyChainAcceptance(t *testing.T) {
	t.Run("transitive dependency outside the allowlist blocks install with a provenance chain", func(t *testing.T) {
		isolateUserEnv(t)

		// evil 是不会被允许的仓库；mid 合法，但它引入了 evil
		scUpstream(t, "github:sc/evil", "export const evil = 1\n", "")
		scUpstream(t, "github:sc/mid", "export const mid = 1\n",
			`{"dependencies":[{"name":"github:sc/evil","ref":"v1","refType":"tag"}]}`)

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/mid@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}

		// 白名单只放行 mid：evil 只能通过传递依赖进入，正是要拦的情况
		writeSupplyChain(t, proj, `{"allowlistRepos":["github.com/sc/mid"]}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a policy rejection must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "allowlistRepos") {
			t.Errorf("the error should name the violated field; got:\n%s", out)
		}
		// 来源链：必须能看出是谁把它引进来的
		const wantChain = "github:sc/mid → github:sc/evil"
		if !strings.Contains(out, wantChain) {
			t.Errorf("the error should carry the provenance chain %q; got:\n%s", wantChain, out)
		}
		// 且拒绝理由指向是**传递依赖**，而不是用户的直接声明
		if !strings.Contains(out, "not in supplyChain.allowlistRepos") {
			t.Errorf("the error should state the actual rule; got:\n%s", out)
		}
	})

	t.Run("an invalid policy value fails with exit 3 and explains the rule", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:sc/ok", "export const ok = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/ok@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}

		// P1M（一个月）被刻意拒绝：长度不固定，会让同一阈值在不同时刻判出不同结果
		writeSupplyChain(t, proj, `{"minimumReleaseAge":"P1M"}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("an invalid policy must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "minimumReleaseAge") {
			t.Errorf("the error should name the offending field; got:\n%s", out)
		}
		if !strings.Contains(out, "28-31") {
			t.Errorf("the error should explain why months are rejected, not just say invalid; got:\n%s", out)
		}
	})

	t.Run("a dependency younger than the minimum release age is blocked", func(t *testing.T) {
		isolateUserEnv(t)

		// fixture 是刚刚提交的，必然比任何晾晒期都新
		scUpstream(t, "github:sc/fresh", "export const fresh = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/fresh@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		writeSupplyChain(t, proj, `{"minimumReleaseAge":"P30D"}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a too-young dependency must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "younger than") {
			t.Errorf("the error should say why it was blocked; got:\n%s", out)
		}
		// 报错必须给出最早可用的时间，否则用户无从决定"等还是走例外"
		if !strings.Contains(out, "wait until") {
			t.Errorf("the error should state when it becomes usable; got:\n%s", out)
		}
	})

	t.Run("an allowlisted repository bypasses the release age gate", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:sc/urgent", "export const urgent = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/urgent@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		// 同一个"刚刚提交"的依赖，但被显式放行 → 紧急通道生效
		writeSupplyChain(t, proj, `{"minimumReleaseAge":"P30D","allowlistRepos":["github.com/sc/urgent"]}`)

		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("an allowlisted repository must not be blocked by the age gate, got %d:\n%s", code, out)
		}
	})

	t.Run("a policy that allows everything declared is not in the way", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:sc/plain", "export const plain = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/plain@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		// 这一条防的是"把没配策略当成全部拒绝"的镜像错误：配了策略也不该误伤
		writeSupplyChain(t, proj, `{"allowedGitHosts":["github.com"],"allowlistRepos":["github.com/sc/*"]}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a satisfied policy must not block install, got %d:\n%s", code, out)
		}
	})
}
