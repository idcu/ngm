package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/integrations"
	"github.com/idcu/ngm/internal/mappings"
)

// TestV232ViteIntegrationConsumesMappings 固定 E13：Vite 这条不再停在"配置里
// 长得像有 alias"，而是真实生成配置 + 真实调用一次构建，确认 mappings 推导出的
// alias 真的进了产物，且用户的 vite.config.ts 从不被覆盖。
//
// 本机的 Vite 不在 PATH（但 Node 在），因此走仓库内的最小离线替身
// cmd/ngm/testdata/vite-substitute/vite-substitute.cjs：它加载**真实生成的**
// vite.config.ts，执行其中的 specifier/entry 逻辑，把算出的 resolve.alias 写进
// 产物。这是**契约级真调用**，不是真实 Vite；若本机装了 Vite，测试会额外真跑一次
// `vite build`。
//
// 变异检验：把生成配置的 alias 推导改成空数组 → 替身 exit 2 且产物断言红；
// 恢复 → 绿。把 `integrations add vite` 改成覆盖用户配置 → "未被覆盖"红；恢复 → 绿。
func TestV232ViteIntegrationConsumesMappings(t *testing.T) {
	proj := v3UIProject(t)

	// ① 真实生成配置（不是手工拼一份常量）。
	code, out := runCaptureCode(t, "integrations", "add", "vite", "--dir="+proj)
	if code != 0 {
		t.Fatalf("integrations add vite exit=%d:\n%s", code, out)
	}
	config := readProjectFile(t, proj, integrations.ViteConfigPath)
	for _, marker := range []string{"ngm.mappings.json", "resolve", "alias", "specifier(", "replacement:"} {
		if !strings.Contains(config, marker) {
			t.Fatalf("生成的 Vite 配置缺少契约锚点 %q:\n%s", marker, config)
		}
	}

	// ② 从真实 mappings 推导出期望别名——断言的是"配置/调用消费了它"，不是常量。
	f, err := mappings.Read(mappings.Find(proj))
	if err != nil || f == nil || len(f.Mappings) == 0 {
		t.Fatalf("读 ngm.mappings.json 失败或为空: %v", err)
	}
	type wantAlias struct{ specifier, target string }
	var wants []wantAlias
	for _, m := range f.Mappings {
		specifier := m.From
		if m.Path != "" {
			specifier += "/" + m.Path
		}
		target := strings.TrimRight(m.To, "/")
		if m.Main != "" {
			target += "/" + strings.TrimLeft(strings.TrimPrefix(m.Main, "./"), "/")
		}
		wants = append(wants, wantAlias{specifier, target})
	}
	if len(wants) < 2 {
		t.Fatalf("样例项目应有两个子路径别名，实际 %d: %+v", len(wants), wants)
	}

	t.Run("contract-level real build consumes mappings", func(t *testing.T) {
		configBefore := config

		node, nerr := exec.LookPath("node")
		if nerr != nil {
			t.Skip("node not installed; the offline contract-level substitute needs it")
		}
		substitute := filepath.Join(repoRoot(t), "cmd", "ngm", "testdata",
			"vite-substitute", "vite-substitute.cjs")
		if _, serr := os.Stat(substitute); serr != nil {
			t.Fatalf("离线替身缺失: %s (%v)", substitute, serr)
		}
		bcode, bout := runExternal(t, proj, os.Environ(), node, substitute, proj)
		if bcode != 0 {
			t.Fatalf("Vite 替身构建 exit=%d:\n%s", bcode, bout)
		}
		t.Logf("本机无 Vite：使用仓库内最小离线替身（契约级真调用，非真实 Vite）: %s", strings.TrimSpace(bout))

		artifact := readProjectFile(t, proj, "dist/index.js")
		for _, w := range wants {
			if !strings.Contains(artifact, w.specifier) {
				t.Errorf("产物缺少来自 mappings 的别名 %q:\n%s", w.specifier, artifact)
			}
			if !strings.Contains(artifact, w.target) {
				t.Errorf("产物缺少别名落点 %q:\n%s", w.target, artifact)
			}
		}
		if strings.Contains(artifact, "  \n") {
			t.Error("产物里出现了空别名——mappings 可能被当成空数组消费")
		}
		if after := readProjectFile(t, proj, integrations.ViteConfigPath); after != configBefore {
			t.Errorf("构建改写了生成的 Vite 配置:\n%s", after)
		}

		// 真 Vite 若可用则额外真跑一次；缺失是本机环境事实，不是判据失败。
		if vite, verr := exec.LookPath("vite"); verr == nil {
			html := "<html><body><script type=\"module\" src=\"/src/index.ts\"></script></body></html>\n"
			if werr := os.WriteFile(filepath.Join(proj, "index.html"), []byte(html), 0o644); werr != nil {
				t.Fatal(werr)
			}
			vcode, vout := runExternal(t, proj, os.Environ(), vite, "build")
			if vcode != 0 {
				t.Fatalf("real vite build exit=%d:\n%s", vcode, vout)
			}
			t.Logf("本机有 Vite：额外完成一次真实 vite build")
		}
	})

	t.Run("user config is never overwritten", func(t *testing.T) {
		p := v3UIProject(t)
		mine := "// my own vite config; do not touch\n"
		if err := os.WriteFile(filepath.Join(p, integrations.ViteConfigPath), []byte(mine), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out := runCaptureCode(t, "integrations", "add", "vite", "--dir="+p)
		if code != 3 {
			t.Fatalf("已存在的用户配置必须 exit 3，实际 %d:\n%s", code, out)
		}
		if got := readProjectFile(t, p, integrations.ViteConfigPath); got != mine {
			t.Errorf("用户文件被覆盖:\n%s", got)
		}
		if _, err := os.Stat(filepath.Join(p, integrations.TsconfigSidecarPath)); err == nil {
			t.Error("全有或全无：冲突时不应写出 ngm.tsconfig.json")
		}
	})
}
