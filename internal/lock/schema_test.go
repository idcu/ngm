package lock

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/testutils"
)

// sampleLock 构造一个字段完整的 lock，用于格式与序列化测试。
func sampleLock() *File {
	f := NewFile()
	f.Dependencies = []Dependency{
		{
			Name:          "github:org/utils",
			Ref:           "v1.2.3",
			RefType:       "tag",
			Commit:        "abc123def4567890abcdef1234567890abcdef12",
			ArchiveDigest: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			ResolvedAt:    "2026-09-29T10:00:00Z",
			VendorPath:    "github.com/org/utils",
		},
		{
			Name:          "github:org/logger",
			Ref:           "main",
			RefType:       "branch",
			Commit:        "def456abc7890123def456abc7890123def456ab",
			ArchiveDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			ResolvedAt:    "2026-09-29T10:00:00Z",
			VendorPath:    "github.com/org/logger",
		},
	}
	f.Sort()
	return f
}

// TestMarshal_GoldenFormat 冻结 ngm.lock 的序列化字节。
//
// 这是 v0.1 的格式契约：字段顺序 / 2 空格缩进 / LF / 末尾换行。
// 任何变化都必须在本 PR 说明，并评估 lockfileVersion（locking.md §可复现性的定义）。
func TestMarshal_GoldenFormat(t *testing.T) {
	data, err := sampleLock().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	testutils.GoldenString(t, "vectors/lock-basic.golden", string(data))
}

// TestMarshal_ExactBytes 用字面量断言关键格式特征（不依赖 golden 文件的存在）。
func TestMarshal_ExactBytes(t *testing.T) {
	data, err := sampleLock().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)

	// 末尾必须有且仅有一个换行
	if !strings.HasSuffix(s, "}\n") {
		t.Errorf("lock must end with a single LF after the closing brace")
	}
	if strings.HasSuffix(s, "}\n\n") {
		t.Errorf("lock must not end with a blank line")
	}
	// 行尾一律 LF
	if strings.Contains(s, "\r") {
		t.Errorf("lock must use LF line endings only")
	}
	// 缩进：每层 2 空格。顶层字段 2、数组元素 `{` 4、元素内字段 6
	if !strings.Contains(s, "\n  \"version\": 1,") {
		t.Errorf("expected 2-space indentation at the top level:\n%s", s)
	}
	if !strings.Contains(s, "\n    {\n") {
		t.Errorf("expected dependency objects indented by 4 spaces:\n%s", s)
	}
	if !strings.Contains(s, "\n      \"name\":") {
		t.Errorf("expected dependency fields indented by 6 spaces:\n%s", s)
	}

	// 字段顺序（顶层）
	topOrder := []string{`"version"`, `"lockfileVersion"`, `"dependencies"`}
	last := -1
	for _, key := range topOrder {
		idx := strings.Index(s, key)
		if idx < 0 {
			t.Fatalf("missing top-level key %s", key)
		}
		if idx < last {
			t.Errorf("top-level key %s is out of order", key)
		}
		last = idx
	}

	// 字段顺序（条目内）
	entryOrder := []string{
		`"name"`, `"ref"`, `"refType"`, `"commit"`,
		`"archiveDigest"`, `"resolvedAt"`, `"vendorPath"`,
	}
	last = -1
	for _, key := range entryOrder {
		idx := strings.Index(s, key)
		if idx < 0 {
			t.Fatalf("missing entry key %s", key)
		}
		if idx < last {
			t.Errorf("entry key %s is out of order", key)
		}
		last = idx
	}
}

// TestMarshal_Deterministic 同输入两次序列化必须字节一致。
func TestMarshal_Deterministic(t *testing.T) {
	a, err := sampleLock().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	b, err := sampleLock().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Errorf("Marshal is not deterministic")
	}
}

// TestMarshal_ResolvedAtIsOnlyNonDeterministicField 是可复现性的核心断言：
// 两次解析（时间不同）产出的 lock，除 resolvedAt 外字节一致。
//
// 判定方式来自 locking.md §可复现性的定义。
func TestMarshal_ResolvedAtIsOnlyNonDeterministicField(t *testing.T) {
	first := sampleLock()
	first.Dependencies[0].ResolvedAt = FormatResolvedAt(time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC))
	first.Dependencies[1].ResolvedAt = FormatResolvedAt(time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC))
	a, err := first.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	second := sampleLock()
	second.Dependencies[0].ResolvedAt = FormatResolvedAt(time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC))
	second.Dependencies[1].ResolvedAt = FormatResolvedAt(time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC))
	b, err := second.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	// 原始字节必须不同（时间确实写进去了）
	if string(a) == string(b) {
		t.Fatalf("resolvedAt did not affect the bytes")
	}
	// 屏蔽 resolvedAt 后必须一致
	if stripResolvedAt(string(a)) != stripResolvedAt(string(b)) {
		t.Errorf("locks differ beyond resolvedAt:\n--- A ---\n%s\n--- B ---\n%s",
			stripResolvedAt(string(a)), stripResolvedAt(string(b)))
	}
}

var resolvedAtLine = regexp.MustCompile(`(?m)^(\s*)"resolvedAt": "[^"]*"(,?)$`)

func stripResolvedAt(s string) string {
	return resolvedAtLine.ReplaceAllString(s, `${1}"resolvedAt": "<TIME>"${2}`)
}

func TestFormatResolvedAt(t *testing.T) {
	// 时区归一为 UTC
	loc := time.FixedZone("UTC+8", 8*3600)
	got := FormatResolvedAt(time.Date(2026, 9, 29, 18, 0, 0, 0, loc))
	if got != "2026-09-29T10:00:00Z" {
		t.Errorf("FormatResolvedAt=%q want 2026-09-29T10:00:00Z", got)
	}
	// 亚秒被截断（秒级精度）
	got2 := FormatResolvedAt(time.Date(2026, 9, 29, 10, 0, 0, 987654321, time.UTC))
	if got2 != "2026-09-29T10:00:00Z" {
		t.Errorf("sub-second precision should be truncated, got %q", got2)
	}
}

func TestSort(t *testing.T) {
	f := NewFile()
	f.Dependencies = []Dependency{
		{Name: "github:z/zeta", VendorPath: "x"},
		{Name: "github:a/alpha", VendorPath: "x"},
		{Name: "github:m/mid", VendorPath: "x"},
		// 同仓库不同子路径：subPath 是二级排序键
		{Name: "github:a/alpha", SubPath: "packages/core", VendorPath: "x"},
	}
	f.Sort()

	got := make([]string, len(f.Dependencies))
	for i, d := range f.Dependencies {
		got[i] = d.Name + "#" + d.SubPath
	}
	want := []string{
		"github:a/alpha#",
		"github:a/alpha#packages/core",
		"github:m/mid#",
		"github:z/zeta#",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// ---------------------------------------------------------------------------
// 读取与校验
// ---------------------------------------------------------------------------

func TestReadWrite_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	original := sampleLock()
	if err := Write(path, original); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got.Dependencies) != len(original.Dependencies) {
		t.Fatalf("deps=%d want %d", len(got.Dependencies), len(original.Dependencies))
	}
	if got.Dependencies[0].ArchiveDigest != original.Dependencies[0].ArchiveDigest {
		t.Errorf("digest round-trip failed")
	}
	if got.Version != SchemaVersion || got.LockfileVersion != FileVersion {
		t.Errorf("version fields: %d / %q", got.Version, got.LockfileVersion)
	}
}

func TestRead_MissingFileIsNotAnError(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "ngm.lock"))
	if err != nil {
		t.Fatalf("missing lock should not be an error: %v", err)
	}
	if got != nil {
		t.Errorf("missing lock should return nil, got %+v", got)
	}
}

// TestRead_IgnoresUnknownFields 锁定向前兼容契约：
// locking.md §lockfileVersion 演进要求"旧版本工具应忽略未知字段继续工作"。
func TestRead_IgnoresUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	content := `{
  "version": 1,
  "lockfileVersion": "1.0.0",
  "dependencies": [
    {
      "name": "github:org/utils",
      "ref": "v1.2.3",
      "refType": "tag",
      "commit": "abc123def4567890abcdef1234567890abcdef12",
      "archiveDigest": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
      "resolvedAt": "2026-09-29T10:00:00Z",
      "vendorPath": "github.com/org/utils",
      "futureFieldWrittenByNewerNgm": {"nested": [1, 2, 3]}
    }
  ],
  "futureTopLevel": true
}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Read(path)
	if err != nil {
		t.Fatalf("unknown fields must be tolerated: %v", err)
	}
	if len(f.Dependencies) != 1 {
		t.Errorf("deps=%d", len(f.Dependencies))
	}
}

func TestRead_Corrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Read(path)
	if err == nil {
		t.Fatalf("corrupt lock should fail")
	}
	// Hint 不参与 Error()（它供 CLI 的 FormatHuman 使用），须单独断言
	var ne *errs.NgmError
	if !errors.As(err, &ne) {
		t.Fatalf("error type %T", err)
	}
	if !strings.Contains(ne.Hint, "ngm install") {
		t.Errorf("hint should point at regeneration, got %q", ne.Hint)
	}
	if ne.Code != errs.CodeConfigInvalid {
		t.Errorf("exit code=%d want 3", ne.Code.ExitCode())
	}
}

func TestValidate(t *testing.T) {
	valid := func() *Dependency {
		return &Dependency{
			Name:          "github:org/utils",
			Ref:           "v1.2.3",
			RefType:       "tag",
			Commit:        "abc123def4567890abcdef1234567890abcdef12",
			ArchiveDigest: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			ResolvedAt:    "2026-09-29T10:00:00Z",
			VendorPath:    "github.com/org/utils",
		}
	}
	cases := []struct {
		name    string
		mutate  func(*File)
		wantSub string
	}{
		{"ok", func(f *File) {}, ""},
		{"bad version", func(f *File) { f.Version = 99 }, "unsupported lock `version`"},
		{"missing lockfileVersion", func(f *File) { f.LockfileVersion = "" }, "lockfileVersion"},
		{"major mismatch", func(f *File) { f.LockfileVersion = "2.0.0" }, "incompatible lockfileVersion"},
		{"missing name", func(f *File) { f.Dependencies[0].Name = "" }, "name"},
		{"invalid slug", func(f *File) { f.Dependencies[0].Name = "nope" }, "invalid git address"},
		{"missing ref", func(f *File) { f.Dependencies[0].Ref = "" }, "ref"},
		{"invalid refType", func(f *File) { f.Dependencies[0].RefType = "banana" }, "refType"},
		{"short commit", func(f *File) { f.Dependencies[0].Commit = "abc1234" }, "full 40-char"},
		{"non-hex commit", func(f *File) {
			f.Dependencies[0].Commit = strings.Repeat("z", 40)
		}, "full 40-char"},
		{"missing digest prefix", func(f *File) {
			f.Dependencies[0].ArchiveDigest = "9f86d081"
		}, "sha256:"},
		{"missing vendorPath", func(f *File) { f.Dependencies[0].VendorPath = "" }, "vendorPath"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFile()
			f.Dependencies = []Dependency{*valid()}
			tc.mutate(f)
			err := f.Validate()
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestMarshal_EmptyDependenciesIsArray(t *testing.T) {
	data, err := NewFile().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"dependencies": []`) {
		t.Errorf("empty dependencies must serialize as [], got:\n%s", data)
	}
}
