package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/config"
)

// W0/E1：三个示例脚手架的**结构门禁**。
//
// 它只守"目录齐全、清单合法、README 链回唯一指南、命令序列成形、越界词不出现"，
// 不冒充"已经在某台机器上跑通构建"——可运行性由 examples/*/smoke.sh 人工/CI 跑一次
// （tasks.md E1 验收附注）。覆盖集是下面这张显式清单：不在清单里的目录（例如
// examples/third-party-engine，E12 的范围）一概不碰。
func TestV220ExamplesAreRunnableScaffolds(t *testing.T) {
	root := repoRoot(t)
	guide := readRepoFile(t, "docs/guides/examples.md")
	if !strings.Contains(guide, "examples/esbuild-ts/") ||
		!strings.Contains(guide, "examples/local-dir-offline/") ||
		!strings.Contains(guide, "examples/deno-engine/") {
		t.Fatal("唯一权威指南必须链到三个示例目录")
	}

	type scaffold struct {
		dir      string
		commands []string // README 里必须可识别的命令序列（子串）
	}
	cases := []scaffold{
		{
			dir: "examples/esbuild-ts",
			commands: []string{
				"ngm config validate",
				"ngm build --engine=esbuild",
				"ngm transform ",
				"ngm integrations add esbuild",
				"ngm mappings validate",
			},
		},
		{
			dir: "examples/local-dir-offline",
			commands: []string{
				"ngm config validate",
				"ngm install --offline",
				"ngm verify --offline",
			},
		},
		{
			dir: "examples/deno-engine",
			commands: []string{
				"ngm config validate",
				"ngm engines validate",
				"ngm engines list --json",
				"ngm typecheck --engine=deno",
				"ngm build --engine=deno",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			dir := filepath.Join(root, filepath.FromSlash(tc.dir))
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				t.Fatalf("示例目录缺失：%s (%v)", tc.dir, err)
			}

			// ① ngm.json 必须存在且合法；schemaVersion 必须**显式**写 3。
			rel := tc.dir + "/ngm.json"
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatalf("读 %s: %v", rel, err)
			}
			var raw struct {
				SchemaVersion int `json:"schemaVersion"`
			}
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("%s 不是合法 JSON: %v", rel, err)
			}
			if raw.SchemaVersion != config.SchemaVersion {
				t.Errorf("%s: schemaVersion = %d，要求显式 %d", rel, raw.SchemaVersion, config.SchemaVersion)
			}
			pf, err := config.ParseProjectFile(data, rel)
			if err != nil {
				t.Fatalf("%s 解析失败: %v", rel, err)
			}
			if err := pf.Validate(); err != nil {
				t.Fatalf("%s 清单不合法: %v", rel, err)
			}

			// ② README 链回唯一指南，且不复制它的正文；命令序列可识别。
			readme := readRepoFile(t, tc.dir+"/README.md")
			if !strings.Contains(readme, "](../../docs/guides/examples.md)") {
				t.Error("README 必须链回 docs/guides/examples.md（唯一权威指南）")
			}
			for _, onlyInGuide := range []string{"## 共同的边界", "## 命令对照"} {
				if strings.Contains(readme, onlyInGuide) {
					t.Errorf("README 复制了指南正文的章节 %q", onlyInGuide)
				}
			}
			for _, cmd := range tc.commands {
				if !strings.Contains(readme, cmd) {
					t.Errorf("README 缺少可识别的命令 %q", cmd)
				}
			}

			// ③ smoke 脚本骨架存在（不要求联网、也不要求真的跑过）。
			if _, err := os.Stat(filepath.Join(dir, "smoke.sh")); err != nil {
				t.Errorf("缺少 smoke.sh 骨架: %v", err)
			}
		})
	}

	// ④ deno-engine 必须显式声明引擎清单，且声明本身合法。
	catData := readRepoFile(t, "examples/deno-engine/ngm.engines.json")
	dec := json.NewDecoder(strings.NewReader(catData))
	dec.DisallowUnknownFields()
	var cat adapter.Catalog
	if err := dec.Decode(&cat); err != nil {
		t.Fatalf("deno 引擎清单解析失败: %v", err)
	}
	if cat.Version != adapter.CatalogVersion {
		t.Errorf("deno 引擎清单 version = %d，要求 %d", cat.Version, adapter.CatalogVersion)
	}
	declared := map[string]bool{}
	for _, e := range cat.Engines {
		if e.Name == "deno" && e.Adapter == adapter.AdapterSubprocess {
			declared[string(e.Kind)] = true
		}
	}
	for _, kind := range []adapter.EngineKind{adapter.KindTypeCheck, adapter.KindBundle} {
		if !declared[string(kind)] {
			t.Errorf("deno 引擎清单缺少 kind=%s 的 subprocess 条目", kind)
		}
	}

	// ⑤ local-dir-offline 全程不出现越界词（所有文件，大小写不敏感）。
	localDir := filepath.Join(root, "examples", "local-dir-offline")
	err := filepath.WalkDir(localDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		lower := strings.ToLower(string(body))
		for _, banned := range []string{"registry", "semver"} {
			if strings.Contains(lower, banned) {
				t.Errorf("%s 出现越界词 %q", filepath.ToSlash(path), banned)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描 local-dir-offline: %v", err)
	}
}
