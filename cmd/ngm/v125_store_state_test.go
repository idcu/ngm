package main

import (
	"os"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/testutils"
)

// D1-重做的表驱动判据：content store 的四种条目形状必须各自落到**正确**的
// 退出码，且报告能定位到条目。
//
// 关键在"被截断"这一格：默认 hardlink 布局下 vendor 与 content store 的 blob
// 是**同一 inode**，就地截断 vendor 会同时改变 blob 的大小。若 verify 把
// "大小不符"读成"条目未就绪"（`Has()` 的旧语义），这份已确证的篡改就会被
// 报成资源缺失（exit 4）。本表钉住的是：它是 critical（exit 2），而不是 4。
//
// 对照的"内容不符"用 copy 布局：等长改写只看 size 检不出，必须由 --content
// 的字节比对来定位到具体文件——这条路径与 hardlink 那条互为补充。
func TestV125StoreEntryStates(t *testing.T) {
	testutils.MustHaveGit(t)

	const slug = "github:m5/lib"

	sameLengthEdit := func(t *testing.T, path string) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) == 0 {
			t.Fatalf("fixture file is empty")
		}
		raw[0] = 'E'
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name            string
		linkMode        string // "" → 默认（hardlink）
		mutate          func(t *testing.T, proj string, dep lock.Dependency)
		wantCode        int
		wantCritical    int
		wantOperational int
		// wantDetail 是失败检查的 detail 里必须出现的片段（报告定位）。
		wantDetail []string
		// wantProof 是 content proof 里必须出现的 "<path>:<kind>"。
		wantProof []string
	}{
		{
			name:     "正常",
			wantCode: 0,
		},
		{
			name: "条目缺失（blob 被删）",
			mutate: func(t *testing.T, proj string, dep lock.Dependency) {
				t.Helper()
				if err := os.Remove(m5ContentFile(t, dep.ContentDigest(), "index.ts")); err != nil {
					t.Fatal(err)
				}
			},
			wantCode:        4,
			wantOperational: 1,
			wantDetail:      []string{"content store has no usable entry", "index.ts is missing"},
		},
		{
			name: "被截断（hardlink 下 blob 同步变小）",
			mutate: func(t *testing.T, proj string, dep lock.Dependency) {
				t.Helper()
				p := m5VendorFile(proj, "github.com/m5/lib", "", "index.ts")
				raw, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				if len(raw) < 2 {
					t.Fatalf("fixture file is too small to truncate")
				}
				if err := os.WriteFile(p, raw[:len(raw)/2], 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantCode:     2,
			wantCritical: 1,
			wantDetail:   []string{"is damaged", "index.ts", "truncated or tampered content"},
			wantProof:    []string{"index.ts:size"},
		},
		{
			name:     "内容不符（等长改写，copy 布局）",
			linkMode: "copy",
			mutate: func(t *testing.T, proj string, dep lock.Dependency) {
				t.Helper()
				sameLengthEdit(t, m5VendorFile(proj, "github.com/m5/lib", "", "index.ts"))
			},
			wantCode:     2,
			wantCritical: 1,
			wantDetail:   []string{"content differs", "index.ts"},
			wantProof:    []string{"index.ts:content"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserEnv(t)
			m5Upstream(t, slug, "export const x = 1;\n")
			proj := m5Project(t, slug+" v1 tag")
			if tc.linkMode != "" {
				setVendorConfig(t, proj, `{"mode":"local","linkMode":"`+tc.linkMode+`"}`)
			}
			dep := m5Install(t, proj, slug)
			if tc.mutate != nil {
				tc.mutate(t, proj, dep)
			}

			code, rep, out := runVerifyJSON(t, "--content", "--json", "--dir="+proj)
			if code != tc.wantCode {
				t.Fatalf("verify exit = %d, want %d:\n%s", code, tc.wantCode, out)
			}
			if len(rep.Dependencies) != 1 {
				t.Fatalf("want exactly one dependency in the report, got %d:\n%s",
					len(rep.Dependencies), out)
			}
			if rep.Summary.Critical != tc.wantCritical {
				t.Errorf("summary critical = %d, want %d: %+v",
					rep.Summary.Critical, tc.wantCritical, rep.Summary)
			}
			if rep.Summary.Operational != tc.wantOperational {
				t.Errorf("summary operational = %d, want %d: %+v",
					rep.Summary.Operational, tc.wantOperational, rep.Summary)
			}

			depRes := rep.Dependencies[0]
			detail := ""
			for _, c := range depRes.Checks {
				detail += c.Detail + "\n"
			}
			for _, want := range tc.wantDetail {
				if !strings.Contains(detail, want) {
					t.Errorf("失败检查的 detail 必须定位到 %q，实际：\n%s", want, detail)
				}
			}

			// 内容证明只在 --content 下产出；篡改形状必须能给出 <path>:<kind>。
			gots := map[string]bool{}
			if depRes.Content != nil {
				for _, m := range depRes.Content.Mismatches {
					gots[m.Path+":"+m.Kind] = true
				}
			}
			for _, want := range tc.wantProof {
				if !gots[want] {
					t.Errorf("content proof 必须包含 %q，实际 %v", want, gots)
				}
			}
		})
	}
}
