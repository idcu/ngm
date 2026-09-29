package digest

import (
	"strings"
	"testing"
)

const sampleLFSPointer = "version https://git-lfs.github.com/spec/v1\n" +
	"oid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\n" +
	"size 12345\n"

func TestDetectLFSPointer(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
		wantOID string
	}{
		{
			name:    "canonical pointer",
			content: sampleLFSPointer,
			want:    true,
			wantOID: "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393",
		},
		{
			name:    "no trailing newline",
			content: strings.TrimRight(sampleLFSPointer, "\n"),
			want:    true,
			wantOID: "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393",
		},
		{
			name:    "extra fields after size (e.g. extensions)",
			content: sampleLFSPointer + "x-custom-field foo\n",
			want:    true,
			wantOID: "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393",
		},
		{
			name:    "large size value",
			content: strings.Replace(sampleLFSPointer, "size 12345", "size 4294967296", 1),
			want:    true,
			wantOID: "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393",
		},
		// ---- 非指针：必须不误报 ----
		{name: "empty", content: "", want: false},
		{
			name:    "plain text",
			content: "export const x = 1\n",
			want:    false,
		},
		{
			name:    "mentions lfs but not a pointer",
			content: "This project uses https://git-lfs.github.com/spec/v1 for big files.\n",
			want:    false,
		},
		{
			name:    "version line only, no oid/size",
			content: "version https://git-lfs.github.com/spec/v1\n",
			want:    false,
		},
		{
			name: "missing size line",
			content: "version https://git-lfs.github.com/spec/v1\n" +
				"oid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\n",
			want: false,
		},
		{
			name: "missing oid line",
			content: "version https://git-lfs.github.com/spec/v1\n" +
				"size 12345\n",
			want: false,
		},
		{
			name: "short oid",
			content: "version https://git-lfs.github.com/spec/v1\n" +
				"oid sha256:abc123\n" +
				"size 12345\n",
			want: false,
		},
		{
			name: "uppercase oid (spec requires lowercase hex)",
			content: "version https://git-lfs.github.com/spec/v1\n" +
				"oid sha256:4D7A214614AB2935C943F9E0FF69D22EADBB8F32B1258DAAA5E2CA24D17E2393\n" +
				"size 12345\n",
			want: false,
		},
		{
			name: "sha256 oid but wrong algo prefix",
			content: "version https://git-lfs.github.com/spec/v1\n" +
				"oid sha512:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\n" +
				"size 12345\n",
			want: false,
		},
		{
			name: "negative size",
			content: "version https://git-lfs.github.com/spec/v1\n" +
				"oid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\n" +
				"size -1\n",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DetectLFSPointer([]byte(tc.content))
			if ok != tc.want {
				t.Fatalf("DetectLFSPointer(ok)=%v want %v\ncontent:\n%s", ok, tc.want, tc.content)
			}
			if tc.want && got.OID != tc.wantOID {
				t.Errorf("OID=%q want %q", got.OID, tc.wantOID)
			}
		})
	}
}

// TestDetectLFSPointer_SizeGuard 超长内容不做正则扫描（避免大文件被无谓解析）。
func TestDetectLFSPointer_SizeGuard(t *testing.T) {
	big := make([]byte, 4096)
	copy(big, sampleLFSPointer)
	if _, ok := DetectLFSPointer(big); ok {
		t.Errorf("oversized content must not be treated as a pointer")
	}
}

// TestDetectLFSPointer_CRLF 虽然 LFS 规范要求 LF，但现实中存在 CRLF 指针；
// 我们容忍它并仍然报错（宁可识别出来报错，也好过静默哈希一个"像指针的普通文件"）。
func TestDetectLFSPointer_CRLF(t *testing.T) {
	content := strings.ReplaceAll(sampleLFSPointer, "\n", "\r\n")
	got, ok := DetectLFSPointer([]byte(content))
	if !ok {
		t.Fatalf("CRLF pointer should still be detected")
	}
	if got.OID != "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393" {
		t.Errorf("OID=%q", got.OID)
	}
}
