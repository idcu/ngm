package vendor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/digest"
)

// v0.66：symlink / 路径逃逸的校验（评估报告 P1#2）。
//
// 为什么这一组值得单独存在：现有 fixture **从不含 symlink**（全仓库
// `*_test.go` 里零处），因此"逃逸校验"的两条路径此前都没有证据。
// 这里用**纯函数表 + 清单构造**覆盖，不依赖在测试机上创建真实 symlink
// （Windows 需要开发者模式或管理员权限，会把判据变成平台相关的）。

// TestV66EscapesPackageTree 固定"逃逸判定"的边界表。
func TestV66EscapesPackageTree(t *testing.T) {
	cases := []struct {
		entry, target string
		want          bool
		why           string
	}{
		{"src/link.ts", "./index.ts", false, "同目录下的相对目标"},
		{"src/link.ts", "../index.ts", false, "回到上一级但仍在包内"},
		{"link.ts", "index.ts", false, "同目录名"},
		{"a/b/link", "../../x/y", false, "恰好回到包根内"},
		{"link.ts", "../outside.ts", true, "越过包根"},
		{"a/link", "../../x", true, "越过包根（更远）"},
		{"link.ts", "/etc/passwd", true, "POSIX 绝对路径"},
		{"link.ts", `C:/Windows/x`, true, "Windows 驱动器前缀（在 Unix 上同样要判出）"},
		{"link.ts", `\\server\share`, true, "UNC 前导"},
		{"", "", false, "空目标不是 symlink 的样子"},
		{"link.ts", "a/../b", false, "规范化后仍在包内"},
		{"link.ts", "a/../../b", true, "先入 a/ 再退两级：越过包根"},
	}
	for _, tc := range cases {
		if got := EscapesPackageTree(tc.entry, tc.target); got != tc.want {
			t.Errorf("EscapesPackageTree(%q, %q) = %v, want %v（%s）",
				tc.entry, tc.target, got, tc.want, tc.why)
		}
	}
}

// TestV66ValidateEntryPaths 覆盖两类拒绝：path 逃逸与 symlink 目标逃逸。
func TestV66ValidateEntryPaths(t *testing.T) {
	t.Run("条目 path 不得逃逸", func(t *testing.T) {
		bad := []ContentEntry{
			{Path: "../outside/x", Size: 1},
			{Path: "/abs/x", Size: 1},
			{Path: `C:/abs/x`, Size: 1},
		}
		for _, e := range bad {
			if err := validateEntryPaths([]ContentEntry{e}); err == nil {
				t.Errorf("path %q 必须被拒绝", e.Path)
			} else if !strings.Contains(err.Error(), "escapes the vendor tree") {
				t.Errorf("拒绝 %q 时要说清成因，got: %v", e.Path, err)
			}
		}
	})

	t.Run("合法的相对条目放行", func(t *testing.T) {
		ok := []ContentEntry{
			{Path: "a/b.ts", Size: 1},
			{Path: "./c.ts", Size: 1},
			{Path: "l", Symlink: true, LinkTarget: "a/b.ts"},
			{Path: "a/l", Symlink: true, LinkTarget: "../b.ts"},
		}
		if err := validateEntryPaths(ok); err != nil {
			t.Errorf("合法的条目不该被拒绝: %v", err)
		}
	})

	t.Run("逃逸 symlink 必须被拒绝且点名", func(t *testing.T) {
		err := validateEntryPaths([]ContentEntry{
			{Path: "link.ts", Symlink: true, LinkTarget: "../outside.ts"},
		})
		if err == nil {
			t.Fatal("目标越过包根的 symlink 必须被拒绝")
		}
		if !strings.Contains(err.Error(), "link.ts") || !strings.Contains(err.Error(), "escapes") {
			t.Errorf("错误必须点名条目与成因，got: %v", err)
		}
	})
}

// escapeManifest 构造一份含逃逸 symlink 的清单并发布进 store，返回 digest。
func escapeManifest(t *testing.T, store *ContentStore) string {
	t.Helper()
	dg := "sha256:" + strings.Repeat("11", 32)
	mf := Manifest{
		SchemaVersion:   layoutSchemaVersion,
		ManifestVersion: digest.ManifestVersion,
		Digest:          dg,
		Entries: []ManifestEntry{
			{Path: "escape.ts", Mode: digest.ModeSymlink, Size: 15, Target: "../../outside.ts"},
		},
	}
	meta := Meta{
		SchemaVersion:   MetaSchemaVersion,
		Repo:            "github:test/escape",
		Commit:          strings.Repeat("a", 40),
		Digest:          dg,
		ManifestVersion: digest.ManifestVersion,
	}
	if err := store.publishManifest(mf, meta); err != nil {
		t.Fatalf("publishManifest: %v", err)
	}
	return dg
}

// TestV66MaterializeRefusesEscapingEntry 是落地侧的核心断言，并且固定一条
// 容易被忽略的顺序性质：**预检发生在清理旧内容之前**。
//
// 若顺序反了（先 RemoveAll(dst) 再校验），一次被拒绝的落地会先把用户已有的
// vendor 树删掉——"拒绝"本身就成了破坏。
func TestV66MaterializeRefusesEscapingEntry(t *testing.T) {
	// 正常内容先落地（真实 fixture，保证 store 里有一份可用内容）。
	dg, store := contentFixture(t, basicRepo)
	vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
	lt := NewLinkTree(vendorRoot, store, LinkAuto)

	first, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatalf("第一次落地应成功: %v", err)
	}

	// 同一个 canonical 路径上的逃逸清单。
	badDigest := escapeManifest(t, store)
	_, err = lt.Materialize(badDigest, "github.com/test/repo", "")
	if err == nil {
		t.Fatal("symlink 目标逃逸包根时必须拒绝落地")
	}
	if !strings.Contains(err.Error(), "escape.ts") || !strings.Contains(err.Error(), "escapes") {
		t.Errorf("错误必须点名条目与成因，got: %v", err)
	}

	// 顺序性质：已有内容必须原样还在。
	if _, serr := os.Stat(filepath.Join(first.Path, "index.ts")); serr != nil {
		t.Errorf("一次被拒绝的落地不得先删掉已有的 vendor 内容: %v", serr)
	}
}

// TestV66PruneKeepsYoungResidue 固定 Prune 的年龄门槛（评估报告 P2#15）：
// `Put` 的临时清单目录与"残骸"共用 `.unpack-` 前缀，因此新目录一律不动。
func TestV66PruneKeepsYoungResidue(t *testing.T) {
	store := newStore(t)
	algDir := filepath.Join(store.Root(), digest.Algorithm)

	oldDir := filepath.Join(algDir, unpackPrefix+"old")
	youngDir := filepath.Join(algDir, unpackPrefix+"young")
	for _, d := range []string{oldDir, youngDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// 把"旧"的那个拨到门槛之外（用 Chtimes，不 Sleep）。
	past := time.Now().Add(-2 * UnpackResidueGrace)
	if err := os.Chtimes(oldDir, past, past); err != nil {
		t.Fatal(err)
	}

	res, err := store.Prune(false)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if _, serr := os.Stat(oldDir); !os.IsNotExist(serr) {
		t.Errorf("比门槛旧的残骸应被删除；stat err = %v", serr)
	}
	if _, serr := os.Stat(youngDir); serr != nil {
		t.Errorf("比门槛新的残骸必须保留（那是并发 install 的活动目录）: %v", serr)
	}
	if res.KeptYoung != 1 {
		t.Errorf("报告里 KeptYoung = %d, want 1（「没删的那些」必须被说出来）", res.KeptYoung)
	}
	if len(res.Removed) != 1 {
		t.Errorf("Removed = %v, want 恰好一个", res.Removed)
	}
}

// TestV66PruneOrphansRejectsNonPositiveOlderThan 固定"0 不是合法的安全窗"
// （评估报告 P2#16）：门槛为 0 会删掉刚写入、尚未被清单引用的 blob。
func TestV66PruneOrphansRejectsNonPositiveOlderThan(t *testing.T) {
	store := newStore(t)
	for _, dur := range []time.Duration{0, -time.Hour} {
		if _, err := store.PruneOrphans(dur, time.Now(), true); err == nil {
			t.Errorf("older-than=%s 必须被拒绝（它让并发安全窗失效）", dur)
		}
	}
}
