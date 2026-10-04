package main

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV14EngineKindsWithoutBuiltinDefaultNeedDeclaration 把"哪些 kind 会被自动选中"钉住。
//
// 为什么需要它：v0.14 复核发现**三份文档**都声称 `ngm typecheck` / `ngm css`
// "内置、装了就能用"，而实现要求**声明**（`engines.<kind>` 或 `--engine`）——
// 内置的**默认选择**只覆盖 `bundle` / `transform`（`builtinDefaultFor`），
// 而内置**清单**覆盖得更多（`typescript` 管 typeCheck/typeDecl、`postcss` 管 css）。
// **"在清单里" ≠ "会被自动选中"**，这一处混淆让三份文档同时说错：
//
//   - docs/README（"`ngm typecheck` 内置 `typescript`（装了就能用）"）
//   - docs/guides/cli.md（把 css 的引擎写成 `esbuild`）
//   - docs/internals/capability-matrix.md（CSS/SCSS 一栏写 `postcss / esbuild`）
//
// 其中最后一条是**实测证伪**的：`ngm css --engine=esbuild` 报
// "no `css` engine named \"esbuild\""——esbuild 覆盖的是 bundle / transform，不是 css。
//
// 这条测试的判据全部取自**运行结果**，因此文档再漂回去时它会红。
func TestV14EngineKindsWithoutBuiltinDefaultNeedDeclaration(t *testing.T) {
	isolateUserEnv(t)
	proj := newProject(t) // `ngm init` 只声明 engines.transform 与 engines.bundle
	probeCSS := "src/index.css"
	testutils.WriteFile(t, proj, probeCSS, "body { color: red }\n")

	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantSee  string
		why      string
	}{
		{
			name:     "bundle has a built-in default (esbuild)",
			args:     []string{"build", "src/index.ts", "--dry-run"},
			wantCode: 0,
			wantSee:  "esbuild",
			why:      "bundle / transform 是**唯一**两个有内置默认选择的 kind",
		},
		{
			name:     "typeCheck without a declaration is a config error",
			args:     []string{"typecheck"},
			wantCode: 3,
			wantSee:  "engines.typeCheck",
			why:      "未声明 = 缺配置（不是'引擎没装'）",
		},
		{
			name:     "typeDecl without a declaration is a config error",
			args:     []string{"typedecl", "--outdir=types"},
			wantCode: 3,
			wantSee:  "engines.typeDecl",
			why:      "同上",
		},
		{
			name:     "css without a declaration is a config error",
			args:     []string{"css", probeCSS},
			wantCode: 3,
			wantSee:  "engines.css",
			why:      "同上。内置清单里 css 只有 postcss 与 self",
		},
		{
			name:     "esbuild is not a css engine",
			args:     []string{"css", probeCSS, "--engine=esbuild"},
			wantCode: 3,
			wantSee:  "in the catalog",
			why:      "三份文档曾把 esbuild 写成 css 引擎；实测它在清单里只覆盖 bundle / transform",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{}, tc.args...)
			args = append(args, "--dir="+proj)
			code, out := runCaptureCode(t, args...)
			if code != tc.wantCode {
				t.Fatalf("exit=%d want %d (%s)\n%s", code, tc.wantCode, tc.why, out)
			}
			if !strings.Contains(out, tc.wantSee) {
				t.Errorf("output lacks %q (%s):\n%s", tc.wantSee, tc.why, out)
			}
		})
	}

	// esbuild 与 typeCheck 的关系要分**两步**测：它取决于清单里有没有那个名字，
	// 而"名字在不在"决定退出码是 3 还是 5——这个差别不是措辞问题（改名字 vs 换引擎）。
	// docs/guides/build.md 此前只给了后半段，于是示例命令在默认项目上跑不出它展示的输出。
	t.Run("esbuild is not a typeCheck engine in the built-in catalog", func(t *testing.T) {
		fresh := newProject(t)
		code, out := runCaptureCode(t, "typecheck", "--engine=esbuild", "--dir="+fresh)
		if code != 3 {
			t.Fatalf("exit=%d want 3: `--engine` is looked up **per kind**, and the built-in catalog has no esbuild/typeCheck entry\n%s", code, out)
		}
		if !strings.Contains(out, "in the catalog") {
			t.Errorf("the message must say the name is unknown *for this kind*:\n%s", out)
		}
	})

	t.Run("esbuild refuses type-check once declared for that kind", func(t *testing.T) {
		fresh := m6Project(t, `{}`)
		m6WriteCatalog(t, fresh, m6CatalogEntry{
			Name: "esbuild", Kind: "typeCheck", Adapter: "subprocess", Command: "esbuild",
		})
		code, out := runCaptureCode(t, "typecheck", "--engine=esbuild", "--dir="+fresh)
		if code != 5 {
			t.Fatalf("exit=%d want 5: capability is checked before availability, and esbuild cannot type-check\n%s", code, out)
		}
		if !strings.Contains(out, "does not type-check") {
			t.Errorf("the refusal must say *why* (it only strips annotations):\n%s", out)
		}
	})
}
