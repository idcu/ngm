package lock

import (
	"strings"
	"testing"
)

// v0.81：`archiveDigest` 的**完整形态**判据，以及它**不该**用在 lock 读取路径上。
//
// 背景：`Validate` 只校验 `sha256:` 前缀，于是 `"sha256:"`（空哈希）能通过，
// SBOM 会导出 `{"alg":"SHA-256","content":""}`、attestation 写出 `sha256: ""`
// ——**结构合法、内容为空**的假证据。
//
// 修法不放在 Validate 里，原因见 `IsArchiveDigest` 的注释（这段是判据的一部分）：
// lock 里形状不对的 digest 也可能是**被篡改**，而那必须是 exit 2 的完整性事件，
// 不能在读取时被判成 exit 3 的配置问题——`TestM3Acceptance/
// install_rejects_a_tampered_lock_digest` 钉的就是这条。
// 于是形状判据放在**导出层**（`internal/sbom.FromLock`）。
//
// 牙齿：把 IsArchiveDigest 的长度/字符集判定去掉，本测试红。

func TestV81ArchiveDigestShape(t *testing.T) {
	const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cases := []struct {
		digest string
		want   bool
		why    string
	}{
		{"sha256:" + hex64, true, "标准形态"},
		{"sha256:", false, "空哈希——正是让下游导出空承诺的那一种"},
		{"sha256:abc", false, "长度不足"},
		{"sha256:" + hex64 + "00", false, "超长（被篡改的 lock 就是这种形状）"},
		{"sha256:" + strings.ToUpper(hex64), false, "大写 hex 不是 ngm 产出的形态"},
		{"sha512:" + hex64, false, "算法前缀不对"},
		{"", false, "空串"},
		{hex64, false, "缺算法前缀"},
	}
	for _, tc := range cases {
		if got := IsArchiveDigest(tc.digest); got != tc.want {
			t.Errorf("IsArchiveDigest(%q) = %v, want %v（%s）", tc.digest, got, tc.want, tc.why)
		}
	}
}

// TestV81ValidateOnlyChecksTheAlgorithmPrefix 钉住"读取路径只校验前缀"这条决定。
//
// 它是**有意的**：一个形状不对的 digest 要留到真正比对内容的那一层
// （install / verify）去报 exit 2，而不是在读取时被判成 exit 3。
// 若有人"顺手"把 IsArchiveDigest 接进 Validate，本测试红并说明代价。
func TestV81ValidateOnlyChecksTheAlgorithmPrefix(t *testing.T) {
	base := func() Dependency {
		return Dependency{
			Name:       "github:org/dep",
			Ref:        "v1.0.0",
			RefType:    "tag",
			Commit:     "0123456789abcdef0123456789abcdef01234567",
			VendorPath: "github.com/org/dep",
			ArchiveDigest: "sha256:0123456789abcdef0123456789abcdef" +
				"0123456789abcdef0123456789abcdef",
		}
	}

	// 长度不对但仍是 sha256 → 由 Validate **放行**（留给内容比对层报 exit 2）
	d := base()
	d.ArchiveDigest = "sha256:" + strings.Repeat("0", 128)
	if err := d.validate(); err != nil {
		t.Errorf("形状不对但算法正确的 digest 必须由 Validate 放行（留给完整性层），got: %v", err)
	}

	// 算法前缀不对 → 仍然拒绝（这不是"被篡改"，是写错了）
	d = base()
	d.ArchiveDigest = "md5:" + strings.Repeat("0", 64)
	if err := d.validate(); err == nil {
		t.Error("算法前缀不对必须被拒绝")
	}
}
