package digest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestBuildManifest_ByteFormat 是 ADR-008 清单格式的**规格即测试**。
//
// 断言的是精确字节：头部 + 每条记录的 `path NUL mode NUL sha256 NUL`。
// 用 Go 字符串字面量里的 \x00 显式写出 NUL，避免"看起来对"的模糊断言。
func TestBuildManifest_ByteFormat(t *testing.T) {
	records := []Record{
		{Path: "b.txt", Mode: ModeRegular, BlobSHA256: strings.Repeat("b", 64)},
		{Path: "a.txt", Mode: ModeExecutable, BlobSHA256: strings.Repeat("a", 64)},
	}
	got := BuildManifest(records)

	want := "ngm-archive-digest/v1\x00" +
		// 排序后 a.txt 在前（字节序）
		"a.txt\x00100755\x00" + strings.Repeat("a", 64) + "\x00" +
		"b.txt\x00100644\x00" + strings.Repeat("b", 64) + "\x00"

	if string(got) != want {
		t.Errorf("manifest bytes mismatch\n got: %q\nwant: %q", got, want)
	}
}

// TestBuildManifest_SortingIsBytewise 锁定"按 UTF-8 字节序排序"。
//
// 关键：不可依赖本地化（collation）。某些 locale 下 'a' < 'B'，但字节序是 'B'(0x42) < 'a'(0x61)。
// 若实现用了语言感知排序，跨平台/跨 locale 的 digest 就会不一致。
func TestBuildManifest_SortingIsBytewise(t *testing.T) {
	// 覆盖：大写先于小写、数字先于字母、含空格与 Unicode 的路径
	paths := []string{
		"z.txt",
		"B.txt",
		"a.txt",
		"1.txt",
		"_underscore.txt",
		"with space.txt",
		"中文.txt",
		"src/x.ts",
		"src/a.ts",
	}
	records := make([]Record, len(paths))
	for i, p := range paths {
		records[i] = Record{Path: p, Mode: ModeRegular, BlobSHA256: strings.Repeat("0", 64)}
	}
	manifest := BuildManifest(records)

	// 独立计算期望顺序：按原始字节排序
	sorted := make([]string, len(paths))
	copy(sorted, paths)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] { // Go 的 < 即字节序
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	rendered := RenderManifest(manifest)
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if lines[0] != ManifestVersion {
		t.Fatalf("header=%q", lines[0])
	}
	for i, want := range sorted {
		gotPath := strings.SplitN(lines[i+1], "\t", 2)[0]
		if gotPath != want {
			t.Errorf("position %d: got %q want %q\nfull order: %v", i, gotPath, want, sorted)
		}
	}
}

func TestBuildManifest_Empty(t *testing.T) {
	got := BuildManifest(nil)
	if string(got) != ManifestVersion+"\x00" {
		t.Errorf("empty manifest=%q", got)
	}
	// nil 与空切片等价
	if !bytes.Equal(got, BuildManifest([]Record{})) {
		t.Errorf("nil and empty slice must produce identical manifests")
	}
}

// TestBuildManifest_DoesNotMutateInput 保证调用方的切片不被排序副作用影响。
func TestBuildManifest_DoesNotMutateInput(t *testing.T) {
	records := []Record{
		{Path: "z.txt", Mode: ModeRegular, BlobSHA256: strings.Repeat("1", 64)},
		{Path: "a.txt", Mode: ModeRegular, BlobSHA256: strings.Repeat("2", 64)},
	}
	BuildManifest(records)
	if records[0].Path != "z.txt" {
		t.Errorf("input slice was reordered: %+v", records)
	}
}

func TestDigest_Format(t *testing.T) {
	manifest := BuildManifest([]Record{
		{Path: "index.ts", Mode: ModeRegular, BlobSHA256: strings.Repeat("c", 64)},
	})
	got := Digest(manifest)

	if !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("digest must start with algorithm prefix: %q", got)
	}
	hexPart := strings.TrimPrefix(got, "sha256:")
	if len(hexPart) != 64 {
		t.Errorf("hex length=%d want 64", len(hexPart))
	}
	if hexPart != strings.ToLower(hexPart) {
		t.Errorf("hex must be lowercase: %q", hexPart)
	}
	// 独立复算
	sum := sha256.Sum256(manifest)
	if want := hex.EncodeToString(sum[:]); hexPart != want {
		t.Errorf("digest=%s want %s", hexPart, want)
	}
}

// TestDigest_Deterministic 同一内容集合的不同顺序必须得到相同 digest
// （因为 BuildManifest 内部排序）。
func TestDigest_Deterministic(t *testing.T) {
	a := Record{Path: "a.ts", Mode: ModeRegular, BlobSHA256: strings.Repeat("a", 64)}
	b := Record{Path: "b.ts", Mode: ModeExecutable, BlobSHA256: strings.Repeat("b", 64)}
	c := Record{Path: "c/d.ts", Mode: ModeSymlink, BlobSHA256: strings.Repeat("c", 64)}

	d1 := Digest(BuildManifest([]Record{a, b, c}))
	d2 := Digest(BuildManifest([]Record{c, a, b}))
	d3 := Digest(BuildManifest([]Record{b, c, a}))

	if d1 != d2 || d2 != d3 {
		t.Errorf("digest depends on input order:\n%s\n%s\n%s", d1, d2, d3)
	}
}

// TestDigest_ChangesWithContent 锁定"改了内容 digest 必变"。
func TestDigest_ChangesWithContent(t *testing.T) {
	base := Record{Path: "a.ts", Mode: ModeRegular, BlobSHA256: strings.Repeat("a", 64)}

	cases := []struct {
		name string
		mod  Record
	}{
		{"path changed", Record{Path: "b.ts", Mode: ModeRegular, BlobSHA256: strings.Repeat("a", 64)}},
		{"content changed", Record{Path: "a.ts", Mode: ModeRegular, BlobSHA256: strings.Repeat("b", 64)}},
		{"mode changed", Record{Path: "a.ts", Mode: ModeExecutable, BlobSHA256: strings.Repeat("a", 64)}},
	}
	baseDigest := Digest(BuildManifest([]Record{base}))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Digest(BuildManifest([]Record{tc.mod})); got == baseDigest {
				t.Errorf("digest did not change when %s", tc.name)
			}
		})
	}

	// 多一条记录也必须变化（防止"漏项不影响 digest"）
	twoRecords := Digest(BuildManifest([]Record{base, {Path: "extra.ts", Mode: ModeRegular, BlobSHA256: strings.Repeat("d", 64)}}))
	if twoRecords == baseDigest {
		t.Errorf("digest did not change when a record was added")
	}
}

// TestHashBytes_CRLFPreserved 是 ADR-008 "不做 CRLF → LF 转换" 的单元级防线。
func TestHashBytes_CRLFPreserved(t *testing.T) {
	lf := HashBytes([]byte("a\nb\n"))
	crlf := HashBytes([]byte("a\r\nb\r\n"))
	if lf == crlf {
		t.Fatalf("CRLF and LF contents must hash differently (no newline conversion allowed)")
	}

	// 与直接 sha256 对照，确认没有做任何归一化
	sum := sha256.Sum256([]byte("a\r\nb\r\n"))
	if want := hex.EncodeToString(sum[:]); crlf != want {
		t.Errorf("HashBytes did not hash raw bytes: %s want %s", crlf, want)
	}
}

func TestRenderManifest(t *testing.T) {
	manifest := BuildManifest([]Record{
		{Path: "with space.txt", Mode: ModeRegular, BlobSHA256: strings.Repeat("1", 64)},
		{Path: "core/user info.ts", Mode: ModeExecutable, BlobSHA256: strings.Repeat("2", 64)},
	})
	got := RenderManifest(manifest)
	want := ManifestVersion + "\n" +
		"core/user info.ts\t100755\t" + strings.Repeat("2", 64) + "\n" +
		"with space.txt\t100644\t" + strings.Repeat("1", 64) + "\n"
	if got != want {
		t.Errorf("RenderManifest\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderManifest_Truncated(t *testing.T) {
	// 残缺清单不应 panic
	got := RenderManifest([]byte(ManifestVersion + "\x00bad\x00record"))
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncated manifest should be flagged: %q", got)
	}
	if got := RenderManifest(nil); got != "" {
		t.Errorf("nil manifest should render empty, got %q", got)
	}
}

func TestModeConstants(t *testing.T) {
	// 锁定 Git 模式串的字面值——它们直接进入 digest 输入
	pairs := map[string]string{
		ModeRegular:    "100644",
		ModeExecutable: "100755",
		ModeSymlink:    "120000",
		ModeGitlink:    "160000",
		ModeTree:       "40000",
	}
	for got, want := range pairs {
		if got != want {
			t.Errorf("mode constant %q != %q", got, want)
		}
	}
	if ManifestVersion != "ngm-archive-digest/v1" {
		t.Errorf("manifest version changed: %q (this breaks every digest; requires lock migration)", ManifestVersion)
	}
	if Algorithm != "sha256" {
		t.Errorf("algorithm changed: %q", Algorithm)
	}
}
