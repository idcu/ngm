package digest

import (
	"strings"
	"testing"
)

const okSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// v0.107：清单的字节流以 NUL 分隔字段，**路径本身就是分隔符的载体**。
//
// 缺陷形态：`BuildManifest` 直接拼接，不做字段校验，于是
//
//	Path = "a\x00100644\x00<64hex>\x00b"
//
// 能把**一条记录变成两条**。生产路径目前只从 `git ls-tree -z` 取路径
// （Git 路径不含 NUL），所以尚未被利用；但 `verify --deep` 的模块分支会把
// **content store 清单 JSON** 里的 Path/Mode 直接拿去重算摘要，而那是可改写的文件——
// 也就是说 `--deep` 可以被一份改过的清单骗过。
func TestValidateRecordRejectsBoundaryForgery(t *testing.T) {
	bad := []struct {
		name string
		rec  Record
	}{
		{"NUL in path", Record{Path: "a\x00100644\x00" + okSHA + "\x00b", Mode: ModeRegular, BlobSHA256: okSHA}},
		{"newline in path", Record{Path: "a\nb", Mode: ModeRegular, BlobSHA256: okSHA}},
		{"empty path", Record{Path: "", Mode: ModeRegular, BlobSHA256: okSHA}},
		{"parent segment", Record{Path: "../etc/passwd", Mode: ModeRegular, BlobSHA256: okSHA}},
		{"dot segment", Record{Path: "a/./b", Mode: ModeRegular, BlobSHA256: okSHA}},
		{"double slash", Record{Path: "a//b", Mode: ModeRegular, BlobSHA256: okSHA}},
		{"absolute", Record{Path: "/etc/passwd", Mode: ModeRegular, BlobSHA256: okSHA}},
		{"dot-slash prefix", Record{Path: "./a", Mode: ModeRegular, BlobSHA256: okSHA}},
		{"backslash", Record{Path: `a\b`, Mode: ModeRegular, BlobSHA256: okSHA}},
		{"gitlink mode", Record{Path: "a", Mode: ModeGitlink, BlobSHA256: okSHA}},
		{"unknown mode", Record{Path: "a", Mode: "100664", BlobSHA256: okSHA}},
		{"short sha", Record{Path: "a", Mode: ModeRegular, BlobSHA256: "abc"}},
		{"upper-case sha", Record{Path: "a", Mode: ModeRegular, BlobSHA256: strings.ToUpper(okSHA)}},
		{"non-hex sha", Record{Path: "a", Mode: ModeRegular, BlobSHA256: "../../../../etc/passwd"}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRecord(tc.rec); err == nil {
				t.Error("this record can forge manifest boundaries and must be rejected")
			}
		})
	}

	// 反向：真实会出现的三种形态都必须通过（否则这条判据会误伤生产路径）。
	good := []Record{
		{Path: "index.js", Mode: ModeRegular, BlobSHA256: okSHA},
		{Path: "src/deep/mod.ts", Mode: ModeExecutable, BlobSHA256: okSHA},
		{Path: "link", Mode: ModeSymlink, BlobSHA256: okSHA},
	}
	for _, r := range good {
		if err := ValidateRecord(r); err != nil {
			t.Errorf("%+v must be accepted: %v", r, err)
		}
	}
}

// 零字节文件的 sha256 是合法的 64 位十六进制——不许因为"看起来像空"被拒。
func TestValidateRecordAcceptsTheEmptyBlobHash(t *testing.T) {
	rec := Record{Path: "empty", Mode: ModeRegular, BlobSHA256: strings.Repeat("0", 64)}
	if err := ValidateRecord(rec); err != nil {
		t.Errorf("a zero-filled (but well-formed) hash must pass: %v", err)
	}
}
