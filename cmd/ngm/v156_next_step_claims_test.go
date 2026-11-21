package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件守 2026-11-21 下一阶段计划 N1–N3 的三条判据（spec: next-step）。
// 证据是同日全维度差距评估 §5.1–5.3 已识别的过期事实与主轴演示缺口，
// 不是"文档最好写得好看"这类泛化要求（ADR-049）。

func readNextStepDoc(t *testing.T, parts ...string) string {
	t.Helper()
	args := append([]string{repoRoot(t)}, parts...)
	data, err := os.ReadFile(filepath.Join(args...))
	if err != nil {
		t.Fatalf("cannot read %v: %v", parts, err)
	}
	return string(data)
}

// TestV156CompetitiveClaimsDoNotAssertThreeUniques 固定 N1：
// "三项在主流工具里仍无对应物"已被差距评估 §5.2 推翻，不能再出现在对外结论里。
func TestV156CompetitiveClaimsDoNotAssertThreeUniques(t *testing.T) {
	doc := readNextStepDoc(t, "docs", "COMPETITIVE-ANALYSIS.md")
	if strings.Contains(doc, "三项在主流工具里仍无对应物") {
		t.Error("the stale 'three unique controls' claim is still present — see gap report §5.2")
	}
	for _, needle := range []string{"同一状态机", "不再独有"} {
		if !strings.Contains(doc, needle) {
			t.Errorf("the corrected claim must say %q (the unique thing is the closed state machine)", needle)
		}
	}
}

// TestV157SLSAParagraphCarriesProvenanceAdoptionReading 固定 N2：
// SLSA 段此前只写 top500 约 1/5 / PyPI 17%，缺 2026-10/11 的 provenance 覆盖实测。
func TestV157SLSAParagraphCarriesProvenanceAdoptionReading(t *testing.T) {
	doc := readNextStepDoc(t, "docs", "COMPETITIVE-ANALYSIS.md")
	for _, needle := range []string{"12%", "38.8%", "告警"} {
		if !strings.Contains(doc, needle) {
			t.Errorf("the SLSA paragraph must carry %q (provenance adoption is the reason for warn-not-reject)", needle)
		}
	}
}

// TestV158ConsumerCaseStudyDemonstratesTheWholeLoop 固定 N3：
// 差异化闭环必须有一份可复现案例，且必须包含"篡改被抓到"的注入演示。
func TestV158ConsumerCaseStudyDemonstratesTheWholeLoop(t *testing.T) {
	doc := readNextStepDoc(t, "docs", "practice", "ngm-consumer-case-study-2026-11-21.md")
	lower := strings.ToLower(doc)
	for _, seq := range []struct{ name, needle string }{
		{"init", "ngm init"},
		{"add", "ngm add"},
		{"frozen/offline install", "--frozen-lockfile --offline"},
		{"content verify", "verify --content"},
		{"sbom", "ngm sbom"},
		{"attest", "ngm attest"},
		{"sign recipe reference", "signing.md"},
		{"tamper target", "ngm.vendor"},
		{"tamper exit code", "exit 2"},
	} {
		if !strings.Contains(lower, strings.ToLower(seq.needle)) {
			t.Errorf("case study misses %s (%q)", seq.name, seq.needle)
		}
	}
	// 验证顺序：install < verify < sbom/attest（注入演示在 verify 之后另行展示）。
	iInstall := strings.Index(lower, "--frozen-lockfile --offline")
	iVerify := strings.Index(lower, "verify --content")
	iAttest := strings.Index(lower, "ngm attest")
	if !(iInstall >= 0 && iVerify > iInstall && iAttest > iVerify) {
		t.Errorf("wrong command order: install=%d verify=%d attest=%d", iInstall, iVerify, iAttest)
	}
	if strings.Contains(lower, "ngm attest --sign") || strings.Contains(lower, "ngm sign-blob") {
		t.Error("the case study invented an ngm self-signing command — ADR-014/027 forbids it")
	}
}
