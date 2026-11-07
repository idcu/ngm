package vendor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/digest"
)

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// writeV2StoreEntry 手工写一份 v2 清单 + meta（不经过 git），
// 以便逐个钉住 CheckEntry 的分支而不是靠 E2E 场景碰运气。
func writeV2StoreEntry(t *testing.T, s *ContentStore, digestID string, entries []ManifestEntry) {
	t.Helper()
	dir := s.V2Dir(digestID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mf := Manifest{
		SchemaVersion:   layoutSchemaVersion,
		ManifestVersion: "test/v1",
		Digest:          digestID,
		Entries:         entries,
	}
	raw, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ManifestPath(digestID), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	meta := Meta{SchemaVersion: MetaSchemaVersion, Repo: "github:test/manual", Commit: "c0ffee", Digest: digestID}
	mraw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.MetaPathV2(digestID), mraw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeBlob(t *testing.T, s *ContentStore, sha string, content []byte) {
	t.Helper()
	p := s.BlobPath(sha)
	if p == "" {
		t.Fatalf("test fixture sha %q is not a valid blob path", sha)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func testDigestID(fill string) string {
	return "sha256:" + strings.Repeat(fill, 64)
}

// CheckEntry 的四种条目形状（D1 的语义基础）：不存在 / 完好 / 缺字节 / 长度不符。
// 关键断言是"present 与 issues 分开"——条目缺失（present=false）和条目在但被
// 截断（present=true + issues）必须能被调用方区分，否则 verify 会把篡改报成
// 资源缺失（exit 4 而不是 critical 2）。
func TestCheckEntryStates(t *testing.T) {
	t.Run("empty digest is not present", func(t *testing.T) {
		store := newStore(t)
		present, issues := store.CheckEntry("")
		if present || len(issues) != 0 {
			t.Errorf("present=%v issues=%v, want false/nil", present, issues)
		}
	})

	t.Run("clean v2 entry", func(t *testing.T) {
		store := newStore(t)
		dg := testDigestID("0")
		writeBlob(t, store, shaA, []byte("hello"))
		writeV2StoreEntry(t, store, dg, []ManifestEntry{
			{Path: "index.ts", Mode: digest.ModeRegular, SHA: shaA, Size: 5},
		})

		present, issues := store.CheckEntry(dg)
		if !present || len(issues) != 0 {
			t.Fatalf("present=%v issues=%+v, want true/none", present, issues)
		}
		if !store.Has(dg) {
			t.Error("Has must be true when CheckEntry is clean")
		}
	})

	t.Run("missing blob stays present with a missing issue", func(t *testing.T) {
		store := newStore(t)
		dg := testDigestID("1")
		writeV2StoreEntry(t, store, dg, []ManifestEntry{
			{Path: "index.ts", Mode: digest.ModeRegular, SHA: shaA, Size: 5},
		})

		present, issues := store.CheckEntry(dg)
		if !present {
			t.Fatal("the entry exists (manifest + meta parse); only its blob is gone")
		}
		if len(issues) != 1 || issues[0].Kind != EntryIssueMissing || issues[0].Got != -1 || issues[0].Want != 5 {
			t.Fatalf("issues=%+v, want one missing issue with Got=-1 and Want=5", issues)
		}
		if store.Has(dg) {
			t.Error("Has must be false for a missing blob (install will re-fetch)")
		}
	})

	t.Run("size mismatch is a size issue, not a missing one", func(t *testing.T) {
		store := newStore(t)
		dg := testDigestID("2")
		writeBlob(t, store, shaA, []byte("hel")) // 期望 5，实际 3
		writeV2StoreEntry(t, store, dg, []ManifestEntry{
			{Path: "index.ts", Mode: digest.ModeRegular, SHA: shaA, Size: 5},
		})

		present, issues := store.CheckEntry(dg)
		if !present {
			t.Fatal("a truncated blob still means the entry exists")
		}
		if len(issues) != 1 || issues[0].Kind != EntryIssueSize {
			t.Fatalf("issues=%+v, want one size issue", issues)
		}
		if issues[0].Want != 5 || issues[0].Got != 3 {
			t.Errorf("Want/Got = %d/%d, want 5/3", issues[0].Want, issues[0].Got)
		}
	})

	t.Run("symlink entries have no blob and are skipped", func(t *testing.T) {
		store := newStore(t)
		dg := testDigestID("3")
		writeV2StoreEntry(t, store, dg, []ManifestEntry{
			{Path: "link", Mode: digest.ModeSymlink, Target: "index.ts", Size: 8},
			{Path: "real.ts", Mode: digest.ModeRegular, SHA: shaA, Size: 5},
		})
		writeBlob(t, store, shaA, []byte("hello"))

		present, issues := store.CheckEntry(dg)
		if !present || len(issues) != 0 {
			t.Errorf("present=%v issues=%+v; a symlink's content is its Target, not a blob", present, issues)
		}
		paths := map[string]bool{}
		for _, is := range issues {
			paths[is.Path] = true
		}
		if paths["link"] {
			t.Error("the symlink entry leaked into the issue list")
		}
	})

	t.Run("issues are sorted by path", func(t *testing.T) {
		store := newStore(t)
		dg := testDigestID("4")
		writeV2StoreEntry(t, store, dg, []ManifestEntry{
			{Path: "z.ts", Mode: digest.ModeRegular, SHA: shaB, Size: 1},
			{Path: "a.ts", Mode: digest.ModeRegular, SHA: shaA, Size: 1},
		})

		_, issues := store.CheckEntry(dg)
		if len(issues) != 2 {
			t.Fatalf("issues=%d, want 2", len(issues))
		}
		if issues[0].Path != "a.ts" || issues[1].Path != "z.ts" {
			t.Errorf("issues order = %q,%q; the report must not depend on fs/JSON order",
				issues[0].Path, issues[1].Path)
		}
	})

	t.Run("a sha that is not 64 lower hex cannot escape the blob root", func(t *testing.T) {
		store := newStore(t)
		dg := testDigestID("5")
		writeV2StoreEntry(t, store, dg, []ManifestEntry{
			{Path: "evil", Mode: digest.ModeRegular, SHA: "../../../../etc/passwd", Size: 1},
		})

		present, issues := store.CheckEntry(dg)
		if !present || len(issues) != 1 || issues[0].Kind != EntryIssueMissing {
			t.Fatalf("present=%v issues=%+v: an invalid sha must fail closed as missing", present, issues)
		}
	})
}

// 布局判定的两条：有清单时哪怕 meta 坏了也**不**回落 v1（否则一棵可能已被
// 收敛删掉的旧树会把"损坏"读成"存在"）；没有清单时才认 v1 的 meta + tree/。
func TestCheckEntryLayoutFallbacks(t *testing.T) {
	t.Run("corrupt v2 meta is not present and does not fall back", func(t *testing.T) {
		store := newStore(t)
		dg := testDigestID("6")
		writeV2StoreEntry(t, store, dg, []ManifestEntry{
			{Path: "a.ts", Mode: digest.ModeRegular, SHA: shaA, Size: 1},
		})
		// 同时放一棵"看起来完整"的 v1 树：如果实现错误地回落，这里就会变成 true。
		if err := os.MkdirAll(store.TreePath(dg), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.MetaPath(dg), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.MetaPathV2(dg), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}

		present, issues := store.CheckEntry(dg)
		if present || len(issues) != 0 {
			t.Errorf("present=%v issues=%+v: a corrupt v2 meta must not be rescued by a v1 tree", present, issues)
		}
	})

	t.Run("corrupt manifest is not present", func(t *testing.T) {
		store := newStore(t)
		dg := testDigestID("7")
		writeV2StoreEntry(t, store, dg, nil)
		if err := os.WriteFile(store.ManifestPath(dg), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}

		present, issues := store.CheckEntry(dg)
		if present || len(issues) != 0 {
			t.Errorf("present=%v issues=%+v, want false/nil", present, issues)
		}
	})

	t.Run("v1 meta plus tree is present", func(t *testing.T) {
		store := newStore(t)
		dg := testDigestID("8")
		meta := Meta{SchemaVersion: MetaSchemaVersion, Repo: "github:test/v1", Commit: "c0ffee", Digest: dg}
		raw, err := json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(store.PathForDigest(dg), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.MetaPath(dg), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(store.TreePath(dg), 0o755); err != nil {
			t.Fatal(err)
		}

		present, issues := store.CheckEntry(dg)
		if !present || len(issues) != 0 {
			t.Errorf("present=%v issues=%+v, want true/none for a legacy v1 tree", present, issues)
		}
	})

	t.Run("v1 meta without tree is not present", func(t *testing.T) {
		store := newStore(t)
		dg := testDigestID("9")
		meta := Meta{SchemaVersion: MetaSchemaVersion, Repo: "github:test/v1", Commit: "c0ffee", Digest: dg}
		raw, err := json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(store.PathForDigest(dg), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.MetaPath(dg), raw, 0o644); err != nil {
			t.Fatal(err)
		}

		present, issues := store.CheckEntry(dg)
		if present || len(issues) != 0 {
			t.Errorf("present=%v issues=%+v, want false/nil without tree/", present, issues)
		}
	})
}
